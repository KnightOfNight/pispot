package hotspot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mcs-net/pispot/dashboard/internal/config"
)

// Default timings. cacheTTL bounds iw exec frequency even under rapid
// polling; execTimeout prevents a hung iw from wedging the handler.
const (
	cacheTTL    = 1 * time.Second
	execTimeout = 2 * time.Second
)

// ErrInterfaceAbsent is the error stored on Snapshot.Err when the
// configured hotspot interface does not exist in sysfs.
var ErrInterfaceAbsent = errors.New("interface absent")

// Snapshot is the cached result of a successful (or last-good) refresh.
type Snapshot struct {
	At      time.Time
	Iface   string
	Clients []Client
	Err     error
}

type iwFunc func(ctx context.Context, iface string) ([]byte, error)
type leasesFunc func() ([]byte, error)
type existsFunc func(name string) bool

// Collector lazily refreshes a cached hotspot snapshot.
type Collector struct {
	iface  string
	runIw  iwFunc
	leases leasesFunc
	exists existsFunc
	clock  func() time.Time
	ttl    time.Duration

	mu              sync.Mutex
	lastAt          time.Time
	snap            atomic.Pointer[Snapshot]
	prevClientCount int
	prevAbsent      bool
}

// New returns a Collector configured from cfg.
func New(cfg config.Config) *Collector {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(cfg.SysPath, "class", "net", name))
		return err == nil
	}
	return newWithDeps(
		cfg.HotspotIf,
		defaultRunIw,
		func() ([]byte, error) { return os.ReadFile(cfg.LeasesPath) },
		exists,
		time.Now,
		cacheTTL,
	)
}

func newWithDeps(iface string, runIw iwFunc, leases leasesFunc, exists existsFunc, clock func() time.Time, ttl time.Duration) *Collector {
	c := &Collector{
		iface:  iface,
		runIw:  runIw,
		leases: leases,
		exists: exists,
		clock:  clock,
		ttl:    ttl,
	}
	c.snap.Store(&Snapshot{At: clock(), Iface: iface})
	return c
}

// Snapshot returns the cached snapshot, refreshing it if the cache is
// older than the configured TTL.
func (c *Collector) Snapshot(ctx context.Context) *Snapshot {
	c.mu.Lock()
	age := c.clock().Sub(c.lastAt)
	needRefresh := age >= c.ttl
	c.mu.Unlock()

	if needRefresh {
		c.refresh(ctx)
	}
	return c.snap.Load()
}

func (c *Collector) refresh(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.clock().Sub(c.lastAt) < c.ttl {
		return
	}

	now := c.clock()
	prev := c.snap.Load()

	if c.exists != nil && !c.exists(c.iface) {
		if !c.prevAbsent {
			log.Printf("hotspot: interface %s absent in sysfs", c.iface)
			c.prevAbsent = true
		}
		c.snap.Store(&Snapshot{
			At:    now,
			Iface: c.iface,
			Err:   ErrInterfaceAbsent,
		})
		c.lastAt = now
		return
	}
	if c.prevAbsent {
		log.Printf("hotspot: interface %s present in sysfs", c.iface)
		c.prevAbsent = false
	}

	iwCtx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	iwOut, iwErr := c.runIw(iwCtx, c.iface)
	if iwErr != nil {
		log.Printf("hotspot: iw station dump failed on %s: %v", c.iface, iwErr)
		next := &Snapshot{
			At:      now,
			Iface:   c.iface,
			Clients: prev.Clients,
			Err:     fmt.Errorf("iw: %w", iwErr),
		}
		c.snap.Store(next)
		c.lastAt = now
		return
	}

	clients := parseStationDump(iwOut)

	leasesRaw, leaseErr := c.leases()
	if leaseErr == nil {
		leases := parseLeases(leasesRaw)
		for i := range clients {
			if entry, ok := leases[clients[i].MAC]; ok {
				clients[i].IP = entry.IP
				clients[i].Hostname = entry.Hostname
			}
		}
	}

	sort.Slice(clients, func(i, j int) bool {
		return clients[i].MAC < clients[j].MAC
	})

	next := &Snapshot{
		At:      now,
		Iface:   c.iface,
		Clients: clients,
	}
	if leaseErr != nil && !errors.Is(leaseErr, os.ErrNotExist) {
		next.Err = fmt.Errorf("leases: %w", leaseErr)
	}
	if len(clients) != c.prevClientCount {
		log.Printf("hotspot: client count changed %d -> %d on %s", c.prevClientCount, len(clients), c.iface)
		c.prevClientCount = len(clients)
	}
	c.snap.Store(next)
	c.lastAt = now
}

func defaultRunIw(ctx context.Context, iface string) ([]byte, error) {
	// #nosec G204 -- iface comes from our own config, not user input.
	cmd := exec.CommandContext(ctx, "iw", "dev", iface, "station", "dump")
	return cmd.Output()
}

// Package admin collects link state and IP address for the administration
// interface (typically eth0).
package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mcs-net/pispot/dashboard/internal/config"
)

const (
	cacheTTL    = 1 * time.Second
	execTimeout = 2 * time.Second
)

// ErrInterfaceAbsent is the error stored on Snapshot.Err when the
// configured admin interface does not exist in sysfs.
var ErrInterfaceAbsent = errors.New("interface absent")

// Info is the public, flattened view of the admin interface.
type Info struct {
	Interface string
	IP        string
	Gateway   string
	Link      bool
}

// Snapshot is the cached result plus refresh error (if any).
type Snapshot struct {
	At   time.Time
	Info Info
	Err  error
}

type operstateFunc func(name string) (string, error)
type execFunc func(ctx context.Context, name string, args ...string) ([]byte, error)
type existsFunc func(name string) bool

// Collector lazily refreshes admin-interface state.
type Collector struct {
	iface     string
	operstate operstateFunc
	run       execFunc
	exists    existsFunc
	clock     func() time.Time
	ttl       time.Duration

	mu         sync.Mutex
	lastAt     time.Time
	snap       atomic.Pointer[Snapshot]
	prevLink   bool
	prevIP     string
	prevAbsent bool
}

// New returns a Collector configured from cfg.
func New(cfg config.Config) *Collector {
	operstate := func(name string) (string, error) {
		p := filepath.Join(cfg.SysPath, "class", "net", name, "operstate")
		b, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(cfg.SysPath, "class", "net", name))
		return err == nil
	}
	return newWithDeps(cfg.AdminIf, operstate, defaultRun, exists, time.Now, cacheTTL)
}

func newWithDeps(iface string, op operstateFunc, run execFunc, exists existsFunc, clock func() time.Time, ttl time.Duration) *Collector {
	c := &Collector{
		iface:     iface,
		operstate: op,
		run:       run,
		exists:    exists,
		clock:     clock,
		ttl:       ttl,
	}
	c.snap.Store(&Snapshot{At: clock(), Info: Info{Interface: iface}})
	return c
}

// Snapshot returns the cached snapshot, refreshing if older than TTL.
func (c *Collector) Snapshot(ctx context.Context) *Snapshot {
	c.mu.Lock()
	stale := c.clock().Sub(c.lastAt) >= c.ttl
	c.mu.Unlock()
	if stale {
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
			log.Printf("admin: interface %s absent in sysfs", c.iface)
			c.prevAbsent = true
		}
		c.snap.Store(&Snapshot{
			At:   now,
			Info: Info{Interface: c.iface},
			Err:  ErrInterfaceAbsent,
		})
		c.lastAt = now
		return
	}
	if c.prevAbsent {
		log.Printf("admin: interface %s present in sysfs", c.iface)
		c.prevAbsent = false
	}

	runCtx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	info := Info{Interface: c.iface}

	state, opErr := c.operstate(c.iface)
	info.Link = opErr == nil && state == "up"

	addrOut, addrErr := c.run(runCtx, "ip", "-j", "addr", "show", c.iface)
	if addrErr != nil {
		prior := prev.Info
		info.IP = prior.IP
		info.Gateway = prior.Gateway
		err := fmt.Errorf("ip addr: %w", addrErr)
		if opErr != nil {
			err = fmt.Errorf("%w; operstate: %v", err, opErr)
		}
		c.snap.Store(&Snapshot{At: now, Info: info, Err: err})
		c.lastAt = now
		return
	}
	info.IP = parseIPAddr(addrOut)

	if routeOut, routeErr := c.run(runCtx, "ip", "-j", "route", "show", "default"); routeErr == nil {
		info.Gateway = parseIPRoute(routeOut, c.iface)
	}

	if info.Link != c.prevLink || info.IP != c.prevIP {
		log.Printf("admin: %s state changed link=%v ip=%s gateway=%s", c.iface, info.Link, info.IP, info.Gateway)
		c.prevLink = info.Link
		c.prevIP = info.IP
	}

	next := &Snapshot{At: now, Info: info}
	if opErr != nil {
		next.Err = fmt.Errorf("operstate: %w", opErr)
	}
	c.snap.Store(next)
	c.lastAt = now
}

func parseIPAddr(raw []byte) string {
	var doc []struct {
		AddrInfo []struct {
			Family string `json:"family"`
			Local  string `json:"local"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	for _, iface := range doc {
		for _, a := range iface.AddrInfo {
			if a.Family == "inet" && a.Local != "" {
				return a.Local
			}
		}
	}
	return ""
}

func parseIPRoute(raw []byte, iface string) string {
	var doc []struct {
		Dst     string `json:"dst"`
		Gateway string `json:"gateway"`
		Dev     string `json:"dev"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	for _, r := range doc {
		if r.Dev == iface && r.Gateway != "" {
			return r.Gateway
		}
	}
	return ""
}

func defaultRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	// #nosec G204 -- name and args are hard-coded in the collector.
	return exec.CommandContext(ctx, name, args...).Output()
}

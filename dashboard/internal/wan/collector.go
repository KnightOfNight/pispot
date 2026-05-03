package wan

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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
// configured WAN interface does not exist in sysfs.
var ErrInterfaceAbsent = errors.New("interface absent")

// Snapshot is the cached WAN state plus the error from the most recent
// refresh attempt (or nil on success).
type Snapshot struct {
	At   time.Time
	Info Info
	Err  error
}

type execFunc func(ctx context.Context, name string, args ...string) ([]byte, error)
type existsFunc func(name string) bool
type supplicantFunc func(iface string) bool

// Collector lazily refreshes a cached WAN snapshot.
type Collector struct {
	iface      string
	run        execFunc
	exists     existsFunc
	supplicant supplicantFunc
	clock      func() time.Time
	ttl        time.Duration

	mu            sync.Mutex
	lastAt        time.Time
	snap          atomic.Pointer[Snapshot]
	prevConnected bool
	prevSSID      string
	prevAbsent    bool
}

// New returns a Collector configured from cfg.
func New(cfg config.Config) *Collector {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(cfg.SysPath, "class", "net", name))
		return err == nil
	}
	procPath := cfg.ProcPath
	supplicant := func(iface string) bool {
		matches, err := filepath.Glob(filepath.Join(procPath, "*/cmdline"))
		if err != nil {
			return false
		}
		needle := []byte("wpa_supplicant")
		ifaceBytes := []byte(iface)
		for _, m := range matches {
			data, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			if bytes.Contains(data, needle) && bytes.Contains(data, ifaceBytes) {
				return true
			}
		}
		return false
	}
	return newWithDeps(cfg.WANIf, defaultRun, exists, supplicant, time.Now, cacheTTL)
}

func newWithDeps(iface string, run execFunc, exists existsFunc, supplicant supplicantFunc, clock func() time.Time, ttl time.Duration) *Collector {
	c := &Collector{
		iface:      iface,
		run:        run,
		exists:     exists,
		supplicant: supplicant,
		clock:      clock,
		ttl:        ttl,
	}
	c.snap.Store(&Snapshot{At: clock(), Info: Info{Interface: iface}})
	return c
}

// Snapshot returns the cached snapshot, refreshing it if older than TTL.
func (c *Collector) Snapshot(ctx context.Context) *Snapshot {
	c.mu.Lock()
	age := c.clock().Sub(c.lastAt)
	stale := age >= c.ttl
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
			log.Printf("wan: interface %s absent in sysfs", c.iface)
			c.prevAbsent = true
		}
		c.snap.Store(&Snapshot{
			At:   now,
			Info: Info{Interface: c.iface, InterfacePresent: false},
			Err:  ErrInterfaceAbsent,
		})
		c.lastAt = now
		return
	}
	if c.prevAbsent {
		log.Printf("wan: interface %s present in sysfs", c.iface)
		c.prevAbsent = false
	}

	runCtx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	iwOut, iwErr := c.run(runCtx, "iw", "dev", c.iface, "link")
	if iwErr != nil {
		log.Printf("wan: iw link failed on %s: %v", c.iface, iwErr)
		c.storeFailure(now, prev, fmt.Errorf("iw: %w", iwErr))
		return
	}
	link := parseIwLink(iwOut)

	supplicantActive := c.supplicant != nil && c.supplicant(c.iface)
	info := Info{Interface: c.iface, InterfacePresent: true, SupplicantActive: supplicantActive, Connected: link.connected}
	if !link.connected {
		if c.prevConnected {
			log.Printf("wan: %s disconnected (was connected to %q)", c.iface, c.prevSSID)
			c.prevConnected = false
			c.prevSSID = ""
		}
		c.storeSuccess(now, info)
		return
	}
	if !c.prevConnected || link.ssid != c.prevSSID {
		log.Printf("wan: %s connected ssid=%q signal=%ddBm freq=%dMHz", c.iface, link.ssid, link.signalDBm, link.freqMHz)
		c.prevConnected = true
		c.prevSSID = link.ssid
	}
	info.SSID = link.ssid
	info.BSSID = link.bssid
	info.SignalDBm = link.signalDBm
	info.FreqMHz = link.freqMHz
	info.TxBitrateMbps = link.txBitrateMbps

	addrOut, addrErr := c.run(runCtx, "ip", "-j", "addr", "show", c.iface)
	if addrErr != nil {
		c.storeFailure(now, prev, fmt.Errorf("ip addr: %w", addrErr))
		return
	}
	info.IP = parseIPAddr(addrOut)

	routeOut, routeErr := c.run(runCtx, "ip", "-j", "route", "show", "default")
	if routeErr != nil {
		c.storeFailure(now, prev, fmt.Errorf("ip route: %w", routeErr))
		return
	}
	info.Gateway = parseIPRoute(routeOut, c.iface)

	c.storeSuccess(now, info)
}

func (c *Collector) storeSuccess(now time.Time, info Info) {
	c.snap.Store(&Snapshot{At: now, Info: info})
	c.lastAt = now
}

func (c *Collector) storeFailure(now time.Time, prev *Snapshot, err error) {
	info := Info{Interface: c.iface}
	if prev != nil {
		info = prev.Info
	}
	c.snap.Store(&Snapshot{At: now, Info: info, Err: err})
	c.lastAt = now
}

func defaultRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	// #nosec G204 -- name and args are hard-coded in the collector.
	return exec.CommandContext(ctx, name, args...).Output()
}

var errUnexpectedCall = errors.New("unexpected command invocation")

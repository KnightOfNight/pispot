// Package netstats samples per-interface byte counters from /proc/net/dev
// at a fixed cadence and exposes a lock-free snapshot of the most recent
// Mbps rates and cumulative totals.
package netstats

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mcs-net/pispot/dashboard/internal/config"
)

const sampleInterval = 1 * time.Second

// InterfaceStats is the public, per-interface view of a single snapshot.
type InterfaceStats struct {
	Name         string
	Up           bool
	RxMbps       float64
	TxMbps       float64
	RxTotalBytes uint64
	TxTotalBytes uint64
}

// Snapshot is an immutable, point-in-time view of all tracked interfaces.
type Snapshot struct {
	At         time.Time
	Interfaces map[string]InterfaceStats
}

type sample struct {
	rxBytes uint64
	txBytes uint64
	at      time.Time
	present bool
}

// Source supplies the raw bytes of /proc/net/dev.
type Source func() ([]byte, error)

// OperstateFunc returns the operstate string for the named interface.
type OperstateFunc func(name string) (string, error)

// Clock returns the current wall-clock time.
type Clock func() time.Time

// Collector samples interface counters on a ticker and publishes snapshots.
type Collector struct {
	ifaces    []string
	source    Source
	operstate OperstateFunc
	clock     Clock
	interval  time.Duration
	prev      map[string]sample
	snap      atomic.Pointer[Snapshot]
}

// New constructs a Collector for the interfaces named in cfg.
func New(cfg config.Config) *Collector {
	procFile := filepath.Join(cfg.ProcPath, "net", "dev")
	source := func() ([]byte, error) {
		return os.ReadFile(procFile)
	}
	operstate := func(name string) (string, error) {
		p := filepath.Join(cfg.SysPath, "class", "net", name, "operstate")
		b, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	return newWithDeps(
		filterEmpty([]string{cfg.HotspotIf, cfg.WANIf, cfg.AdminIf}),
		source,
		operstate,
		time.Now,
		sampleInterval,
	)
}

func newWithDeps(ifaces []string, source Source, operstate OperstateFunc, clock Clock, interval time.Duration) *Collector {
	c := &Collector{
		ifaces:    ifaces,
		source:    source,
		operstate: operstate,
		clock:     clock,
		interval:  interval,
		prev:      make(map[string]sample, len(ifaces)),
	}
	empty := &Snapshot{
		At:         clock(),
		Interfaces: initialInterfaces(ifaces),
	}
	c.snap.Store(empty)
	return c
}

// Run blocks, sampling every interval until ctx is done.
func (c *Collector) Run(ctx context.Context) {
	log.Printf("netstats: collector starting (interval=%s ifaces=%v)", c.interval, c.ifaces)

	c.tick()

	t := time.NewTicker(c.interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("netstats: collector stopped")
			return
		case <-t.C:
			c.tick()
		}
	}
}

// Snapshot returns the most recently published snapshot.
func (c *Collector) Snapshot() *Snapshot {
	return c.snap.Load()
}

func (c *Collector) tick() {
	now := c.clock()

	raw, err := c.source()
	current := map[string]sample{}
	if err == nil {
		parsed := parseProcNetDev(raw)
		for _, name := range c.ifaces {
			if s, ok := parsed[name]; ok {
				current[name] = sample{
					rxBytes: s.rxBytes,
					txBytes: s.txBytes,
					at:      now,
					present: true,
				}
			} else {
				current[name] = sample{at: now, present: false}
			}
		}
	} else {
		for _, name := range c.ifaces {
			current[name] = sample{at: now, present: false}
		}
	}

	ifaces := make(map[string]InterfaceStats, len(c.ifaces))
	for _, name := range c.ifaces {
		cur := current[name]
		prev, hadPrev := c.prev[name]

		stats := InterfaceStats{
			Name:         name,
			Up:           c.isUp(name),
			RxTotalBytes: cur.rxBytes,
			TxTotalBytes: cur.txBytes,
		}
		if cur.present && hadPrev && prev.present {
			stats.RxMbps = computeMbps(prev.rxBytes, cur.rxBytes, prev.at, cur.at)
			stats.TxMbps = computeMbps(prev.txBytes, cur.txBytes, prev.at, cur.at)
		}
		ifaces[name] = stats
	}

	c.prev = current
	c.snap.Store(&Snapshot{At: now, Interfaces: ifaces})
}

func (c *Collector) isUp(name string) bool {
	state, err := c.operstate(name)
	if err != nil {
		return false
	}
	return state == "up"
}

func computeMbps(prevBytes, curBytes uint64, prevAt, curAt time.Time) float64 {
	elapsed := curAt.Sub(prevAt).Seconds()
	if elapsed <= 0 {
		return 0
	}
	if curBytes < prevBytes {
		return 0
	}
	delta := curBytes - prevBytes
	return float64(delta) * 8 / 1_000_000 / elapsed
}

type parsedLine struct {
	rxBytes uint64
	txBytes uint64
}

func parseProcNetDev(raw []byte) map[string]parsedLine {
	out := make(map[string]parsedLine)
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	for sc.Scan() {
		line := sc.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		name := strings.TrimSpace(line[:colon])
		if name == "" {
			continue
		}
		fields := strings.Fields(line[colon+1:])
		if len(fields) < 16 {
			continue
		}
		var rx, tx uint64
		if _, err := fmt.Sscanf(fields[0], "%d", &rx); err != nil {
			continue
		}
		if _, err := fmt.Sscanf(fields[8], "%d", &tx); err != nil {
			continue
		}
		out[name] = parsedLine{rxBytes: rx, txBytes: tx}
	}
	return out
}

func filterEmpty(s []string) []string {
	out := s[:0:0]
	for _, v := range s {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func initialInterfaces(ifaces []string) map[string]InterfaceStats {
	out := make(map[string]InterfaceStats, len(ifaces))
	for _, name := range ifaces {
		out[name] = InterfaceStats{Name: name}
	}
	return out
}

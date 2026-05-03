// Package system collects host-level metrics suitable for a small
// system-status pane: load average, memory, SoC temperature, and an
// inferred thermal-throttling flag.
package system

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mcs-net/pispot/dashboard/internal/config"
)

const sampleInterval = 1 * time.Second

const thermalThrottleCelsius = 80.0

// Info is the public, per-snapshot view of host state.
type Info struct {
	Load1m        float64
	Load5m        float64
	Load15m       float64
	MemTotalBytes uint64
	MemUsedBytes  uint64
	TempCelsius   float64
	Throttled     bool
	UptimeSeconds float64
}

// Snapshot is a point-in-time view plus any error encountered during
// the most recent refresh.
type Snapshot struct {
	At   time.Time
	Info Info
	Err  error
}

type readerFunc func() ([]byte, error)
type thermalSelector func() (string, error)

// Collector samples /proc and /sys on a ticker and publishes snapshots.
type Collector struct {
	readLoad    readerFunc
	readMem     readerFunc
	readUptime  readerFunc
	readTemp    readerFunc
	selectTherm thermalSelector
	clock       func() time.Time
	interval    time.Duration
	snap        atomic.Pointer[Snapshot]
}

// New returns a Collector configured from cfg.
func New(cfg config.Config) *Collector {
	loadPath := filepath.Join(cfg.ProcPath, "loadavg")
	memPath := filepath.Join(cfg.ProcPath, "meminfo")
	uptimePath := filepath.Join(cfg.ProcPath, "uptime")
	thermalRoot := filepath.Join(cfg.SysPath, "class", "thermal")

	selectTherm := func() (string, error) {
		return selectThermalZone(thermalRoot)
	}

	readTemp := func() ([]byte, error) {
		path, err := selectTherm()
		if err != nil {
			return nil, err
		}
		return os.ReadFile(path)
	}

	return newWithDeps(
		func() ([]byte, error) { return os.ReadFile(loadPath) },
		func() ([]byte, error) { return os.ReadFile(memPath) },
		func() ([]byte, error) { return os.ReadFile(uptimePath) },
		readTemp,
		selectTherm,
		time.Now,
		sampleInterval,
	)
}

func newWithDeps(readLoad, readMem, readUptime, readTemp readerFunc, selectTherm thermalSelector, clock func() time.Time, interval time.Duration) *Collector {
	c := &Collector{
		readLoad:    readLoad,
		readMem:     readMem,
		readUptime:  readUptime,
		readTemp:    readTemp,
		selectTherm: selectTherm,
		clock:       clock,
		interval:    interval,
	}
	c.snap.Store(&Snapshot{At: clock()})
	return c
}

// Run blocks, sampling every interval until ctx is done.
func (c *Collector) Run(ctx context.Context) {
	log.Printf("system: collector starting (interval=%s)", c.interval)

	c.tick()

	t := time.NewTicker(c.interval)
	defer t.Stop()

	var prevThrottled bool
	for {
		select {
		case <-ctx.Done():
			log.Printf("system: collector stopped")
			return
		case <-t.C:
			c.tick()
			snap := c.Snapshot()
			if snap != nil && snap.Info.Throttled != prevThrottled {
				if snap.Info.Throttled {
					log.Printf("system: THROTTLED temp=%.1f°C (threshold=%.0f°C)",
						snap.Info.TempCelsius, thermalThrottleCelsius)
				} else {
					log.Printf("system: throttle cleared temp=%.1f°C", snap.Info.TempCelsius)
				}
				prevThrottled = snap.Info.Throttled
			}
		}
	}
}

// Snapshot returns the most recently published snapshot.
func (c *Collector) Snapshot() *Snapshot {
	return c.snap.Load()
}

func (c *Collector) tick() {
	now := c.clock()
	var info Info
	var errs []string

	if raw, err := c.readLoad(); err != nil {
		errs = append(errs, fmt.Sprintf("loadavg: %v", err))
	} else {
		l1, l5, l15, err := parseLoadavg(raw)
		if err != nil {
			errs = append(errs, fmt.Sprintf("loadavg: %v", err))
		} else {
			info.Load1m, info.Load5m, info.Load15m = l1, l5, l15
		}
	}

	if raw, err := c.readMem(); err != nil {
		errs = append(errs, fmt.Sprintf("meminfo: %v", err))
	} else {
		total, used, err := parseMeminfo(raw)
		if err != nil {
			errs = append(errs, fmt.Sprintf("meminfo: %v", err))
		} else {
			info.MemTotalBytes = total
			info.MemUsedBytes = used
		}
	}

	if raw, err := c.readTemp(); err != nil {
		errs = append(errs, fmt.Sprintf("temp: %v", err))
	} else {
		temp, err := parseTempMillideg(raw)
		if err != nil {
			errs = append(errs, fmt.Sprintf("temp: %v", err))
		} else {
			info.TempCelsius = temp
			info.Throttled = temp >= thermalThrottleCelsius
		}
	}

	if raw, err := c.readUptime(); err != nil {
		errs = append(errs, fmt.Sprintf("uptime: %v", err))
	} else {
		u, err := parseUptime(raw)
		if err != nil {
			errs = append(errs, fmt.Sprintf("uptime: %v", err))
		} else {
			info.UptimeSeconds = u
		}
	}

	snap := &Snapshot{At: now, Info: info}
	if len(errs) > 0 {
		snap.Err = fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	c.snap.Store(snap)
}

// parseUptime extracts the system uptime in seconds from /proc/uptime.
// Format: "<uptime_seconds> <idle_seconds>". Only the first field is used.
func parseUptime(raw []byte) (float64, error) {
	fields := strings.Fields(string(raw))
	if len(fields) < 1 {
		return 0, fmt.Errorf("empty uptime reading")
	}
	u, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse uptime: %w", err)
	}
	return u, nil
}

func parseLoadavg(raw []byte) (l1, l5, l15 float64, err error) {
	fields := strings.Fields(string(raw))
	if len(fields) < 3 {
		return 0, 0, 0, fmt.Errorf("expected 3+ fields, got %d", len(fields))
	}
	l1, err = strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("1m: %w", err)
	}
	l5, err = strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("5m: %w", err)
	}
	l15, err = strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("15m: %w", err)
	}
	return l1, l5, l15, nil
}

func parseMeminfo(raw []byte) (total, used uint64, err error) {
	var totalKB, availKB uint64
	var haveTotal, haveAvail bool
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			if v, ok := parseMeminfoValue(line); ok {
				totalKB = v
				haveTotal = true
			}
		case strings.HasPrefix(line, "MemAvailable:"):
			if v, ok := parseMeminfoValue(line); ok {
				availKB = v
				haveAvail = true
			}
		}
		if haveTotal && haveAvail {
			break
		}
	}
	if !haveTotal {
		return 0, 0, fmt.Errorf("MemTotal not found")
	}
	if !haveAvail {
		return 0, 0, fmt.Errorf("MemAvailable not found")
	}
	if availKB > totalKB {
		availKB = totalKB
	}
	return totalKB * 1024, (totalKB - availKB) * 1024, nil
}

func parseMeminfoValue(line string) (uint64, bool) {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return 0, false
	}
	fields := strings.Fields(line[colon+1:])
	if len(fields) == 0 {
		return 0, false
	}
	v, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func parseTempMillideg(raw []byte) (float64, error) {
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return 0, fmt.Errorf("empty temperature reading")
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return float64(n) / 1000.0, nil
}

func selectThermalZone(root string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(root, "thermal_zone*", "type"))
	if err == nil && len(matches) > 0 {
		var cpuContains string
		for _, m := range matches {
			b, rErr := os.ReadFile(m)
			if rErr != nil {
				continue
			}
			t := strings.TrimSpace(string(b))
			dir := filepath.Dir(m)
			temp := filepath.Join(dir, "temp")
			switch {
			case t == "cpu-thermal":
				return temp, nil
			case t == "bcm2712_thermal":
				if cpuContains == "" {
					cpuContains = temp
				}
			case strings.Contains(strings.ToLower(t), "cpu"):
				if cpuContains == "" {
					cpuContains = temp
				}
			}
		}
		if cpuContains != "" {
			return cpuContains, nil
		}
	}

	fallback := filepath.Join(root, "thermal_zone0", "temp")
	if _, err := os.Stat(fallback); err != nil {
		return "", fmt.Errorf("no usable thermal zone under %s: %w", root, err)
	}
	return fallback, nil
}

package admin

import (
	"context"
	"errors"
	"io/fs"
	"sync/atomic"
	"testing"
	"time"
)

const sampleIPAddr = `[
  {
    "ifname": "eth0",
    "addr_info": [
      {"family": "inet",  "local": "10.0.0.5", "prefixlen": 24},
      {"family": "inet6", "local": "fe80::1",   "prefixlen": 64}
    ]
  }
]`

const sampleIPAddrEmpty = `[]`

const sampleIPRoute = `[
  {"dst": "default", "gateway": "10.0.0.1",   "dev": "eth0"},
  {"dst": "default", "gateway": "192.168.1.1", "dev": "wlan1"}
]`

const sampleIPRouteNoMatch = `[
  {"dst": "default", "gateway": "192.168.1.1", "dev": "wlan1"}
]`

func dispatchRun(onAddr, onRoute func() ([]byte, error)) execFunc {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "ip" && len(args) >= 2 {
			switch args[1] {
			case "addr":
				return onAddr()
			case "route":
				return onRoute()
			}
		}
		return nil, errors.New("unexpected command")
	}
}

func TestOperstateMapping(t *testing.T) {
	run := dispatchRun(
		func() ([]byte, error) { return []byte(sampleIPAddr), nil },
		func() ([]byte, error) { return []byte(sampleIPRoute), nil },
	)
	cases := []struct {
		state    string
		readErr  error
		wantLink bool
		wantErr  bool
	}{
		{"up", nil, true, false},
		{"down", nil, false, false},
		{"unknown", nil, false, false},
		{"dormant", nil, false, false},
		{"", fs.ErrNotExist, false, true},
	}
	exists := func(name string) bool { return true }
	for _, tc := range cases {
		t.Run(tc.state, func(t *testing.T) {
			op := func(name string) (string, error) {
				return tc.state, tc.readErr
			}
			c := newWithDeps("eth0", op, run, exists, time.Now, 1*time.Millisecond)
			snap := c.Snapshot(context.Background())
			if snap.Info.Link != tc.wantLink {
				t.Errorf("state=%q: Link got %v, want %v", tc.state, snap.Info.Link, tc.wantLink)
			}
			if (snap.Err != nil) != tc.wantErr {
				t.Errorf("state=%q: Err=%v, wantErr=%v", tc.state, snap.Err, tc.wantErr)
			}
		})
	}
}

func TestParseIPHelpers(t *testing.T) {
	if got := parseIPAddr([]byte(sampleIPAddr)); got != "10.0.0.5" {
		t.Errorf("addr single: got %q, want 10.0.0.5", got)
	}
	if got := parseIPAddr([]byte(sampleIPAddrEmpty)); got != "" {
		t.Errorf("addr empty: got %q, want empty", got)
	}
	if got := parseIPAddr([]byte("garbage")); got != "" {
		t.Errorf("addr garbage: got %q, want empty", got)
	}

	if got := parseIPRoute([]byte(sampleIPRoute), "eth0"); got != "10.0.0.1" {
		t.Errorf("route match: got %q, want 10.0.0.1", got)
	}
	if got := parseIPRoute([]byte(sampleIPRouteNoMatch), "eth0"); got != "" {
		t.Errorf("route no match: got %q, want empty", got)
	}
	if got := parseIPRoute([]byte("garbage"), "eth0"); got != "" {
		t.Errorf("route garbage: got %q, want empty", got)
	}
}

func TestCollectorTTLAndLastGood(t *testing.T) {
	var addrCalls, routeCalls atomic.Int64
	run := dispatchRun(
		func() ([]byte, error) { addrCalls.Add(1); return []byte(sampleIPAddr), nil },
		func() ([]byte, error) { routeCalls.Add(1); return []byte(sampleIPRoute), nil },
	)
	op := func(name string) (string, error) { return "up", nil }

	var now atomic.Int64
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()
	now.Store(base)
	clock := func() time.Time { return time.Unix(0, now.Load()) }

	ttl := 5 * time.Second
	exists := func(name string) bool { return true }
	c := newWithDeps("eth0", op, run, exists, clock, ttl)

	first := c.Snapshot(context.Background())
	if !first.Info.Link || first.Info.IP != "10.0.0.5" {
		t.Fatalf("first: got %+v, want Link=true IP=10.0.0.5", first.Info)
	}
	if first.Info.Gateway != "10.0.0.1" {
		t.Fatalf("first: got Gateway=%q, want 10.0.0.1", first.Info.Gateway)
	}
	for i := 0; i < 20; i++ {
		_ = c.Snapshot(context.Background())
	}
	if addrCalls.Load() != 1 || routeCalls.Load() != 1 {
		t.Errorf("within-TTL: expected 1 addr + 1 route call, got addr=%d route=%d",
			addrCalls.Load(), routeCalls.Load())
	}

	c.run = dispatchRun(
		func() ([]byte, error) { return []byte(sampleIPAddr), nil },
		func() ([]byte, error) { return nil, errors.New("route boom") },
	)
	now.Add(int64(ttl + time.Second))
	snap := c.Snapshot(context.Background())
	if snap.Err != nil {
		t.Errorf("route-only failure should be non-fatal; got Err=%v", snap.Err)
	}
	if snap.Info.Gateway != "" {
		t.Errorf("route-only failure: expected blank Gateway, got %q", snap.Info.Gateway)
	}
	if snap.Info.IP != "10.0.0.5" {
		t.Errorf("route-only failure: IP should remain, got %q", snap.Info.IP)
	}

	c.run = dispatchRun(
		func() ([]byte, error) { return []byte(sampleIPAddr), nil },
		func() ([]byte, error) { return []byte(sampleIPRoute), nil },
	)
	now.Add(int64(ttl + time.Second))
	_ = c.Snapshot(context.Background())
	addrErr := errors.New("ip boom")
	c.run = dispatchRun(
		func() ([]byte, error) { return nil, addrErr },
		func() ([]byte, error) { return []byte(sampleIPRoute), nil },
	)
	now.Add(int64(ttl + time.Second))
	snap = c.Snapshot(context.Background())
	if snap.Err == nil || !errors.Is(snap.Err, addrErr) {
		t.Errorf("expected wrapped ip-addr error, got %v", snap.Err)
	}
	if snap.Info.IP != "10.0.0.5" {
		t.Errorf("last-good IP lost: got %q, want 10.0.0.5", snap.Info.IP)
	}
	if snap.Info.Gateway != "10.0.0.1" {
		t.Errorf("last-good Gateway lost: got %q, want 10.0.0.1", snap.Info.Gateway)
	}
	if !snap.Info.Link {
		t.Errorf("Link should still be true on ip-addr-only failure")
	}
}

func TestCollectorInterfaceAbsent(t *testing.T) {
	op := func(name string) (string, error) {
		t.Errorf("operstate should not be called when interface is absent")
		return "", nil
	}
	run := dispatchRun(
		func() ([]byte, error) {
			t.Errorf("ip addr should not be called when interface is absent")
			return nil, nil
		},
		func() ([]byte, error) {
			t.Errorf("ip route should not be called when interface is absent")
			return nil, nil
		},
	)
	exists := func(name string) bool { return false }

	c := newWithDeps("eth0", op, run, exists, time.Now, 1*time.Millisecond)
	snap := c.Snapshot(context.Background())
	if !errors.Is(snap.Err, ErrInterfaceAbsent) {
		t.Errorf("expected ErrInterfaceAbsent, got %v", snap.Err)
	}
	if snap.Info.Link {
		t.Errorf("absent: expected Link=false; got %+v", snap.Info)
	}
	if snap.Info.IP != "" || snap.Info.Gateway != "" {
		t.Errorf("absent: expected empty IP/Gateway, got %+v", snap.Info)
	}
}

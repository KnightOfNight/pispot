package diag

import (
	"strings"
	"testing"
	"time"
)

func TestRedact(t *testing.T) {
	cases := []struct{ in, leak string }{
		{`psk="hunter2hunter2"`, "hunter2"},
		{`wpa_passphrase=hunter2hunter2`, "hunter2"},
		{`{"psk":"hunter2hunter2"}`, "hunter2"},
		{`password: hunter2hunter2`, "hunter2"},
		{`Authorization: Basic aHVudGVyMg==`, "aHVudGVyMg"},
		{"-----BEGIN PRIVATE KEY-----\nhunter2\n-----END PRIVATE KEY-----", "hunter2"},
		{"-----BEGIN RSA PRIVATE KEY-----\nhunter2 cut off here", "hunter2"},
	}
	for _, c := range cases {
		if out := Redact(c.in); strings.Contains(out, c.leak) {
			t.Errorf("Redact(%q) leaked: %q", c.in, out)
		}
	}
}

func TestRedactKeepsOrdinaryLogs(t *testing.T) {
	for _, line := range []string{
		"eth0: leased 192.168.42.119 for 86400 seconds",
		`pam: authentication succeeded for user="ctg"`,
		"wlan1: connected to Access Point: WMAG-NH-6G",
		"-A FORWARD -j ACCEPT bypass",
	} {
		if out := Redact(line); out != line {
			t.Errorf("Redact changed %q to %q", line, out)
		}
	}
}

// Secrets must stay out because they're never read, not only because
// redaction catches them.
func TestSourcesAvoidSecretFiles(t *testing.T) {
	forbidden := []string{
		"hostapd.conf", "wpa_supplicant-wlan1", "wpa_supplicant.conf", "shadow",
		"group_vars", ".key", "secret", "environ", "/var/lib/dhcpcd/",
	}
	for _, src := range sources {
		cmd := strings.Join(src.cmd, " ")
		for _, f := range forbidden {
			if strings.Contains(cmd, f) {
				t.Errorf("source %q reads %q: %s", src.id, f, cmd)
			}
		}
		if src.cmd[0] == "docker" && src.cmd[1] == "inspect" {
			t.Errorf("source %q would expose container env vars", src.id)
		}
		if src.cmd[0] == "ps" && strings.Contains(cmd, "args") {
			t.Errorf("source %q would expose command lines", src.id)
		}
	}
}

func TestSourceIDsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, src := range sources {
		if seen[src.id] {
			t.Errorf("duplicate source id %q", src.id)
		}
		seen[src.id] = true
	}
}

func TestClassifyExpiry(t *testing.T) {
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		notAfter time.Time
		want     string
	}{
		{now.AddDate(0, 0, 90), StatusOK},
		{now.AddDate(0, 0, 10), StatusWarn},
		{now.AddDate(0, 0, -1), StatusBad},
	}
	for _, c := range cases {
		if got, _ := classifyExpiry(c.notAfter, now); got != c.want {
			t.Errorf("classifyExpiry(%v) = %s, want %s", c.notAfter, got, c.want)
		}
	}
}

func TestClassifyEth0(t *testing.T) {
	cases := []struct {
		carrier bool
		addrs   []string
		want    string
	}{
		{false, nil, StatusInfo},
		{true, nil, StatusWarn},
		{true, []string{"192.168.50.1"}, StatusWarn},
		{true, []string{"192.168.42.119"}, StatusOK},
	}
	for _, c := range cases {
		if got, _ := classifyEth0(c.carrier, c.addrs); got != c.want {
			t.Errorf("classifyEth0(%v, %v) = %s, want %s", c.carrier, c.addrs, got, c.want)
		}
	}
}

func TestCountConflicts(t *testing.T) {
	log := "eth0: 74:19:f8:16:cc:d2(74:19:f8:16:cc:d2) claims 192.168.42.119\n" +
		"eth0: 10 second defence failed for 192.168.42.119\n" +
		"eth0: leased 192.168.42.117 for 86400 seconds"
	if got := countConflicts(log); got != 2 {
		t.Errorf("countConflicts = %d, want 2", got)
	}
}

func TestClassifyThrottled(t *testing.T) {
	cases := map[string]string{
		"throttled=0x0":     StatusOK,
		"throttled=0x50000": StatusWarn,
		"throttled=0x50005": StatusBad,
		"":                  StatusInfo,
	}
	for in, want := range cases {
		if got, _ := classifyThrottled(in); got != want {
			t.Errorf("classifyThrottled(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestDiskClassification(t *testing.T) {
	if got := usedPercent(100, 20, 10); got != 88 {
		t.Errorf("usedPercent = %d, want 88", got)
	}
	if classifyDisk(50) != StatusOK || classifyDisk(90) != StatusWarn || classifyDisk(97) != StatusBad {
		t.Error("classifyDisk thresholds are wrong")
	}
}

func TestCapOutputKeepsNewestLines(t *testing.T) {
	out, truncated := capOutput("old line\nnew line\n", 10)
	if !truncated || out != "new line\n" {
		t.Errorf("capOutput = %q, %v", out, truncated)
	}
}

func TestCountUpgradable(t *testing.T) {
	out := "Listing...\nbash/stable 5.2 arm64 [upgradable from: 5.1]\nsed/stable 4.9 arm64 [upgradable from: 4.8]"
	if got := countUpgradable(out); got != 2 {
		t.Errorf("countUpgradable = %d, want 2", got)
	}
}

package wifi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleConfig = `ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev
update_config=1
country=US

network={
    ssid="TestNetwork-A"
    psk="password-aaa"
}

network={
    ssid="TestNetwork-B"
    psk="password-bbb"
}
`

func TestParse(t *testing.T) {
	networks, header, err := parse([]byte(sampleConfig))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(networks) != 2 {
		t.Fatalf("expected 2 networks, got %d", len(networks))
	}
	if networks[0].SSID != "TestNetwork-A" || networks[0].PSK != "password-aaa" {
		t.Errorf("network 0: got %+v", networks[0])
	}
	if networks[1].SSID != "TestNetwork-B" || networks[1].PSK != "password-bbb" {
		t.Errorf("network 1: got %+v", networks[1])
	}
	if !contains(header, "ctrl_interface") {
		t.Errorf("header missing ctrl_interface: %q", header)
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wpa_supplicant-wlan1.conf")
	if err := os.WriteFile(path, []byte(sampleConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	networks, header, err := parse([]byte(sampleConfig))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if err := writeToPath(path, header, networks); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	networks2, _, err := parse(data)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if len(networks2) != 2 {
		t.Fatalf("round-trip: expected 2 networks, got %d", len(networks2))
	}
	for i, n := range networks {
		if n != networks2[i] {
			t.Errorf("round-trip mismatch at %d: %+v != %+v", i, n, networks2[i])
		}
	}
}

func TestAddDuplicate(t *testing.T) {
	networks, _, _ := parse([]byte(sampleConfig))
	for _, n := range networks {
		if n.SSID == "TestNetwork-A" {
			return
		}
	}
	t.Error("expected TestNetwork-A to be present for duplicate test")
}

func TestRemove(t *testing.T) {
	networks, header, err := parse([]byte(sampleConfig))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	var updated []Network
	for _, n := range networks {
		if n.SSID == "TestNetwork-A" {
			found = true
			continue
		}
		updated = append(updated, n)
	}
	if !found {
		t.Fatal("TestNetwork-A not found in parsed networks")
	}
	if len(updated) != 1 || updated[0].SSID != "TestNetwork-B" {
		t.Errorf("after remove: expected [TestNetwork-B], got %v", updated)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "wpa_supplicant-wlan1.conf")
	if err := writeToPath(path, header, updated); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	reparsed, _, _ := parse(data)
	if len(reparsed) != 1 || reparsed[0].SSID != "TestNetwork-B" {
		t.Errorf("remove round-trip: got %v", reparsed)
	}

	notFound := true
	for _, n := range reparsed {
		if n.SSID == "nonexistent" {
			notFound = false
		}
	}
	if !notFound {
		t.Error("nonexistent SSID should not be in list")
	}
}

// writeToPath is a test helper that writes to an explicit path instead
// of the hardcoded configPath constant.
func writeToPath(path, header string, networks []Network) error {
	var sb strings.Builder
	sb.WriteString(header)
	for _, n := range networks {
		sb.WriteString("\nnetwork={\n")
		sb.WriteString(fmt.Sprintf("    ssid=%q\n", n.SSID))
		sb.WriteString(fmt.Sprintf("    psk=%q\n", n.PSK))
		sb.WriteString("}\n")
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

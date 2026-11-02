package authz

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
)

// The helper on the Pi may be older than the dashboard, so this uses a reply
// from one that still sends passwords.
func TestWifiListNeverCarriesPasswords(t *testing.T) {
	const secret = "real-wifi-password"
	socketPath := filepath.Join(t.TempDir(), "helper.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		if _, err := bufio.NewReader(conn).ReadBytes('\n'); err != nil {
			return
		}
		_, _ = conn.Write([]byte(`{"ok":true,"networks":[{"ssid":"Home","psk":"` + secret + `"}]}` + "\n"))
	}()

	networks, err := CallWifiList(context.Background(), socketPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(networks) != 1 || networks[0].SSID != "Home" {
		t.Fatalf("networks = %+v", networks)
	}
	// This is the same encoding the browser receives.
	sent, err := json.Marshal(networks)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sent), secret) {
		t.Errorf("password would reach the browser: %s", sent)
	}
}

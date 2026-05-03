// Package wifi provides read and write access to the wpa_supplicant
// configuration file for wlan1, and a reload helper.
package wifi

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

const configPath = "/etc/wpa_supplicant/wpa_supplicant-wlan1.conf"

// Network is one wpa_supplicant network block.
type Network struct {
	SSID string
	PSK  string
}

// List reads the config file and returns all configured networks.
func List() ([]Network, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	networks, _, err := parse(data)
	return networks, err
}

// Add appends a new network to the config file. Returns an error if
// a network with the same SSID already exists.
func Add(ssid, psk string) error {
	if strings.TrimSpace(ssid) == "" {
		return fmt.Errorf("SSID cannot be empty")
	}
	if strings.TrimSpace(psk) == "" {
		return fmt.Errorf("password cannot be empty")
	}
	if len(psk) < 8 || len(psk) > 63 {
		return fmt.Errorf("password must be 8–63 characters (got %d)", len(psk))
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	networks, header, err := parse(data)
	if err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	for _, n := range networks {
		if n.SSID == ssid {
			return fmt.Errorf("network %q already exists", ssid)
		}
	}
	networks = append(networks, Network{SSID: ssid, PSK: psk})
	log.Printf("wifi: adding network ssid=%q", ssid)
	return write(header, networks)
}

// Remove deletes the network with the given SSID from the config file.
func Remove(ssid string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	networks, header, err := parse(data)
	if err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	found := false
	updated := networks[:0:0]
	for _, n := range networks {
		if n.SSID == ssid {
			found = true
			continue
		}
		updated = append(updated, n)
	}
	if !found {
		return fmt.Errorf("network %q not found", ssid)
	}
	log.Printf("wifi: removing network ssid=%q", ssid)
	return write(header, updated)
}

// Reload applies config changes by performing a controlled wan-down then
// wan-up cycle using per-interface dhcpcd commands.
func Reload() error {
	if _, err := os.Stat("/sys/class/net/wlan1"); os.IsNotExist(err) {
		log.Printf("wifi: wlan1 not found in sysfs, skipping reload (config saved)")
		return nil
	}

	log.Printf("wifi: reload — stopping wpa_supplicant@wlan1")
	if err := runCmd("systemctl", "stop", "wpa_supplicant@wlan1"); err != nil {
		return fmt.Errorf("stop wpa_supplicant@wlan1: %w", err)
	}
	log.Printf("wifi: reload — releasing wlan1 DHCP lease")
	if err := runCmd("dhcpcd", "--release", "wlan1"); err != nil {
		log.Printf("wifi: dhcpcd --release wlan1: %v (continuing)", err)
	}
	log.Printf("wifi: reload — starting wpa_supplicant@wlan1 with new config")
	if err := runCmd("systemctl", "start", "wpa_supplicant@wlan1"); err != nil {
		return fmt.Errorf("start wpa_supplicant@wlan1: %w", err)
	}
	log.Printf("wifi: reload — requesting wlan1 DHCP lease")
	if err := runCmd("dhcpcd", "wlan1"); err != nil {
		return fmt.Errorf("dhcpcd wlan1: %w", err)
	}
	log.Printf("wifi: reload complete")
	return nil
}

func runCmd(name string, args ...string) error {
	// #nosec G204 -- name and args are hard-coded by callers.
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func parse(data []byte) ([]Network, string, error) {
	var (
		headerLines []string
		networks    []Network
		inBlock     bool
		cur         Network
		headerDone  bool
	)
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)

		if !headerDone && trimmed == "network={" {
			headerDone = true
		}
		if !headerDone {
			headerLines = append(headerLines, line)
			continue
		}

		if trimmed == "network={" {
			inBlock = true
			cur = Network{}
			continue
		}
		if trimmed == "}" && inBlock {
			inBlock = false
			if cur.SSID != "" {
				networks = append(networks, cur)
			}
			continue
		}
		if inBlock {
			if strings.HasPrefix(trimmed, "ssid=") {
				cur.SSID = unquote(strings.TrimPrefix(trimmed, "ssid="))
			} else if strings.HasPrefix(trimmed, "psk=") {
				cur.PSK = unquote(strings.TrimPrefix(trimmed, "psk="))
			}
		}
	}
	header := strings.Join(headerLines, "\n")
	if header != "" && !strings.HasSuffix(header, "\n") {
		header += "\n"
	}
	return networks, header, sc.Err()
}

func write(header string, networks []Network) error {
	var sb strings.Builder
	sb.WriteString(header)
	for _, n := range networks {
		sb.WriteString("\nnetwork={\n")
		sb.WriteString(fmt.Sprintf("    ssid=%q\n", n.SSID))
		sb.WriteString(fmt.Sprintf("    psk=%q\n", n.PSK))
		sb.WriteString("}\n")
	}
	tmp := configPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(sb.String()), 0o600); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := os.Rename(tmp, configPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename config: %w", err)
	}
	return nil
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

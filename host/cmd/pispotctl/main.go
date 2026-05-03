// Command pispotctl is the pispot host management tool.
//
// Usage:
//
//	pispotctl <command> [args]
//
// Commands:
//
//	wan-up        Start wpa_supplicant@wlan1 and request wlan1 DHCP lease
//	wan-down      Stop wpa_supplicant@wlan1 and release wlan1 DHCP lease
//	status        Show current service and network status
//	wifi list     List configured WAN WiFi networks
//	logs          Dump all pispot-related logs
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

const socketPath = "/run/pispot/pispot-authd.sock"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "wan-up":
		requireRoot("wan-up")
		if err := wanUp(); err != nil {
			fmt.Fprintf(os.Stderr, "wan-up: %v\n", err)
			os.Exit(1)
		}
	case "wan-down":
		requireRoot("wan-down")
		if err := wanDown(); err != nil {
			fmt.Fprintf(os.Stderr, "wan-down: %v\n", err)
			os.Exit(1)
		}
	case "status":
		requireRoot("status")
		if err := status(); err != nil {
			fmt.Fprintf(os.Stderr, "status: %v\n", err)
			os.Exit(1)
		}
	case "wifi":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "usage: pispotctl wifi <list>\n")
			os.Exit(1)
		}
		switch os.Args[2] {
		case "list":
			requireRoot("wifi list")
			if err := wifiList(); err != nil {
				fmt.Fprintf(os.Stderr, "wifi list: %v\n", err)
				os.Exit(1)
			}
		default:
			fmt.Fprintf(os.Stderr, "unknown wifi subcommand: %q\n", os.Args[2])
			os.Exit(1)
		}
	case "logs":
		requireRoot("logs")
		if err := logs(); err != nil {
			fmt.Fprintf(os.Stderr, "logs: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %q\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `pispotctl — pispot host management tool

Usage: pispotctl <command> [args]

Commands:
  wan-up        Start wpa_supplicant@wlan1 and request wlan1 DHCP lease
  wan-down      Stop wpa_supplicant@wlan1 and release wlan1 DHCP lease
  status        Show current service and network status
  wifi list     List configured WAN WiFi networks (passwords masked)
  logs          Dump all pispot-related logs
`)
}

func requireRoot(cmd string) {
	if os.Getuid() != 0 {
		fmt.Fprintf(os.Stderr, "%s requires root\n", cmd)
		os.Exit(1)
	}
}

// --- wan-up / wan-down -------------------------------------------------------

func wanUp() error {
	if _, err := os.Stat("/sys/class/net/wlan1"); os.IsNotExist(err) {
		return fmt.Errorf("wlan1 does not exist")
	}
	if _, err := os.Stat("/sys/class/net/wlan1/wireless"); os.IsNotExist(err) {
		return fmt.Errorf("wlan1 exists but is not a wireless interface")
	}
	if err := run("systemctl", "start", "wpa_supplicant@wlan1"); err != nil {
		return fmt.Errorf("start wpa_supplicant@wlan1: %w", err)
	}
	if err := run("dhcpcd", "wlan1"); err != nil {
		return fmt.Errorf("dhcpcd wlan1: %w", err)
	}
	fmt.Println("started wpa_supplicant@wlan1 and requested wlan1 DHCP lease")
	return nil
}

func wanDown() error {
	if err := run("systemctl", "stop", "wpa_supplicant@wlan1"); err != nil {
		return fmt.Errorf("stop wpa_supplicant@wlan1: %w", err)
	}
	if err := run("dhcpcd", "--release", "wlan1"); err != nil {
		return fmt.Errorf("dhcpcd --release wlan1: %w", err)
	}
	fmt.Println("stopped wpa_supplicant@wlan1 and released wlan1 DHCP lease")
	return nil
}

// --- status ------------------------------------------------------------------

func status() error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	fmt.Printf("pispotctl status — %s\n", now)

	services := []string{
		"pispot-authd",
		"docker",
		"wpa_supplicant@wlan1",
		"dhcpcd",
		"hostapd",
		"dnsmasq",
	}

	fmt.Println("\nServices:")
	for _, svc := range services {
		out, _ := exec.Command("systemctl", "is-active", svc).Output()
		state := strings.TrimSpace(string(out))
		marker := "  "
		if state == "active" {
			marker = "✓ "
		} else {
			marker = "✗ "
		}
		fmt.Printf("  %s%-35s %s\n", marker, svc, state)
	}

	fmt.Println("\nDocker:")
	runSection("docker ps -a", "docker", "ps", "-a")

	fmt.Println("\nNetwork:")
	runSection("ip addr", "ip", "addr")
	runSection("ip route", "ip", "route")

	fmt.Println("\nSocket:")
	if _, err := os.Stat("/run/pispot/pispot-authd.sock"); err == nil {
		fmt.Println("  /run/pispot/pispot-authd.sock  present")
	} else {
		fmt.Println("  /run/pispot/pispot-authd.sock  MISSING")
	}

	return nil
}

// --- wifi list ---------------------------------------------------------------

// socketRequest is the JSON envelope sent to pispot-authd.
type socketRequest struct {
	Op string `json:"op"`
}

// socketResponse is the JSON envelope received from pispot-authd.
type socketResponse struct {
	Ok       bool            `json:"ok"`
	Error    string          `json:"error,omitempty"`
	Networks []socketNetwork `json:"networks,omitempty"`
}

type socketNetwork struct {
	SSID string `json:"ssid"`
	PSK  string `json:"psk"`
}

func wifiList() error {
	conn, err := net.DialTimeout("unix", socketPath, 3*time.Second)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", socketPath, err)
	}
	defer conn.Close()

	req := socketRequest{Op: "wifi_list"}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return fmt.Errorf("send request: %w", err)
	}

	var resp socketResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if !resp.Ok {
		return fmt.Errorf("daemon error: %s", resp.Error)
	}

	if len(resp.Networks) == 0 {
		fmt.Println("No WiFi networks configured.")
		return nil
	}

	fmt.Printf("%-3s  %-40s  %s\n", "#", "SSID", "Password")
	fmt.Printf("%-3s  %-40s  %s\n", "---", strings.Repeat("-", 40), "--------")
	for i, n := range resp.Networks {
		psk := "••••••••"
		_ = n.PSK // PSK received but always masked in output
		fmt.Printf("%-3d  %-40s  %s\n", i+1, n.SSID, psk)
	}
	return nil
}

// --- logs --------------------------------------------------------------------

func logs() error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	sep := strings.Repeat("=", 72)
	fmt.Printf("pispotctl logs — collected at %s\n", now)

	sections := []struct {
		title string
		cmd   []string
	}{
		{"systemctl status pispot-authd", []string{"systemctl", "status", "pispot-authd", "--no-pager", "--full"}},
		{"systemctl status docker", []string{"systemctl", "status", "docker", "--no-pager", "--full"}},
		{"systemctl status wpa_supplicant@wlan1", []string{"systemctl", "status", "wpa_supplicant@wlan1", "--no-pager", "--full"}},
		{"systemctl status dhcpcd", []string{"systemctl", "status", "dhcpcd", "--no-pager", "--full"}},
		{"systemctl status hostapd", []string{"systemctl", "status", "hostapd", "--no-pager", "--full"}},
		{"systemctl status dnsmasq", []string{"systemctl", "status", "dnsmasq", "--no-pager", "--full"}},
		{"journalctl -u pispot-authd (last 200 lines)", []string{"journalctl", "-u", "pispot-authd", "--no-pager", "-n", "200"}},
		{"journalctl -u wpa_supplicant@wlan1 (last 100 lines)", []string{"journalctl", "-u", "wpa_supplicant@wlan1", "--no-pager", "-n", "100"}},
		{"journalctl -u dhcpcd (last 100 lines)", []string{"journalctl", "-u", "dhcpcd", "--no-pager", "-n", "100"}},
		{"journalctl -u docker (last 50 lines)", []string{"journalctl", "-u", "docker", "--no-pager", "-n", "50"}},
		{"docker ps -a", []string{"docker", "ps", "-a"}},
		{"docker logs pispot-ui (last 200 lines)", []string{"docker", "logs", "pispot-ui", "--tail", "200"}},
		{"ip addr", []string{"ip", "addr"}},
		{"ip route", []string{"ip", "route"}},
		{"ss -lx (Unix sockets listening)", []string{"ss", "-lx"}},
		{"ls -la /run/pispot/", []string{"ls", "-la", "/run/pispot/"}},
		{"ls -la /usr/local/bin/pispot-authd /etc/pispot/", []string{"ls", "-la", "/usr/local/bin/pispot-authd", "/etc/pispot/"}},
		{"cat /etc/pispot/authd.conf", []string{"cat", "/etc/pispot/authd.conf"}},
		{"cat /etc/pam.d/pispot", []string{"cat", "/etc/pam.d/pispot"}},
		{"cat /etc/systemd/system/pispot-authd.service", []string{"cat", "/etc/systemd/system/pispot-authd.service"}},
		{"ls -la /etc/pispot/certs/", []string{"ls", "-la", "/etc/pispot/certs/"}},
		{"openssl x509 -in /etc/pispot/certs/fullchain.pem -noout -subject -issuer -dates",
			[]string{"openssl", "x509", "-in", "/etc/pispot/certs/fullchain.pem", "-noout", "-subject", "-issuer", "-dates"}},
	}

	for _, s := range sections {
		fmt.Printf("\n%s\n  %s\n%s\n", sep, s.title, sep)
		// #nosec G204 — all commands are hard-coded above, not user input.
		out, err := exec.Command(s.cmd[0], s.cmd[1:]...).CombinedOutput()
		if err != nil && len(out) == 0 {
			fmt.Printf("(error: %v)\n", err)
		} else {
			text := strings.TrimSpace(string(out))
			if text == "" {
				fmt.Println("(no output)")
			} else {
				fmt.Println(text)
			}
		}
	}

	fmt.Printf("\n%s\n  End of pispotctl logs\n%s\n", sep, sep)
	return nil
}

// --- helpers -----------------------------------------------------------------

func run(name string, args ...string) error {
	// #nosec G204 — name and args are hard-coded by callers.
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runSection(title string, name string, args ...string) {
	fmt.Printf("  [%s]\n", title)
	// #nosec G204 — all invocations are hard-coded.
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil && len(out) == 0 {
		fmt.Printf("  (error: %v)\n\n", err)
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fmt.Printf("  %s\n", line)
	}
	fmt.Println()
}



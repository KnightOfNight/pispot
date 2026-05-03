// Package wan provides host-level WAN control operations for pispot-authd.
package wan

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"time"
)

const execTimeout = 10 * time.Second

const wlan1SysfsPath = "/sys/class/net/wlan1"
const wlan1WirelessPath = "/sys/class/net/wlan1/wireless"

// Up starts the WAN connection: verifies wlan1 exists and is wireless,
// starts wpa_supplicant@wlan1, then requests a DHCP lease on wlan1 only.
func Up(ctx context.Context) error {
	log.Printf("wan: up requested")
	if _, err := os.Stat(wlan1SysfsPath); os.IsNotExist(err) {
		log.Printf("wan: up failed: wlan1 not found in sysfs")
		return fmt.Errorf("wlan1 does not exist")
	}
	if _, err := os.Stat(wlan1WirelessPath); os.IsNotExist(err) {
		log.Printf("wan: up failed: wlan1 exists but is not wireless")
		return fmt.Errorf("wlan1 exists but is not a wireless interface")
	}

	runCtx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	log.Printf("wan: running systemctl start wpa_supplicant@wlan1")
	if err := systemctl(runCtx, "start", "wpa_supplicant@wlan1"); err != nil {
		log.Printf("wan: systemctl start wpa_supplicant@wlan1 failed: %v", err)
		return fmt.Errorf("start wpa_supplicant@wlan1: %w", err)
	}
	log.Printf("wan: systemctl start wpa_supplicant@wlan1 ok")

	log.Printf("wan: running dhcpcd wlan1")
	if err := dhcpcd(runCtx, "wlan1"); err != nil {
		log.Printf("wan: dhcpcd wlan1 failed: %v", err)
		return fmt.Errorf("dhcpcd wlan1: %w", err)
	}
	log.Printf("wan: dhcpcd wlan1 ok")
	log.Printf("wan: up completed successfully")
	return nil
}

// Down stops the WAN connection: stops wpa_supplicant@wlan1 then
// releases the wlan1 DHCP lease.
func Down(ctx context.Context) error {
	log.Printf("wan: down requested")
	runCtx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()

	log.Printf("wan: running systemctl stop wpa_supplicant@wlan1")
	if err := systemctl(runCtx, "stop", "wpa_supplicant@wlan1"); err != nil {
		log.Printf("wan: systemctl stop wpa_supplicant@wlan1 failed: %v", err)
		return fmt.Errorf("stop wpa_supplicant@wlan1: %w", err)
	}
	log.Printf("wan: systemctl stop wpa_supplicant@wlan1 ok")

	log.Printf("wan: running dhcpcd --release wlan1")
	if err := dhcpcd(runCtx, "--release", "wlan1"); err != nil {
		log.Printf("wan: dhcpcd --release wlan1 failed: %v", err)
		return fmt.Errorf("dhcpcd --release wlan1: %w", err)
	}
	log.Printf("wan: dhcpcd --release wlan1 ok")
	log.Printf("wan: down completed successfully")
	return nil
}

func systemctl(ctx context.Context, verb, unit string) error {
	// #nosec G204 — verb and unit are hard-coded by callers, not user input.
	out, err := exec.CommandContext(ctx, "systemctl", verb, unit).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w (output: %s)", err, out)
	}
	return nil
}

func dhcpcd(ctx context.Context, args ...string) error {
	// #nosec G204 — args are hard-coded by callers, not user input.
	out, err := exec.CommandContext(ctx, "dhcpcd", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w (output: %s)", err, out)
	}
	return nil
}

// Package pwrgate detects whether an Epic PWRgate is on USB.
package pwrgate

import (
	"os"
	"path/filepath"
	"strings"
)

const productName = "Epic-PWRgate"

// Present reports whether the device is on USB, using sysfs descriptors
// under sysPath. It never opens the serial port: an open port can echo the
// device's output back to it, and that can trigger its settings menu.
func Present(sysPath string) bool {
	matches, err := filepath.Glob(filepath.Join(sysPath, "bus", "usb", "devices", "*", "product"))
	if err != nil {
		return false
	}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), productName) {
			return true
		}
	}
	return false
}

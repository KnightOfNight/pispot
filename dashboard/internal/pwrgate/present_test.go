package pwrgate

import (
	"os"
	"path/filepath"
	"testing"
)

func addDevice(t *testing.T, root, name, product string) {
	t.Helper()
	dir := filepath.Join(root, "bus", "usb", "devices", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "product"), []byte(product+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPresent(t *testing.T) {
	root := t.TempDir()
	if Present(root) {
		t.Error("Present = true with no devices")
	}
	addDevice(t, root, "3-1.2", "CT77 Radio Cable")
	if Present(root) {
		t.Error("Present = true with only another device")
	}
	addDevice(t, root, "3-1.1", "Epic-PWRgate")
	if !Present(root) {
		t.Error("Present = false with the device connected")
	}
}

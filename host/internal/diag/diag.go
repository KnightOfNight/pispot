// Package diag collects read-only system health information for the
// dashboard's Logs tab.
//
// Everything collected comes from the fixed lists in this file. Callers
// can't choose commands or arguments, and nothing here changes system
// state. Files holding secrets (hostapd.conf, the wpa_supplicant config,
// TLS private keys) are never read, and all output also passes through
// Redact as a second layer of protection.
package diag

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mcs-net/pispot/host/internal/groups"
	"github.com/mcs-net/pispot/host/internal/users"
)

// Check statuses.
const (
	StatusOK   = "ok"
	StatusWarn = "warn"
	StatusBad  = "bad"
	StatusInfo = "info"
)

const (
	logLines       = "200"
	maxOutputBytes = 128 * 1024
	cmdTimeout     = 8 * time.Second
	slowCmdTimeout = 20 * time.Second
	maxConcurrent  = 6

	fullchainPath = "/etc/pispot/certs/fullchain.pem"
	sudoersPath   = "/etc/sudoers.d/pispot"
	// fallbackAddr is eth0's static fallback from ansible/pi-setup.yml.
	fallbackAddr = "192.168.50.1"
)

// Check is one line of the health summary.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// Section is the output of one fixed command.
type Section struct {
	ID        string `json:"id"`
	Group     string `json:"group"`
	Title     string `json:"title"`
	Command   string `json:"command"`
	Output    string `json:"output"`
	Error     string `json:"error,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// Report is the full response for the Logs tab.
type Report struct {
	GeneratedAt string    `json:"generated_at"`
	Hostname    string    `json:"hostname"`
	Checks      []Check   `json:"checks"`
	Sections    []Section `json:"sections"`
}

type source struct {
	id, group, title string
	cmd              []string
	slow             bool
}

func journal(id, title string, args ...string) source {
	cmd := append([]string{"journalctl", "-b", "--no-pager", "-o", "short-iso", "-n", logLines}, args...)
	return source{id: id, group: "Logs", title: title, cmd: cmd}
}

// sources is the complete list of what the Logs tab can show.
var sources = []source{
	{id: "kernel-version", group: "System", title: "Kernel", cmd: []string{"uname", "-a"}},
	{id: "os-release", group: "System", title: "OS release", cmd: []string{"cat", "/etc/os-release"}},
	{id: "uptime", group: "System", title: "Uptime and load", cmd: []string{"uptime"}},
	{id: "clock", group: "System", title: "Clock", cmd: []string{"timedatectl"}},
	{id: "disk", group: "System", title: "Disk space", cmd: []string{"df", "-h", "/", "/boot/firmware"}},
	{id: "memory", group: "System", title: "Memory", cmd: []string{"free", "-h"}},
	{id: "temperature", group: "System", title: "Temperature", cmd: []string{"vcgencmd", "measure_temp"}},
	{id: "throttled", group: "System", title: "Throttling", cmd: []string{"vcgencmd", "get_throttled"}},
	// comm only: full command lines can contain secrets.
	{id: "processes", group: "System", title: "Top processes by memory", cmd: []string{"ps", "-eo", "pid,user,comm,%cpu,%mem", "--sort=-%mem"}},
	{id: "upgradable", group: "System", title: "Upgradable packages (as of the last apt update)", cmd: []string{"apt", "list", "--upgradable"}, slow: true},

	{id: "services", group: "Services", title: "Service states", cmd: append([]string{"systemctl", "is-active"}, append(append([]string{}, requiredServices...), wanService)...)},
	{id: "failed-units", group: "Services", title: "Failed units", cmd: []string{"systemctl", "--failed", "--no-pager"}},
	{id: "containers", group: "Services", title: "Docker containers", cmd: []string{"docker", "ps", "-a"}},
	{id: "images", group: "Services", title: "Dashboard images", cmd: []string{"docker", "images", "pispot-ui"}},

	{id: "addresses", group: "Network", title: "Addresses", cmd: []string{"ip", "-br", "addr"}},
	{id: "links", group: "Network", title: "Interfaces and MAC addresses", cmd: []string{"ip", "-br", "link"}},
	{id: "routes", group: "Network", title: "Routes", cmd: []string{"ip", "route"}},
	{id: "neighbours", group: "Network", title: "Neighbours (ARP)", cmd: []string{"ip", "neigh"}},
	{id: "sysctl", group: "Network", title: "Forwarding and ARP settings", cmd: []string{"sysctl", "net.ipv4.ip_forward", "net.ipv4.conf.all.arp_ignore", "net.ipv4.conf.all.arp_announce", "net.ipv4.conf.all.rp_filter", "net.ipv4.conf.eth0.rp_filter", "net.ipv4.conf.wlan1.rp_filter"}},
	{id: "eth0-lease", group: "Network", title: "eth0 DHCP lease", cmd: []string{"dhcpcd", "--dumplease", "eth0"}},
	{id: "wireless", group: "Network", title: "Wireless interfaces", cmd: []string{"iw", "dev"}},
	{id: "wan-link", group: "Network", title: "WAN link (wlan1)", cmd: []string{"iw", "dev", "wlan1", "link"}},
	{id: "stations", group: "Network", title: "Hotspot stations (wlan0)", cmd: []string{"iw", "dev", "wlan0", "station", "dump"}},
	{id: "leases", group: "Network", title: "Hotspot DHCP leases", cmd: []string{"cat", "/var/lib/misc/dnsmasq.leases"}},
	{id: "resolvers", group: "Network", title: "DNS resolvers", cmd: []string{"cat", "/etc/resolv.conf"}},
	{id: "listeners", group: "Network", title: "Listening ports", cmd: []string{"ss", "-tuln"}},
	{id: "firewall", group: "Network", title: "Firewall rules", cmd: []string{"nft", "list", "ruleset"}},

	{id: "cert", group: "Certificate", title: "Installed certificate", cmd: []string{"openssl", "x509", "-in", fullchainPath, "-noout", "-subject", "-issuer", "-dates", "-ext", "subjectAltName"}},
	{id: "cert-files", group: "Certificate", title: "Certificate files (names and dates only)", cmd: []string{"ls", "-l", "--time-style=long-iso", "/etc/pispot/certs/"}},

	{id: "usb", group: "USB", title: "USB devices", cmd: []string{"lsusb"}},
	{id: "usb-tree", group: "USB", title: "USB tree and drivers", cmd: []string{"lsusb", "-t"}},
	{id: "serial", group: "USB", title: "Serial devices", cmd: []string{"ls", "-l", "/dev/serial/by-id/"}},

	journal("log-errors", "Errors this boot, all services", "-p", "err"),
	journal("log-dhcpcd", "dhcpcd", "-u", "dhcpcd"),
	journal("log-authd", "pispot-authd", "-u", "pispot-authd"),
	journal("log-wpa", "wpa_supplicant@wlan1", "-u", wanService),
	journal("log-hostapd", "hostapd", "-u", "hostapd"),
	journal("log-dnsmasq", "dnsmasq", "-u", "dnsmasq"),
	journal("log-nftables", "nftables", "-u", "nftables"),
	journal("log-docker", "docker", "-u", "docker"),
	journal("log-kernel", "Kernel", "-k"),
	{id: "log-dashboard", group: "Logs", title: "Dashboard container", cmd: []string{"docker", "logs", "--tail", logLines, "pispot-ui"}},
}

const wanService = "wpa_supplicant@wlan1"

var requiredServices = []string{"dhcpcd", "hostapd", "dnsmasq", "nftables", "pispot-authd", "docker"}

// secretPattern matches key=value or key: value pairs with sensitive names.
var secretPattern = regexp.MustCompile(`(?i)(\b(?:wpa_)?(?:psk|passphrase|password|passwd|pass|secret|token)"?\s*[=:]\s*)("[^"]*"|\S+)`)

// authPattern matches HTTP Authorization values.
var authPattern = regexp.MustCompile(`(?i)(\bauthorization"?\s*[=:]\s*)("[^"]*"|\S+(?:\s+\S+)?)`)

// privateKeyPattern matches a PEM private key, complete or cut off.
var privateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?(?:-----END [A-Z0-9 ]*PRIVATE KEY-----|\z)`)

// Redact removes anything that looks like a secret from text.
func Redact(s string) string {
	s = privateKeyPattern.ReplaceAllString(s, "[private key redacted]")
	s = authPattern.ReplaceAllString(s, "${1}[redacted]")
	return secretPattern.ReplaceAllString(s, "${1}[redacted]")
}

func run(ctx context.Context, timeout time.Duration, cmd []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// #nosec G204 — commands come only from the fixed lists in this file.
	c := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	c.Env = append(os.Environ(), "LC_ALL=C", "SYSTEMD_COLORS=0", "NO_COLOR=1")
	out, err := c.CombinedOutput()
	return string(bytes.TrimRight(out, "\n")), err
}

// capOutput keeps the end of text, where the newest log lines are.
func capOutput(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}
	s = s[len(s)-limit:]
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return s, true
}

func collectSection(ctx context.Context, src source) Section {
	timeout := cmdTimeout
	if src.slow {
		timeout = slowCmdTimeout
	}
	out, err := run(ctx, timeout, src.cmd)
	capped, truncated := capOutput(Redact(out), maxOutputBytes)
	sec := Section{
		ID:        src.id,
		Group:     src.group,
		Title:     src.title,
		Command:   strings.Join(src.cmd, " "),
		Output:    capped,
		Truncated: truncated,
	}
	// Some commands exit non-zero to report state (systemctl is-active),
	// so only a command with no output counts as an error.
	if err != nil && out == "" {
		sec.Error = Redact(err.Error())
	}
	return sec
}

// Collect gathers the full report. It's safe to call concurrently.
func Collect(ctx context.Context, cfg groups.Config) Report {
	host, _ := os.Hostname()
	sections := make([]Section, len(sources))
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for i, src := range sources {
		wg.Add(1)
		go func(i int, src source) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			sections[i] = collectSection(ctx, src)
		}(i, src)
	}
	wg.Wait()
	return Report{
		GeneratedAt: time.Now().Format(time.RFC3339),
		Hostname:    host,
		Checks:      collectChecks(ctx, cfg),
		Sections:    sections,
	}
}

func collectChecks(ctx context.Context, cfg groups.Config) []Check {
	return []Check{
		checkCert(ctx, time.Now()),
		checkEth0(),
		checkConflicts(ctx),
		checkServices(ctx),
		checkAccounts(ctx, cfg),
		checkWAN(ctx),
		checkContainer(ctx),
		checkClock(ctx),
		checkForwarding(),
		checkDisk(),
		checkThrottled(ctx),
		checkReboot(),
		checkUpgradable(ctx),
		checkPWRgate(),
	}
}

// --- certificate -------------------------------------------------------------

func firstCert(pemData []byte) (*x509.Certificate, error) {
	for {
		block, rest := pem.Decode(pemData)
		if block == nil {
			return nil, fmt.Errorf("no certificate found")
		}
		if block.Type == "CERTIFICATE" {
			return x509.ParseCertificate(block.Bytes)
		}
		pemData = rest
	}
}

func classifyExpiry(notAfter, now time.Time) (string, string) {
	days := int(notAfter.Sub(now).Hours() / 24)
	date := notAfter.Format("2006-01-02")
	switch {
	case !now.Before(notAfter):
		return StatusBad, "expired on " + date
	case days <= 30:
		return StatusWarn, fmt.Sprintf("%d days left, expires %s", days, date)
	default:
		return StatusOK, fmt.Sprintf("%d days left, expires %s", days, date)
	}
}

func servedCert(ctx context.Context) (*x509.Certificate, error) {
	d := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 3 * time.Second},
		// #nosec G402 — reads the local server's cert to compare it with
		// the installed one; no data is exchanged.
		Config: &tls.Config{InsecureSkipVerify: true},
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	conn, err := d.DialContext(ctx, "tcp", "127.0.0.1:443")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return conn.(*tls.Conn).ConnectionState().PeerCertificates[0], nil
}

func checkCert(ctx context.Context, now time.Time) Check {
	c := Check{Name: "TLS certificate"}
	data, err := os.ReadFile(fullchainPath)
	if err != nil {
		c.Status, c.Detail = StatusBad, "can't read the installed certificate"
		return c
	}
	cert, err := firstCert(data)
	if err != nil {
		c.Status, c.Detail = StatusBad, "installed certificate isn't valid"
		return c
	}
	c.Status, c.Detail = classifyExpiry(cert.NotAfter, now)
	served, err := servedCert(ctx)
	if err != nil {
		if c.Status == StatusOK {
			c.Status = StatusWarn
		}
		c.Detail += "; couldn't read the served certificate"
	} else if !served.Equal(cert) {
		if c.Status == StatusOK {
			c.Status = StatusWarn
		}
		c.Detail += "; dashboard is serving a different certificate, so it may need a restart"
	}
	return c
}

// --- network -----------------------------------------------------------------

func classifyEth0(carrier bool, addrs []string) (string, string) {
	if !carrier {
		return StatusInfo, "no link"
	}
	if len(addrs) == 0 {
		return StatusWarn, "link is up, but there's no IPv4 address"
	}
	for _, a := range addrs {
		if a == fallbackAddr {
			return StatusWarn, "on the fallback address " + fallbackAddr + ", so DHCP didn't answer; it stays there until the link changes"
		}
	}
	return StatusOK, strings.Join(addrs, ", ")
}

func ipv4Addrs(name string) []string {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			out = append(out, n.IP.String())
		}
	}
	return out
}

func checkEth0() Check {
	data, err := os.ReadFile("/sys/class/net/eth0/carrier")
	carrier := err == nil && strings.TrimSpace(string(data)) == "1"
	status, detail := classifyEth0(carrier, ipv4Addrs("eth0"))
	return Check{Name: "eth0", Status: status, Detail: detail}
}

func countConflicts(log string) int {
	n := 0
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, " claims ") || strings.Contains(line, "defence failed") {
			n++
		}
	}
	return n
}

func checkConflicts(ctx context.Context) Check {
	c := Check{Name: "DHCP address conflicts this boot"}
	out, err := run(ctx, cmdTimeout, []string{"journalctl", "-b", "--no-pager", "-u", "dhcpcd"})
	if err != nil {
		c.Status, c.Detail = StatusWarn, "couldn't read the dhcpcd log"
		return c
	}
	if n := countConflicts(out); n > 0 {
		c.Status, c.Detail = StatusWarn, fmt.Sprintf("%d conflict lines in the dhcpcd log", n)
		return c
	}
	c.Status, c.Detail = StatusOK, "none"
	return c
}

func checkForwarding() Check {
	c := Check{Name: "IP forwarding"}
	data, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward")
	if err != nil || strings.TrimSpace(string(data)) != "1" {
		c.Status, c.Detail = StatusBad, "off, so hotspot clients have no internet"
		return c
	}
	c.Status, c.Detail = StatusOK, "on"
	return c
}

// --- accounts ----------------------------------------------------------------

// canSudo asks sudo itself, so the answer holds however access was granted.
// It only sees whether the account can run an ordinary command, so a rule for
// one specific command elsewhere won't count.
func canSudo(ctx context.Context, username string) (bool, error) {
	out, err := run(ctx, cmdTimeout, []string{"sudo", "-n", "-l", "-U", username, "/usr/bin/true"})
	if err == nil {
		return true, nil
	}
	// Exit 1 with no message means not allowed. A message means something
	// else went wrong, such as an unknown user.
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && out == "" {
		return false, nil
	}
	return false, err
}

func classifyAccounts(cfg groups.Config, members map[string]map[string]bool, sudoersExists bool, sudo func(string) (bool, error)) (string, string) {
	var problems []string
	if _, ok := members[cfg.OwnerGroup]; !ok {
		problems = append(problems, "the "+cfg.OwnerGroup+" group doesn't exist")
	}
	if !sudoersExists {
		problems = append(problems, sudoersPath+" is missing")
	}

	seen := map[string]bool{}
	for _, group := range []string{cfg.ReadonlyGroup, cfg.AdminGroup, cfg.OwnerGroup} {
		for name := range members[group] {
			seen[name] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)

	var owners []string
	for _, name := range names {
		inGroup := func(group string) bool { return members[group][name] }
		if missing := cfg.Missing(inGroup); len(missing) > 0 {
			problems = append(problems, fmt.Sprintf("%s isn't in %s, which its other groups require", name, strings.Join(missing, " or ")))
		}
		allowed, err := sudo(name)
		isOwner := inGroup(cfg.OwnerGroup)
		switch {
		case err != nil:
			problems = append(problems, "couldn't check sudo for "+name)
		case isOwner && !allowed:
			problems = append(problems, name+" is in "+cfg.OwnerGroup+" but can't use sudo")
		case !isOwner && allowed:
			problems = append(problems, name+" can use sudo but isn't in "+cfg.OwnerGroup)
		}
		if isOwner {
			owners = append(owners, name)
		}
	}
	if len(owners) == 0 {
		problems = append(problems, "nobody is in "+cfg.OwnerGroup)
	}
	if len(problems) > 0 {
		return StatusWarn, strings.Join(problems, "; ")
	}
	return StatusOK, "owners with sudo: " + strings.Join(owners, ", ") + "; nobody else in the pispot groups has it"
}

func checkAccounts(ctx context.Context, cfg groups.Config) Check {
	c := Check{Name: "Account groups and sudo"}
	members, err := users.GroupMembers("/etc/passwd", "/etc/group")
	if err != nil {
		c.Status, c.Detail = StatusWarn, "couldn't read the account files"
		return c
	}
	_, statErr := os.Stat(sudoersPath)
	c.Status, c.Detail = classifyAccounts(cfg, members, statErr == nil, func(name string) (bool, error) {
		return canSudo(ctx, name)
	})
	return c
}

// --- services ----------------------------------------------------------------

func checkServices(ctx context.Context) Check {
	c := Check{Name: "Services"}
	var down []string
	for _, svc := range requiredServices {
		out, _ := run(ctx, cmdTimeout, []string{"systemctl", "is-active", svc})
		if state := strings.TrimSpace(out); state != "active" {
			if state == "" {
				state = "unknown"
			}
			down = append(down, svc+" is "+state)
		}
	}
	if len(down) > 0 {
		c.Status, c.Detail = StatusBad, strings.Join(down, ", ")
		return c
	}
	c.Status, c.Detail = StatusOK, "all running: "+strings.Join(requiredServices, ", ")
	return c
}

func checkWAN(ctx context.Context) Check {
	c := Check{Name: "WAN (wlan1)", Status: StatusInfo}
	if _, err := os.Stat("/sys/class/net/wlan1"); err != nil {
		c.Detail = "USB adapter not present"
		return c
	}
	out, _ := run(ctx, cmdTimeout, []string{"systemctl", "is-active", wanService})
	if strings.TrimSpace(out) != "active" {
		c.Detail = "adapter present, WAN is off"
		return c
	}
	c.Detail = "on"
	if addrs := ipv4Addrs("wlan1"); len(addrs) > 0 {
		c.Detail = "on, " + strings.Join(addrs, ", ")
	}
	return c
}

func checkContainer(ctx context.Context) Check {
	c := Check{Name: "Dashboard container"}
	// A fixed format, never the whole inspect output, which includes env vars.
	out, err := run(ctx, cmdTimeout, []string{"docker", "inspect", "--format", "{{.State.Status}} {{.RestartCount}}", "pispot-ui"})
	fields := strings.Fields(out)
	if err != nil || len(fields) != 2 {
		c.Status, c.Detail = StatusBad, "couldn't read its state"
		return c
	}
	c.Detail = fmt.Sprintf("%s, %s restarts", fields[0], fields[1])
	switch {
	case fields[0] != "running":
		c.Status = StatusBad
	case fields[1] != "0":
		c.Status = StatusWarn
	default:
		c.Status = StatusOK
	}
	return c
}

// --- system ------------------------------------------------------------------

func checkClock(ctx context.Context) Check {
	c := Check{Name: "Clock"}
	out, err := run(ctx, cmdTimeout, []string{"timedatectl", "show", "-p", "NTPSynchronized", "--value"})
	if err == nil && strings.TrimSpace(out) == "yes" {
		c.Status, c.Detail = StatusOK, "synced"
		return c
	}
	c.Status, c.Detail = StatusWarn, "not synced, so log times may be wrong"
	return c
}

func usedPercent(blocks, free, avail uint64) int {
	used := blocks - free
	total := used + avail
	if total == 0 {
		return 0
	}
	return int(used * 100 / total)
}

func classifyDisk(pct int) string {
	switch {
	case pct >= 95:
		return StatusBad
	case pct >= 85:
		return StatusWarn
	default:
		return StatusOK
	}
}

func checkDisk() Check {
	c := Check{Name: "Disk space", Status: StatusOK}
	var parts []string
	for _, path := range []string{"/", "/boot/firmware"} {
		var st syscall.Statfs_t
		if err := syscall.Statfs(path, &st); err != nil {
			parts = append(parts, path+" unknown")
			if c.Status == StatusOK {
				c.Status = StatusWarn
			}
			continue
		}
		pct := usedPercent(uint64(st.Blocks), uint64(st.Bfree), uint64(st.Bavail))
		parts = append(parts, fmt.Sprintf("%s %d%% used", path, pct))
		switch classifyDisk(pct) {
		case StatusBad:
			c.Status = StatusBad
		case StatusWarn:
			if c.Status == StatusOK {
				c.Status = StatusWarn
			}
		}
	}
	c.Detail = strings.Join(parts, ", ")
	return c
}

func classifyThrottled(out string) (string, string) {
	_, val, found := strings.Cut(strings.TrimSpace(out), "=")
	if !found {
		return StatusInfo, "not available"
	}
	n, err := strconv.ParseUint(strings.TrimPrefix(val, "0x"), 16, 32)
	if err != nil {
		return StatusInfo, "not available"
	}
	switch {
	case n == 0:
		return StatusOK, "none since boot"
	case n&0xF != 0:
		return StatusBad, fmt.Sprintf("active now (%s): under-voltage, frequency cap or overheating", val)
	default:
		return StatusWarn, fmt.Sprintf("happened since boot (%s)", val)
	}
}

func checkThrottled(ctx context.Context) Check {
	out, _ := run(ctx, cmdTimeout, []string{"vcgencmd", "get_throttled"})
	status, detail := classifyThrottled(out)
	return Check{Name: "Pi throttling", Status: status, Detail: detail}
}

func checkReboot() Check {
	c := Check{Name: "Reboot needed"}
	if _, err := os.Stat("/var/run/reboot-required"); err == nil {
		c.Status, c.Detail = StatusWarn, "yes"
		return c
	}
	c.Status, c.Detail = StatusOK, "not flagged"
	return c
}

func countUpgradable(out string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "[upgradable from:") {
			n++
		}
	}
	return n
}

func checkUpgradable(ctx context.Context) Check {
	c := Check{Name: "Package updates", Status: StatusInfo}
	out, err := run(ctx, slowCmdTimeout, []string{"apt", "list", "--upgradable"})
	if err != nil {
		c.Detail = "couldn't read the package list"
		return c
	}
	c.Detail = fmt.Sprintf("%d upgradable as of the last apt update", countUpgradable(out))
	return c
}

// checkPWRgate looks only at USB descriptors. It never opens the serial port.
func checkPWRgate() Check {
	c := Check{Name: "Epic PWRgate", Status: StatusInfo, Detail: "not connected"}
	matches, _ := filepath.Glob("/sys/bus/usb/devices/*/product")
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), "Epic-PWRgate") {
			c.Detail = "connected"
			break
		}
	}
	return c
}

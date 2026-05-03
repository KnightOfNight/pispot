package hotspot

import (
	"bufio"
	"strings"
)

// leaseEntry holds the IP and hostname associated with a given MAC in
// the dnsmasq lease file.
type leaseEntry struct {
	IP       string
	Hostname string
}

// parseLeases reads the contents of /var/lib/misc/dnsmasq.leases and
// returns a map from lowercased MAC address to leaseEntry.
func parseLeases(raw []byte) map[string]leaseEntry {
	out := make(map[string]leaseEntry)
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		mac := strings.ToLower(fields[1])
		ip := fields[2]
		hostname := fields[3]
		if hostname == "*" {
			hostname = ""
		}
		out[mac] = leaseEntry{IP: ip, Hostname: hostname}
	}
	return out
}

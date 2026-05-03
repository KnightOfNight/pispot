// Package hotspot collects information about stations associated with
// the LAN-side access point (wlan0), enriched with IP/hostname data
// from the dnsmasq lease file.
//
// The collector shells out to `iw dev <iface> station dump` to enumerate
// associated stations and parses the result. Results are cached for a
// short TTL so callers (the HTTP API) can poll aggressively without
// thrashing iw.
package hotspot

import (
	"bufio"
	"strconv"
	"strings"
)

// Client is one associated station, optionally enriched with a DHCP-
// derived IP and hostname from the dnsmasq lease file.
type Client struct {
	MAC              string
	IP               string
	Hostname         string
	SignalDBm        int
	ConnectedSeconds uint64
	RxBytes          uint64
	TxBytes          uint64
}

// parseStationDump parses the output of `iw dev <iface> station dump` and
// returns the list of associated stations. MAC addresses are normalized
// to lowercase. Unknown key/value lines are ignored, so future iw
// versions adding new fields will not break parsing.
func parseStationDump(raw []byte) []Client {
	var (
		clients []Client
		cur     *Client
	)

	flush := func() {
		if cur != nil {
			clients = append(clients, *cur)
			cur = nil
		}
	}

	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	// iw rows can be long; grow the buffer just in case.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "Station ") {
			flush()
			// "Station <mac> (on <iface>)"
			fields := strings.Fields(trimmed)
			if len(fields) < 2 {
				continue
			}
			cur = &Client{MAC: strings.ToLower(fields[1])}
			continue
		}
		if cur == nil {
			continue
		}

		key, value, ok := splitKeyValue(trimmed)
		if !ok {
			continue
		}

		switch key {
		case "rx bytes":
			cur.RxBytes = parseUint(value)
		case "tx bytes":
			cur.TxBytes = parseUint(value)
		case "signal":
			cur.SignalDBm = parseFirstInt(value)
		case "connected time":
			cur.ConnectedSeconds = parseUint(value)
		}
	}
	flush()
	return clients
}

func splitKeyValue(line string) (key, value string, ok bool) {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
}

func parseUint(s string) uint64 {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}
	n, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func parseFirstInt(s string) int {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}
	n, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0
	}
	return n
}

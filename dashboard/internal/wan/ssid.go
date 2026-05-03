// Package wan collects upstream-link information for the WAN-facing
// wireless interface: SSID/BSSID/signal from `iw dev <iface> link`, IP
// from `ip -j addr show <iface>`, and default gateway from
// `ip -j route show default`.
package wan

import (
	"bufio"
	"encoding/json"
	"strconv"
	"strings"
)

// Info is the public, flattened view of the WAN interface's current
// upstream association.
type Info struct {
	Interface        string
	InterfacePresent bool
	SupplicantActive bool
	Connected        bool
	SSID             string
	BSSID            string
	SignalDBm        int
	FreqMHz          int
	TxBitrateMbps    float64
	IP               string
	Gateway          string
}

// linkResult is the intermediate parsed view of `iw dev <iface> link`.
type linkResult struct {
	connected     bool
	ssid          string
	bssid         string
	signalDBm     int
	freqMHz       int
	txBitrateMbps float64
}

func parseIwLink(raw []byte) linkResult {
	var out linkResult
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Not connected") {
			return linkResult{}
		}
		if strings.HasPrefix(line, "Connected to ") {
			out.connected = true
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				out.bssid = strings.ToLower(fields[2])
			}
			continue
		}
		key, value, ok := splitKeyValue(line)
		if !ok {
			continue
		}
		switch key {
		case "SSID":
			out.ssid = value
		case "freq":
			out.freqMHz = int(parseFirstFloat(value))
		case "signal":
			out.signalDBm = parseFirstInt(value)
		case "tx bitrate":
			out.txBitrateMbps = parseFirstFloat(value)
		}
	}
	return out
}

func parseIPAddr(raw []byte) string {
	var doc []struct {
		AddrInfo []struct {
			Family string `json:"family"`
			Local  string `json:"local"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	for _, iface := range doc {
		for _, a := range iface.AddrInfo {
			if a.Family == "inet" && a.Local != "" {
				return a.Local
			}
		}
	}
	return ""
}

func parseIPRoute(raw []byte, iface string) string {
	var doc []struct {
		Dst     string `json:"dst"`
		Gateway string `json:"gateway"`
		Dev     string `json:"dev"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ""
	}
	for _, r := range doc {
		if r.Dev == iface && r.Gateway != "" {
			return r.Gateway
		}
	}
	return ""
}

func splitKeyValue(line string) (key, value string, ok bool) {
	idx := strings.IndexByte(line, ':')
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:]), true
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

func parseFirstFloat(s string) float64 {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}
	n, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return n
}

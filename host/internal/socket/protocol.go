// Package socket defines the JSON wire protocol between pispot-ui and
// pispot-authd.
package socket

// Op constants for the socket protocol.
const (
	OpAuth       = "auth"
	OpWanUp      = "wan_up"
	OpWanDown    = "wan_down"
	OpWifiList   = "wifi_list"
	OpWifiAdd    = "wifi_add"
	OpWifiRemove = "wifi_remove"
	OpWifiReload = "wifi_reload"
)

// Request is sent from pispot-ui to pispot-authd over the Unix socket.
type Request struct {
	Op       string `json:"op"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// WiFi fields — used for wifi_add and wifi_remove ops.
	SSID string `json:"ssid,omitempty"`
	PSK  string `json:"psk,omitempty"`
}

// Response is returned by pispot-authd. When Ok is true, Role is one of
// "readonly" or "admin". When Ok is false, Error contains a human-readable
// reason (never the raw PAM error message, to avoid leaking auth detail).
type Response struct {
	Ok       bool   `json:"ok"`
	Username string `json:"username,omitempty"`
	Role     string `json:"role,omitempty"`
	Error    string `json:"error,omitempty"`
	// Networks is populated by wifi_list responses.
	Networks []Network `json:"networks,omitempty"`
}

// Network is one wpa_supplicant network entry returned in wifi_list responses.
type Network struct {
	SSID string `json:"ssid"`
	PSK  string `json:"psk"`
}

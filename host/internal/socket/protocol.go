// Package socket defines the JSON wire protocol between pispot-ui and
// pispot-authd.
package socket

import (
	"github.com/mcs-net/pispot/host/internal/diag"
	"github.com/mcs-net/pispot/host/internal/users"
)

// Op constants for the socket protocol.
const (
	OpAuth       = "auth"
	OpWanUp      = "wan_up"
	OpWanDown    = "wan_down"
	OpWifiList   = "wifi_list"
	OpWifiAdd    = "wifi_add"
	OpWifiRemove = "wifi_remove"
	OpWifiReload = "wifi_reload"
	OpDiag       = "diag"
	OpUserList   = "user_list"
	OpUserCreate = "user_create"
	OpUserUpdate = "user_update"
	OpUserDelete = "user_delete"
)

// Request is sent from pispot-ui to pispot-authd over the Unix socket.
type Request struct {
	Op       string `json:"op"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// WiFi fields — used for wifi_add and wifi_remove ops.
	SSID string `json:"ssid,omitempty"`
	PSK  string `json:"psk,omitempty"`
	// Actor is the logged-in user making a user_* change. Username and
	// Password are the account being changed.
	Actor string `json:"actor,omitempty"`
	// Membership is the requested groups. Nil leaves them unchanged.
	Membership *users.Membership `json:"membership,omitempty"`
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
	// Diag is populated by diag responses.
	Diag *diag.Report `json:"diag,omitempty"`
	// Users is populated by user_list responses.
	Users []users.Account `json:"users,omitempty"`
}

// Network is one wpa_supplicant network entry returned in wifi_list responses.
type Network struct {
	SSID string `json:"ssid"`
	PSK  string `json:"psk"`
}

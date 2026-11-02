// Package authz provides Basic Auth middleware for pispot-ui that
// delegates authentication and role resolution to the pispot-authd
// host helper over a Unix socket.
//
// The wire types below are intentionally duplicated from the host package
// (github.com/mcs-net/pispot/host/internal/socket) for clarity — both
// sides of the socket are in the same module now, but keeping them in the
// authz package avoids a cross-package import cycle between dashboard and
// host sub-trees.
package authz

import "encoding/json"

// authRequest is sent to pispot-authd over the Unix socket.
type authRequest struct {
	Op       string `json:"op"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// WiFi fields — used for wifi_add and wifi_remove ops.
	SSID string `json:"ssid,omitempty"`
	PSK  string `json:"psk,omitempty"`
	// Actor is the logged-in user making a user_* change. Username and
	// Password are the account being changed.
	Actor string `json:"actor,omitempty"`
	// Admin is the requested admin access. Nil leaves it unchanged.
	Admin *bool `json:"admin,omitempty"`
}

// authResponse is returned by pispot-authd.
// Role is "readonly" or "admin" when Ok is true.
// Networks is populated by wifi_list responses.
type authResponse struct {
	Ok       bool          `json:"ok"`
	Username string        `json:"username,omitempty"`
	Role     string        `json:"role,omitempty"`
	Error    string        `json:"error,omitempty"`
	Networks []wifiNetwork `json:"networks,omitempty"`
	// Diag is passed through to the browser without being decoded here.
	Diag json.RawMessage `json:"diag,omitempty"`
	// Users is passed through to the browser without being decoded here.
	Users json.RawMessage `json:"users,omitempty"`
}

// wifiNetwork is one network entry in a wifi_list response. It has no password
// field, so a password sent by an older helper is dropped here.
type wifiNetwork struct {
	SSID string `json:"ssid"`
}

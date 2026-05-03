// Command pispot-authd is the pispot authentication and host-control
// helper daemon. It listens on a Unix socket and handles two categories
// of request:
//
//   - "auth": authenticate a username/password via PAM and resolve the
//     caller's Unix group to a pispot role (readonly or admin).
//   - "wan_up" / "wan_down": privileged WAN control operations. The caller
//     (pispot-ui) is responsible for verifying admin role before sending.
//
// Configuration is read from /etc/pispot/authd.conf (INI format).
//
// Usage:
//
//	pispot-authd [--config /etc/pispot/authd.conf] [--socket /run/pispot/pispot-authd.sock]
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mcs-net/pispot/host/internal/auth"
	"github.com/mcs-net/pispot/host/internal/groups"
	"github.com/mcs-net/pispot/host/internal/socket"
	"github.com/mcs-net/pispot/host/internal/wan"
	"github.com/mcs-net/pispot/host/internal/wifi"
)

func main() {
	configPath := flag.String("config", "/etc/pispot/authd.conf", "path to authd configuration file")
	socketPath := flag.String("socket", "/run/pispot/pispot-authd.sock", "Unix socket path")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	log.Printf("pispot-authd starting (readonly=%s admin=%s socket=%s)",
		cfg.ReadonlyGroup, cfg.AdminGroup, *socketPath)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	handler := func(ctx context.Context, req socket.Request) socket.Response {
		return handleRequest(ctx, req, cfg)
	}

	if err := socket.Serve(ctx, *socketPath, handler); err != nil {
		log.Fatalf("serve: %v", err)
	}
	log.Printf("pispot-authd stopped")
}

func handleRequest(ctx context.Context, req socket.Request, cfg groups.Config) socket.Response {
	log.Printf("dispatch: op=%q", req.Op)
	switch req.Op {
	case socket.OpAuth:
		return handleAuth(ctx, req, cfg)
	case socket.OpWanUp:
		return handleWanOp(ctx, "wan_up", wan.Up)
	case socket.OpWanDown:
		return handleWanOp(ctx, "wan_down", wan.Down)
	case socket.OpWifiList:
		return handleWifiList()
	case socket.OpWifiAdd:
		return handleWifiAdd(req)
	case socket.OpWifiRemove:
		return handleWifiRemove(req)
	case socket.OpWifiReload:
		return handleWifiReload()
	default:
		log.Printf("dispatch: unknown op=%q", req.Op)
		return socket.Response{Error: "unknown op"}
	}
}

func handleWanOp(ctx context.Context, name string, op func(context.Context) error) socket.Response {
	log.Printf("wan op: starting %s", name)
	if err := op(ctx); err != nil {
		log.Printf("wan op: %s failed: %v", name, err)
		return socket.Response{Error: err.Error()}
	}
	log.Printf("wan op: %s succeeded", name)
	return socket.Response{Ok: true}
}

func handleWifiList() socket.Response {
	log.Printf("wifi op: listing networks")
	networks, err := wifi.List()
	if err != nil {
		log.Printf("wifi op: list failed: %v", err)
		return socket.Response{Error: err.Error()}
	}
	var sn []socket.Network
	for _, n := range networks {
		sn = append(sn, socket.Network{SSID: n.SSID, PSK: n.PSK})
	}
	log.Printf("wifi op: list returned %d network(s)", len(sn))
	return socket.Response{Ok: true, Networks: sn}
}

func handleWifiAdd(req socket.Request) socket.Response {
	log.Printf("wifi op: adding network ssid=%q", req.SSID)
	if err := wifi.Add(req.SSID, req.PSK); err != nil {
		log.Printf("wifi op: add failed ssid=%q: %v", req.SSID, err)
		return socket.Response{Error: err.Error()}
	}
	log.Printf("wifi op: add ok ssid=%q", req.SSID)
	return socket.Response{Ok: true}
}

func handleWifiRemove(req socket.Request) socket.Response {
	log.Printf("wifi op: removing network ssid=%q", req.SSID)
	if err := wifi.Remove(req.SSID); err != nil {
		log.Printf("wifi op: remove failed ssid=%q: %v", req.SSID, err)
		return socket.Response{Error: err.Error()}
	}
	log.Printf("wifi op: remove ok ssid=%q", req.SSID)
	return socket.Response{Ok: true}
}

func handleWifiReload() socket.Response {
	log.Printf("wifi op: reloading wpa_supplicant config")
	if err := wifi.Reload(); err != nil {
		log.Printf("wifi op: reload failed: %v", err)
		return socket.Response{Error: err.Error()}
	}
	log.Printf("wifi op: reload ok")
	return socket.Response{Ok: true}
}

func handleAuth(ctx context.Context, req socket.Request, cfg groups.Config) socket.Response {
	if req.Username == "" || req.Password == "" {
		return socket.Response{Error: "username and password required"}
	}

	if err := auth.Authenticate(ctx, "pispot", req.Username, req.Password); err != nil {
		log.Printf("auth: PAM failed for user=%q", req.Username)
		return socket.Response{Error: "authentication failed"}
	}
	log.Printf("auth: PAM passed for user=%q, resolving groups", req.Username)

	role, err := groups.Resolve(req.Username, cfg)
	if err != nil {
		if errors.Is(err, groups.ErrNotAuthorized) {
			log.Printf("auth: denied user=%q not in any pispot group (readonly=%s admin=%s)",
				req.Username, cfg.ReadonlyGroup, cfg.AdminGroup)
			return socket.Response{Error: "not authorized"}
		}
		log.Printf("auth: group lookup failed for user=%q: %v", req.Username, err)
		return socket.Response{Error: "authorization error"}
	}

	log.Printf("auth: ok user=%q role=%s", req.Username, role)
	return socket.Response{Ok: true, Username: req.Username, Role: role}
}

// loadConfig reads the INI-style authd.conf and returns a groups.Config.
func loadConfig(path string) (groups.Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return groups.Config{}, err
	}
	defer f.Close()

	cfg := groups.Config{
		ReadonlyGroup: "pispot-ro",
		AdminGroup:    "pispot-admin",
	}

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "readonly":
			cfg.ReadonlyGroup = val
		case "admin":
			cfg.AdminGroup = val
		}
	}
	return cfg, sc.Err()
}

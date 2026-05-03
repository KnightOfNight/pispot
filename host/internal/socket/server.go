// Package socket provides the Unix socket listener and request dispatcher
// for pispot-authd.
package socket

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"os"
)

// Handler is called for each authenticated request.
type Handler func(ctx context.Context, req Request) Response

// Serve listens on the Unix socket at path and dispatches incoming
// requests to handler until ctx is done. The socket file is removed
// on exit.
func Serve(ctx context.Context, path string, handler Handler) error {
	// Remove any stale socket from a previous run.
	_ = os.Remove(path)

	l, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer func() {
		l.Close()
		os.Remove(path)
	}()

	// Restrict socket access to root only.
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}

	log.Printf("pispot-authd listening on %s", path)

	go func() {
		<-ctx.Done()
		l.Close()
	}()

	for {
		conn, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			log.Printf("accept error: %v", err)
			continue
		}
		go handle(ctx, conn, handler)
	}
}

func handle(ctx context.Context, conn net.Conn, handler Handler) {
	defer conn.Close()

	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		log.Printf("socket: decode error: %v", err)
		writeResponse(conn, Response{Error: "invalid request"})
		return
	}

	log.Printf("socket: received op=%q", req.Op)
	resp := handler(ctx, req)
	if resp.Ok {
		log.Printf("socket: op=%q completed ok", req.Op)
	} else {
		log.Printf("socket: op=%q completed error=%q", req.Op, resp.Error)
	}
	writeResponse(conn, resp)
}

func writeResponse(conn net.Conn, resp Response) {
	if err := json.NewEncoder(conn).Encode(resp); err != nil {
		log.Printf("encode response: %v", err)
	}
}

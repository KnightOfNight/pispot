// Package auth authenticates a username/password pair against the host
// PAM stack by invoking `pamtester`. This keeps the binary CGO-free and
// buildable on the Mac with GOOS=linux GOARCH=arm64 CGO_ENABLED=0.
//
// pamtester must be installed on the Pi host:
//
//	sudo apt install pamtester
package auth

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"time"
)

const pamTimeout = 5 * time.Second

// Authenticate verifies username/password against the PAM service named
// by service (typically "pispot"). Returns nil on success, a non-nil
// error on any failure. The error is intentionally opaque — callers
// should not forward its text to the client.
func Authenticate(ctx context.Context, service, username, password string) error {
	ctx, cancel := context.WithTimeout(ctx, pamTimeout)
	defer cancel()

	log.Printf("pam: authenticating user=%q service=%q", username, service)

	// #nosec G204 — service and username come from our own config/request
	// parsing, not from untrusted external input without validation.
	cmd := exec.CommandContext(ctx, "pamtester", service, username, "authenticate")
	cmd.Stdin = bytes.NewBufferString(password + "\n")

	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("pam: authentication failed for user=%q: %v", username, err)
		return fmt.Errorf("pamtester: %w (output: %s)", err, bytes.TrimSpace(out))
	}
	log.Printf("pam: authentication succeeded for user=%q", username)
	return nil
}

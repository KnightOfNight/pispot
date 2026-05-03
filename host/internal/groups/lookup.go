// Package groups resolves a username's Unix group membership to a pispot
// role using the host's NSS via the Go stdlib os/user package.
package groups

import (
	"fmt"
	"os/user"
)

// Config holds the configured Unix group names for each role.
type Config struct {
	ReadonlyGroup string
	AdminGroup    string
}

// Resolve returns the pispot role ("readonly" or "admin") for the given
// username based on their Unix group membership. If the user is in both
// groups, "admin" wins. If neither, ("", ErrNotAuthorized) is returned.
func Resolve(username string, cfg Config) (string, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return "", fmt.Errorf("user lookup %q: %w", username, err)
	}

	gids, err := u.GroupIds()
	if err != nil {
		return "", fmt.Errorf("group ids for %q: %w", username, err)
	}

	var inReadonly, inAdmin bool
	for _, gid := range gids {
		g, err := user.LookupGroupId(gid)
		if err != nil {
			continue
		}
		switch g.Name {
		case cfg.AdminGroup:
			inAdmin = true
		case cfg.ReadonlyGroup:
			inReadonly = true
		}
	}

	switch {
	case inAdmin:
		return "admin", nil
	case inReadonly:
		return "readonly", nil
	default:
		return "", ErrNotAuthorized
	}
}

// ErrNotAuthorized is returned when the authenticated user is not a
// member of any configured pispot group.
var ErrNotAuthorized = fmt.Errorf("not a member of any pispot group")

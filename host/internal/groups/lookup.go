// Package groups turns a username's Unix group membership into a pispot role
// using the host's NSS via the Go stdlib os/user package.
//
// The groups form a ladder: readonly, then admin, then owner. Each one requires
// all those below it, so a group doesn't count unless the account is also in
// every lower one.
package groups

import (
	"fmt"
	"os/user"
)

// Config holds the configured Unix group names for each level.
type Config struct {
	ReadonlyGroup string
	AdminGroup    string
	OwnerGroup    string
}

// Level is a place on the group ladder.
type Level int

// The levels, lowest first.
const (
	LevelNone Level = iota
	LevelReadonly
	LevelAdmin
	LevelOwner
)

// Level returns how far up the ladder an account gets without a gap. The
// inGroup function reports whether the account is in the named group.
func (c Config) Level(inGroup func(group string) bool) Level {
	switch {
	case !inGroup(c.ReadonlyGroup):
		return LevelNone
	case !inGroup(c.AdminGroup):
		return LevelReadonly
	case !inGroup(c.OwnerGroup):
		return LevelAdmin
	default:
		return LevelOwner
	}
}

// Missing returns the groups an account should also be in, given the highest
// group it holds. It's empty for an account that keeps to the ladder.
func (c Config) Missing(inGroup func(group string) bool) []string {
	ladder := []string{c.ReadonlyGroup, c.AdminGroup, c.OwnerGroup}
	var missing, gaps []string
	for _, group := range ladder {
		if !inGroup(group) {
			gaps = append(gaps, group)
			continue
		}
		missing = append(missing, gaps...)
		gaps = nil
	}
	return missing
}

// Resolve returns the pispot role ("readonly" or "admin") for the given
// username. Owners get the admin role: being an owner only stops the account
// being managed from the dashboard. An account with no level gets
// ("", ErrNotAuthorized).
func Resolve(username string, cfg Config) (string, error) {
	u, err := user.Lookup(username)
	if err != nil {
		return "", fmt.Errorf("user lookup %q: %w", username, err)
	}

	gids, err := u.GroupIds()
	if err != nil {
		return "", fmt.Errorf("group ids for %q: %w", username, err)
	}

	names := make(map[string]bool, len(gids))
	for _, gid := range gids {
		g, err := user.LookupGroupId(gid)
		if err != nil {
			continue
		}
		names[g.Name] = true
	}

	switch level := cfg.Level(func(group string) bool { return names[group] }); {
	case level >= LevelAdmin:
		return "admin", nil
	case level == LevelReadonly:
		return "readonly", nil
	default:
		return "", ErrNotAuthorized
	}
}

// ErrNotAuthorized is returned when the authenticated user isn't in the
// basic access group, which every level requires.
var ErrNotAuthorized = fmt.Errorf("not in the basic access group")

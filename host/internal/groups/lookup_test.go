package groups

import (
	"strings"
	"testing"
)

func TestMissingFindsGapsInTheLadder(t *testing.T) {
	cfg := Config{ReadonlyGroup: "ro", AdminGroup: "admin", OwnerGroup: "owner"}
	cases := []struct {
		groups []string
		want   string
	}{
		{nil, ""},
		{[]string{"ro"}, ""},
		{[]string{"ro", "admin"}, ""},
		{[]string{"ro", "admin", "owner"}, ""},
		{[]string{"admin"}, "ro"},
		{[]string{"owner"}, "ro,admin"},
		{[]string{"ro", "owner"}, "admin"},
		{[]string{"admin", "owner"}, "ro"},
	}
	for _, c := range cases {
		in := map[string]bool{}
		for _, g := range c.groups {
			in[g] = true
		}
		got := strings.Join(cfg.Missing(func(group string) bool { return in[group] }), ",")
		if got != c.want {
			t.Errorf("Missing(%v) = %q, want %q", c.groups, got, c.want)
		}
	}
}

func TestLevelRequiresEveryLowerGroup(t *testing.T) {
	cfg := Config{ReadonlyGroup: "ro", AdminGroup: "admin", OwnerGroup: "owner"}
	cases := []struct {
		name   string
		groups []string
		want   Level
	}{
		{"no groups", nil, LevelNone},
		{"basic", []string{"ro"}, LevelReadonly},
		{"basic and admin", []string{"ro", "admin"}, LevelAdmin},
		{"all three", []string{"ro", "admin", "owner"}, LevelOwner},
		{"admin without basic", []string{"admin"}, LevelNone},
		{"owner alone", []string{"owner"}, LevelNone},
		{"admin and owner without basic", []string{"admin", "owner"}, LevelNone},
		{"basic and owner without admin", []string{"ro", "owner"}, LevelReadonly},
	}
	for _, c := range cases {
		in := map[string]bool{}
		for _, g := range c.groups {
			in[g] = true
		}
		got := cfg.Level(func(group string) bool { return in[group] })
		if got != c.want {
			t.Errorf("%s: Level = %d, want %d", c.name, got, c.want)
		}
	}
}

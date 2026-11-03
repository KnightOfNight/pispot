// Package users manages dashboard accounts, which are Unix accounts.
//
// An account is managed only if it's a human account (UID 1000 to 59999), in
// the basic access group, and not in the owner group. Every other account can't
// be listed or changed here, so it's managed over SSH.
//
// Every change this package makes keeps to the group ladder in package groups:
// accounts get basic access, admin can be added or removed on top of it, and
// the owner group is never touched. Membership of the owner group alone makes
// an account unmanaged, even if it's missing a group below it.
//
// Passwords are passed to chpasswd on stdin, never as arguments, and never
// logged.
package users

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mcs-net/pispot/host/internal/groups"
)

const (
	minUID = 1000
	maxUID = 59999

	minPasswordLen = 8
	maxPasswordLen = 256

	// newUserShell keeps accounts created here dashboard-only: they can't
	// open an SSH session.
	newUserShell = "/usr/sbin/nologin"

	cmdTimeout = 15 * time.Second
)

var usernamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// Errors callers can show directly to an admin.
var (
	ErrNotAdmin        = errors.New("you need to be a dashboard admin to do this")
	ErrDeleteSelf      = errors.New("you can't delete the account you're logged in as")
	ErrRemoveSelfAdmin = errors.New("you can't remove your own admin access")
	ErrInvalidUsername = errors.New("usernames must start with a lowercase letter or underscore, then use lowercase letters, digits, - or _, up to 32 characters")
	ErrInvalidPassword = fmt.Errorf("passwords must be %d to %d characters with no line breaks", minPasswordLen, maxPasswordLen)
	ErrNotManaged      = errors.New("that account isn't managed by this page")
	ErrNothingToChange = errors.New("nothing to change")
)

// Account is one managed account.
type Account struct {
	Username string `json:"username"`
	UID      int    `json:"uid"`
	Admin    bool   `json:"admin"`
}

type runFunc func(stdin string, name string, args ...string) (string, error)

// Manager reads and changes accounts.
type Manager struct {
	groups     groups.Config
	passwdPath string
	groupPath  string
	run        runFunc
	mu         sync.Mutex
}

// New returns a Manager that uses the host's account files and tools.
func New(cfg groups.Config) *Manager {
	return &Manager{
		groups:     cfg,
		passwdPath: "/etc/passwd",
		groupPath:  "/etc/group",
		run:        runCommand,
	}
}

func runCommand(stdin string, name string, args ...string) (string, error) {
	// #nosec G204 — commands are fixed; usernames are validated first.
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	timer := time.AfterFunc(cmdTimeout, func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	defer timer.Stop()
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func validatePassword(p string) error {
	if len(p) < minPasswordLen || len(p) > maxPasswordLen || strings.ContainsAny(p, "\r\n\x00") {
		return ErrInvalidPassword
	}
	return nil
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for s.Scan() {
		lines = append(lines, s.Text())
	}
	return lines, s.Err()
}

func (m *Manager) groupMembers() (map[string]map[string]bool, error) {
	return GroupMembers(m.passwdPath, m.groupPath)
}

// GroupMembers maps each group name to its members, including accounts that
// have it as their primary group, which the group file doesn't list.
func GroupMembers(passwdPath, groupPath string) (map[string]map[string]bool, error) {
	lines, err := readLines(groupPath)
	if err != nil {
		return nil, err
	}
	out := make(map[string]map[string]bool)
	nameByGID := make(map[string]string)
	for _, line := range lines {
		f := strings.Split(line, ":")
		if len(f) != 4 {
			continue
		}
		members := make(map[string]bool)
		for _, name := range strings.Split(f[3], ",") {
			if name != "" {
				members[name] = true
			}
		}
		out[f[0]] = members
		nameByGID[f[2]] = f[0]
	}
	passwd, err := readLines(passwdPath)
	if err != nil {
		return nil, err
	}
	for _, line := range passwd {
		f := strings.Split(line, ":")
		if len(f) != 7 {
			continue
		}
		if group, ok := nameByGID[f[3]]; ok {
			out[group][f[0]] = true
		}
	}
	return out, nil
}

// List returns the managed accounts, in /etc/passwd order.
func (m *Manager) List() ([]Account, error) {
	lines, err := readLines(m.passwdPath)
	if err != nil {
		return nil, err
	}
	members, err := m.groupMembers()
	if err != nil {
		return nil, err
	}
	// Without this group, owners would look like ordinary managed accounts.
	if _, ok := members[m.groups.OwnerGroup]; !ok {
		return nil, fmt.Errorf("the %s group doesn't exist, so accounts can't be managed safely", m.groups.OwnerGroup)
	}
	accounts := []Account{}
	for _, line := range lines {
		f := strings.Split(line, ":")
		if len(f) != 7 {
			continue
		}
		uid, err := strconv.Atoi(f[2])
		if err != nil || uid < minUID || uid > maxUID {
			continue
		}
		if !members[m.groups.ReadonlyGroup][f[0]] || members[m.groups.OwnerGroup][f[0]] {
			continue
		}
		accounts = append(accounts, Account{
			Username: f[0],
			UID:      uid,
			Admin:    members[m.groups.AdminGroup][f[0]],
		})
	}
	return accounts, nil
}

func (m *Manager) find(username string) (Account, bool, error) {
	accounts, err := m.List()
	if err != nil {
		return Account{}, false, err
	}
	for _, a := range accounts {
		if a.Username == username {
			return a, true, nil
		}
	}
	return Account{}, false, nil
}

// requireAdmin checks the host's groups, not the role the caller presented,
// so someone just demoted can't keep making changes.
func (m *Manager) requireAdmin(actor string) error {
	if actor == "" {
		return ErrNotAdmin
	}
	members, err := m.groupMembers()
	if err != nil {
		return err
	}
	level := m.groups.Level(func(group string) bool { return members[group][actor] })
	if level < groups.LevelAdmin {
		return ErrNotAdmin
	}
	return nil
}

func commandError(what string, out string, err error) error {
	if out != "" {
		return fmt.Errorf("%s: %s", what, out)
	}
	return fmt.Errorf("%s: %w", what, err)
}

func (m *Manager) setPassword(username, password string) error {
	// chpasswd's output is dropped in case it ever repeats its input.
	if _, err := m.run(username+":"+password+"\n", "chpasswd"); err != nil {
		return errors.New("couldn't set the password")
	}
	return nil
}

// Create adds a dashboard-only account with basic access, plus admin if asked.
func (m *Manager) Create(actor, username, password string, admin bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.requireAdmin(actor); err != nil {
		return err
	}
	if !usernamePattern.MatchString(username) {
		return ErrInvalidUsername
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	joined := m.groups.ReadonlyGroup
	if admin {
		joined += "," + m.groups.AdminGroup
	}
	args := []string{"-m", "-s", newUserShell, "-G", joined, username}
	if out, err := m.run("", "useradd", args...); err != nil {
		return commandError("couldn't create the account", out, err)
	}
	if err := m.setPassword(username, password); err != nil {
		// Don't leave behind an account with no usable password. The home
		// folder is kept, as with any other delete here.
		if out, delErr := m.run("", "userdel", username); delErr != nil {
			return fmt.Errorf("%w, and couldn't remove the new account: %s", err, out)
		}
		return err
	}
	return nil
}

// Update changes admin access, the password, or both. A nil admin or an empty
// password leaves that setting alone.
func (m *Manager) Update(actor, username string, admin *bool, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.requireAdmin(actor); err != nil {
		return err
	}
	if admin == nil && password == "" {
		return ErrNothingToChange
	}
	if password != "" {
		if err := validatePassword(password); err != nil {
			return err
		}
	}
	account, found, err := m.find(username)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotManaged
	}
	if admin != nil && !*admin && username == actor {
		return ErrRemoveSelfAdmin
	}
	if password != "" {
		if err := m.setPassword(username, password); err != nil {
			return err
		}
	}
	if admin == nil || *admin == account.Admin {
		return nil
	}
	flag := "-d"
	if *admin {
		flag = "-a"
	}
	if out, err := m.run("", "gpasswd", flag, username, m.groups.AdminGroup); err != nil {
		return commandError("couldn't change admin access", out, err)
	}
	return nil
}

// Delete removes an account but keeps its home folder, so a mistake doesn't
// also destroy files.
func (m *Manager) Delete(actor, username string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.requireAdmin(actor); err != nil {
		return err
	}
	if username == actor {
		return ErrDeleteSelf
	}
	_, found, err := m.find(username)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotManaged
	}
	if out, err := m.run("", "userdel", username); err != nil {
		return commandError("couldn't delete the account", out, err)
	}
	return nil
}

package users

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcs-net/pispot/host/internal/groups"
)

const (
	passwdFile = `root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
ctg:x:1000:1000::/home/ctg:/bin/bash
alice:x:1001:1001::/home/alice:/usr/sbin/nologin
bob:x:1002:1002::/home/bob:/usr/sbin/nologin
nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin
`
	groupFile = `root:x:0:
sudo:x:27:ctg
pispot-ro:x:990:alice
pispot-admin:x:991:ctg,bob
`
	goodPassword = "correct horse battery"
)

type call struct {
	stdin string
	cmd   string
}

type fixture struct {
	m     *Manager
	calls []call
	fail  map[string]bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	passwd := filepath.Join(dir, "passwd")
	group := filepath.Join(dir, "group")
	if err := os.WriteFile(passwd, []byte(passwdFile), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(group, []byte(groupFile), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fixture{fail: map[string]bool{}}
	f.m = &Manager{
		groups:     groups.Config{ReadonlyGroup: "pispot-ro", AdminGroup: "pispot-admin"},
		passwdPath: passwd,
		groupPath:  group,
		run: func(stdin, name string, args ...string) (string, error) {
			f.calls = append(f.calls, call{stdin, name + " " + strings.Join(args, " ")})
			if f.fail[name] {
				return "failed output", errors.New("exit 1")
			}
			return "", nil
		},
	}
	return f
}

func (f *fixture) commands() string {
	var out []string
	for _, c := range f.calls {
		out = append(out, c.cmd)
	}
	return strings.Join(out, "; ")
}

func TestListShowsOnlyHumanAccounts(t *testing.T) {
	got, err := newFixture(t).m.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d accounts, want 3: %+v", len(got), got)
	}
	ctg := got[0]
	if ctg.Username != "ctg" || !ctg.Membership.Admin || ctg.Membership.Readonly || !ctg.Sudo || !ctg.CanLogInToShell {
		t.Errorf("ctg = %+v", ctg)
	}
	if alice := got[1]; !alice.Membership.Readonly || alice.Membership.Admin || alice.Sudo || alice.CanLogInToShell {
		t.Errorf("alice = %+v", alice)
	}
}

func TestDeleteSelfRefused(t *testing.T) {
	f := newFixture(t)
	if err := f.m.Delete("ctg", "ctg"); !errors.Is(err, ErrDeleteSelf) {
		t.Errorf("err = %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("ran commands: %s", f.commands())
	}
}

func TestDeleteOther(t *testing.T) {
	f := newFixture(t)
	if err := f.m.Delete("ctg", "alice"); err != nil {
		t.Fatal(err)
	}
	// No -r: the home folder is kept.
	if got := f.commands(); got != "userdel alice" {
		t.Errorf("commands = %q", got)
	}
}

func TestSystemAccountsAreOffLimits(t *testing.T) {
	for _, name := range []string{"root", "daemon", "nobody", "missing"} {
		f := newFixture(t)
		if err := f.m.Delete("ctg", name); !errors.Is(err, ErrNotManaged) {
			t.Errorf("Delete(%s) err = %v", name, err)
		}
		if err := f.m.Update("ctg", name, nil, goodPassword); !errors.Is(err, ErrNotManaged) {
			t.Errorf("Update(%s) err = %v", name, err)
		}
		if len(f.calls) != 0 {
			t.Errorf("%s: ran commands: %s", name, f.commands())
		}
	}
}

func TestChangesRequireCurrentAdmin(t *testing.T) {
	for _, actor := range []string{"", "alice", "missing"} {
		f := newFixture(t)
		ms := Membership{Admin: true}
		errs := []error{
			f.m.Delete(actor, "bob"),
			f.m.Update(actor, actor, &ms, ""),
			f.m.Create(actor, "eve", goodPassword, ms),
		}
		for i, err := range errs {
			if !errors.Is(err, ErrNotAdmin) {
				t.Errorf("actor %q, op %d: err = %v", actor, i, err)
			}
		}
		if len(f.calls) != 0 {
			t.Errorf("actor %q: ran commands: %s", actor, f.commands())
		}
	}
}

func TestRemoveOwnAdminRefused(t *testing.T) {
	f := newFixture(t)
	err := f.m.Update("ctg", "ctg", &Membership{Readonly: true}, goodPassword)
	if !errors.Is(err, ErrRemoveSelfAdmin) {
		t.Errorf("err = %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("ran commands, including the password change: %s", f.commands())
	}
}

func TestUpdateChangesOnlyWhatDiffers(t *testing.T) {
	f := newFixture(t)
	if err := f.m.Update("ctg", "alice", &Membership{Readonly: false, Admin: true}, ""); err != nil {
		t.Fatal(err)
	}
	if got := f.commands(); got != "gpasswd -a alice pispot-admin; gpasswd -d alice pispot-ro" {
		t.Errorf("commands = %q", got)
	}

	f = newFixture(t)
	if err := f.m.Update("ctg", "alice", &Membership{Readonly: true}, ""); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 0 {
		t.Errorf("no change should run nothing: %s", f.commands())
	}
}

func TestPasswordGoesOnlyToStdin(t *testing.T) {
	f := newFixture(t)
	if err := f.m.Update("ctg", "alice", nil, goodPassword); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 1 || f.calls[0].stdin != "alice:"+goodPassword+"\n" {
		t.Fatalf("calls = %+v", f.calls)
	}
	if strings.Contains(f.calls[0].cmd, goodPassword) {
		t.Errorf("password in arguments: %q", f.calls[0].cmd)
	}
}

func TestPasswordFailureDoesNotExposeOutput(t *testing.T) {
	f := newFixture(t)
	f.fail["chpasswd"] = true
	err := f.m.Update("ctg", "alice", nil, goodPassword)
	if err == nil || strings.Contains(err.Error(), "failed output") {
		t.Errorf("err = %v", err)
	}
}

func TestCreate(t *testing.T) {
	f := newFixture(t)
	if err := f.m.Create("ctg", "eve", goodPassword, Membership{Readonly: true, Admin: true}); err != nil {
		t.Fatal(err)
	}
	want := "useradd -m -s /usr/sbin/nologin -G pispot-ro,pispot-admin eve; chpasswd "
	if got := f.commands(); got != want {
		t.Errorf("commands = %q, want %q", got, want)
	}
}

func TestCreateRollsBackWhenPasswordFails(t *testing.T) {
	f := newFixture(t)
	f.fail["chpasswd"] = true
	if err := f.m.Create("ctg", "eve", goodPassword, Membership{Readonly: true}); err == nil {
		t.Fatal("expected an error")
	}
	if got := f.commands(); !strings.HasSuffix(got, "chpasswd ; userdel eve") {
		t.Errorf("commands = %q", got)
	}
}

func TestCreateRejectsBadInput(t *testing.T) {
	badNames := []string{"", "-rf", "Alice", "a b", "a:b", "a/b", "a\nb", "$(id)", strings.Repeat("a", 33)}
	for _, name := range badNames {
		f := newFixture(t)
		if err := f.m.Create("ctg", name, goodPassword, Membership{}); !errors.Is(err, ErrInvalidUsername) {
			t.Errorf("Create(%q) err = %v", name, err)
		}
		if len(f.calls) != 0 {
			t.Errorf("Create(%q) ran commands", name)
		}
	}
	for _, pw := range []string{"short", "line\nbreak1", strings.Repeat("a", 257)} {
		f := newFixture(t)
		if err := f.m.Create("ctg", "eve", pw, Membership{}); !errors.Is(err, ErrInvalidPassword) {
			t.Errorf("password %q err = %v", pw, err)
		}
	}
}

func TestUpdateWithNothingIsAnError(t *testing.T) {
	if err := newFixture(t).m.Update("ctg", "alice", nil, ""); !errors.Is(err, ErrNothingToChange) {
		t.Errorf("err = %v", err)
	}
}

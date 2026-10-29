package gate

import (
	"testing"

	"github.com/kajisho5/assume-nothing/internal/store"
)

func fakeLookup(installed ...string) func(string) store.Fact {
	set := map[string]bool{}
	for _, n := range installed {
		set[n] = true
	}
	return func(name string) store.Fact {
		if set[name] {
			return store.Fact{Key: "cmd." + name, Status: store.OK}
		}
		return store.Fact{Key: "cmd." + name, Status: store.Missing}
	}
}

func TestStopOnceThenTrust(t *testing.T) {
	old := Lookup
	defer func() { Lookup = old }()
	Lookup = fakeLookup("npm", "node")

	st := store.New("s1", "/tmp/fictional-project")
	if m := MissingCommands("npm test", st); len(m) != 0 {
		t.Fatalf("installed command blocked: %v", m)
	}
	m := MissingCommands("zorbtool --render && npm test", st)
	if len(m) != 1 || m[0] != "zorbtool" {
		t.Fatalf("missing command not reported: %v", m)
	}
	if m := MissingCommands("zorbtool --render", st); len(m) != 0 {
		t.Fatalf("second attempt should go through, got %v", m)
	}
	if st.Commands["zorbtool"] != "allowed" {
		t.Fatalf("insisted command not remembered: %v", st.Commands)
	}
}

func TestInstallAndSelfAreNeverBlocked(t *testing.T) {
	old := Lookup
	defer func() { Lookup = old }()
	Lookup = fakeLookup("brew")

	st := store.New("s2", "/tmp/fictional-project")
	if m := MissingCommands("brew install zorbtool && zorbtool -v", st); len(m) != 0 {
		t.Fatalf("command after an install blocked: %v", m)
	}
	if m := MissingCommands("zorbtool -v && brew install zorbtool", st); len(m) != 1 {
		t.Fatalf("command before an install should still be checked: %v", m)
	}
	if m := MissingCommands("assume-nothing deep tool zorbtool", st); len(m) != 0 {
		t.Fatalf("self blocked: %v", m)
	}
}

func TestForgetKeepsKnownGood(t *testing.T) {
	st := store.New("s3", "/tmp/fictional-project")
	st.Commands["npm"] = store.OK
	st.Commands["zorbtool"] = "allowed"
	st.Denied["zorbtool"] = 1
	Forget(st)
	if st.Commands["npm"] != store.OK || st.Commands["zorbtool"] != "allowed" || len(st.Denied) != 0 {
		t.Fatalf("Forget did not reset properly: %v %v", st.Commands, st.Denied)
	}
}

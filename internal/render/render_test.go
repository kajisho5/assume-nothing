package render

import (
	"strings"
	"testing"
	"time"

	"github.com/kajisho5/assume-nothing/internal/store"
)

func TestSnapshotSections(t *testing.T) {
	st := store.New("s", "/tmp/fictional")
	st.TakenAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	st.Facts = []store.Fact{
		{Key: "os", Status: store.OK, Value: "Linux (amd64)"},
		{Key: "lang.python", Status: store.OK, Value: "python3: Python 3.99.0"},
		{Key: "cmd.python", Status: store.Missing},
		{Key: "lang.rust", Status: store.Missing},
		{Key: "tool.ffmpeg", Status: store.Missing},
		{Key: "pm.npm", Status: store.OK},
		{Key: "net", Status: store.Unknown, Why: "not checked"},
	}
	s := Snapshot(st, "x deep")
	for _, want := range []string{"CONFIRMED", "python (python3: Python 3.99.0)", "commands: python", "NOT INSTALLED", "rust", "ffmpeg", "UNKNOWN", "net: not checked", "x deep net", "Do not assume"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
}

func TestCap(t *testing.T) {
	long := strings.Repeat("あ", 3000)
	c := Cap(long, Limit)
	if n := len([]rune(c)); n > Limit {
		t.Fatalf("cap too long: %d", n)
	}
	if Cap("short", Limit) != "short" {
		t.Fatal("short text changed")
	}
}

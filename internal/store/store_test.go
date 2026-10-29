package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeNameCannotEscape(t *testing.T) {
	for _, s := range []string{"../../etc/passwd", `..\..\x`, "a/b", "", strings.Repeat("x", 500)} {
		n := safeName(s)
		if strings.ContainsAny(n, `/\.`) || n == "" || len(n) > 128 {
			t.Errorf("safeName(%q) = %q", s, n)
		}
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("ASSUME_NOTHING_DATA", t.TempDir())
	st := New("sess-1", "/tmp/fictional")
	st.SetFacts([]Fact{{Key: "lang.go", Status: OK, Value: "go1.27.3"}})
	st.SetFacts([]Fact{{Key: "lang.go", Status: Missing}, {Key: "tool.jq", Status: OK}})
	if err := Save(st); err != nil {
		t.Fatal(err)
	}
	got, err := Load("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Facts) != 2 || got.Facts[0].Status != Missing || got.Facts[1].Key != "tool.jq" {
		t.Fatalf("unexpected facts: %+v", got.Facts)
	}
	if _, err := Load("other"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	d, _ := Dir()
	if filepath.Base(d) != "sessions" {
		t.Fatalf("unexpected dir %s", d)
	}
}

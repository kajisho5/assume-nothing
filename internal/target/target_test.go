package target

import (
	"path/filepath"
	"strings"
	"testing"
)

func texts(ts []Target) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Text)
	}
	return out
}

func TestExtract(t *testing.T) {
	cases := []struct {
		prompt string
		want   []string
	}{
		{"たとえばlumen-kitについて調べて", []string{"lumen-kit"}},
		{"Zephyrdocとは？", []string{"Zephyrdoc"}},
		{"orbit-labs/quasar-cli の README を読んで", []string{"orbit-labs/quasar-cli"}},
		{"see https://example.com/docs/quasar-cli.", []string{"https://example.com/docs/quasar-cli"}},
		{"research Quasarbox and tell me", []string{"Quasarbox"}},
		{"run `nebula-build` please", []string{"nebula-build"}},
		{"「Moonpad」の料金", []string{"Moonpad"}},
		// not targets: versions, e.g., plain words, emails, pasted code
		{"update to v1.2.3, e.g. tomorrow", nil},
		{"mail me at someone@example.invalid", nil},
		{"fix this:\n```\nimport foo-bar\n```", nil},
		{"I/O and and/or are fine", nil},
		{"make it faster", nil},
		{"<pasted_content id=\"1\">\nnebula-core crashed\n</pasted_content id=\"1\">\nwhy?", nil},
	}
	for _, c := range cases {
		got := texts(Extract(c.prompt))
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("Extract(%q) = %q, want %q", c.prompt, got, c.want)
		}
	}
}

func TestExtractCaps(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString(" tool-")
		b.WriteByte(byte('a' + i))
	}
	if n := len(Extract(b.String())); n != MaxPerPrompt {
		t.Fatalf("got %d targets, want %d", n, MaxPerPrompt)
	}
}

func TestMentions(t *testing.T) {
	tg := Target{Text: "lumen-kit", Key: Key("lumen-kit")}
	for _, s := range []string{"lumen-kit is a library", "Lumen Kit has 3 modes", "the lumen_kit repo", "LUMENKIT"} {
		if !Mentions(s, tg) {
			t.Errorf("should mention: %q", s)
		}
	}
	short := Target{Text: "bun", Key: "bun"}
	if Mentions("a bundle of files", short) {
		t.Error("bun found inside bundle")
	}
	if !Mentions("use Bun 1.2", short) {
		t.Error("Bun not found")
	}
}

func TestLogFlow(t *testing.T) {
	lg := Log{Path: filepath.Join(t.TempDir(), "s", "x.targets.log")}
	st, err := lg.Read()
	if err != nil || len(st.Targets) != 0 {
		t.Fatalf("empty log: %v %v", st, err)
	}
	ts := Extract("compare lumen-kit and orbit-labs/quasar-cli")
	if err := lg.AddTargets(ts); err != nil {
		t.Fatal(err)
	}
	lg.AddTargets(ts) // duplicates are ignored on read
	st, _ = lg.Read()
	if len(st.Targets) != 2 || len(st.Unseen()) != 2 {
		t.Fatalf("targets = %v", st.Targets)
	}
	keys := st.Touched([]byte(`{"query":"lumen kit github"}`), []byte(`"no results"`))
	if len(keys) != 1 || keys[0] != "lumenkit" {
		t.Fatalf("touched = %v", keys)
	}
	lg.MarkSeen(keys, "WebSearch")
	lg.MarkWarned([]string{"orbitlabsquasarcli"})
	st, _ = lg.Read()
	if u := st.Unseen(); len(u) != 1 || u[0].Text != "orbit-labs/quasar-cli" {
		t.Fatalf("unseen = %v", u)
	}
	if !st.Warned["orbitlabsquasarcli"] || st.Seen["lumenkit"] != "WebSearch" {
		t.Fatalf("state = %+v", st)
	}
}

func TestTouchedSeesEscapedJSON(t *testing.T) {
	st := &State{Targets: []Target{{Text: "lumen-kit", Key: "lumenkit"}}, Seen: map[string]string{}}
	if k := st.Touched([]byte(`{"url":"https:\/\/example.com\/lumen-kit"}`)); len(k) != 1 {
		t.Fatalf("escaped JSON not matched: %v", k)
	}
}

func TestOwnerRepoWithoutPunctuation(t *testing.T) {
	if got := texts(Extract("orbitlabs/quasar の最新版")); strings.Join(got, "|") != "orbitlabs/quasar" {
		t.Errorf("got %q", got)
	}
	if got := Extract("client/server or yes/no"); len(got) != 0 {
		t.Errorf("common pairs taken as repos: %v", got)
	}
}

func TestLatestPrompt(t *testing.T) {
	lg := Log{Path: filepath.Join(t.TempDir(), "l.log")}
	lg.AddTargets(Extract("what is lumen-kit"))
	lg.AddTargets(nil) // a request that names nothing
	st, _ := lg.Read()
	if len(st.Latest) != 0 || len(st.Targets) != 1 {
		t.Fatalf("latest = %v, targets = %v", st.Latest, st.Targets)
	}
	lg.AddTargets(Extract("and lumen-kit again"))
	st, _ = lg.Read()
	if len(st.Latest) != 1 || st.Latest[0] != "lumenkit" {
		t.Fatalf("latest = %v", st.Latest)
	}
}

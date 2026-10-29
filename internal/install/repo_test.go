package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var sums = map[string]string{
	"assume-nothing-linux-amd64":  strings.Repeat("a", 64),
	"assume-nothing-darwin-arm64": strings.Repeat("b", 64),
}

func read(t *testing.T, root string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func count(m map[string]any, event string) (ours, others int) {
	groups, _ := m["hooks"].(map[string]any)[event].([]any)
	for _, g := range groups {
		if mentionsLauncher(g) {
			ours++
		} else {
			others++
		}
	}
	return
}

func TestRepoKeepsExistingSettingsAndIsIdempotent(t *testing.T) {
	root := t.TempDir() // fictional project "comet-notes"
	os.MkdirAll(filepath.Join(root, ".claude"), 0o755)
	existing := `{"permissions":{"allow":["Bash(npm test)"]},"hooks":{"PreToolUse":[{"matcher":"Write","hooks":[{"type":"command","command":"./lint.sh"}]}]}}`
	os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte(existing), 0o644)

	for i := 0; i < 2; i++ {
		if err := Repo(root, "9.9.9", sums); err != nil {
			t.Fatal(err)
		}
	}
	m := read(t, root)
	if m["permissions"] == nil {
		t.Fatal("other settings were lost")
	}
	for _, ev := range []string{"SessionStart", "PreToolUse", "PostToolUse"} {
		ours, _ := count(m, ev)
		if ours != 1 {
			t.Fatalf("%s: want exactly one assume-nothing hook, got %d", ev, ours)
		}
	}
	if _, others := count(m, "PreToolUse"); others != 1 {
		t.Fatal("the existing PreToolUse hook was lost")
	}
	l, err := os.ReadFile(filepath.Join(root, Launcher))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`V="9.9.9"`, "assume-nothing-linux-amd64) sum=" + strings.Repeat("a", 64), "exit 0"} {
		if !strings.Contains(string(l), want) {
			t.Errorf("launcher lacks %q", want)
		}
	}
	if info, _ := os.Stat(filepath.Join(root, Launcher)); runtime.GOOS != "windows" && info.Mode()&0o100 == 0 {
		t.Error("launcher is not executable")
	}

	if err := Remove(root); err != nil {
		t.Fatal(err)
	}
	m = read(t, root)
	if ours, others := count(m, "PreToolUse"); ours != 0 || others != 1 {
		t.Fatalf("remove: ours=%d others=%d", ours, others)
	}
	if _, ok := m["hooks"].(map[string]any)["SessionStart"]; ok {
		t.Fatal("empty event left behind")
	}
	if _, err := os.Stat(filepath.Join(root, Dir)); !os.IsNotExist(err) {
		t.Fatal("launcher folder not removed")
	}
}

func TestRepoRefusesDevBuildsAndBadJSON(t *testing.T) {
	root := t.TempDir()
	if err := Repo(root, "dev", sums); err == nil {
		t.Fatal("a dev build must not pin itself")
	}
	os.MkdirAll(filepath.Join(root, ".claude"), 0o755)
	os.WriteFile(filepath.Join(root, ".claude", "settings.json"), []byte("{ not json"), 0o644)
	if err := Repo(root, "1.0.0", sums); err == nil {
		t.Fatal("broken settings.json must be left alone with an error")
	}
	if b, _ := os.ReadFile(filepath.Join(root, ".claude", "settings.json")); string(b) != "{ not json" {
		t.Fatal("broken settings.json was overwritten")
	}
}

func TestReadChecksums(t *testing.T) {
	p := filepath.Join(t.TempDir(), "checksums.txt")
	os.WriteFile(p, []byte(strings.Repeat("c", 64)+"  assume-nothing-linux-arm64\n"+strings.Repeat("d", 64)+"  assume-nothing-windows-amd64.exe\njunk line\n"), 0o644)
	m, err := ReadChecksums(p)
	if err != nil || len(m) != 2 || m["assume-nothing-windows-amd64.exe"] != strings.Repeat("d", 64) {
		t.Fatalf("got %v %v", m, err)
	}
}

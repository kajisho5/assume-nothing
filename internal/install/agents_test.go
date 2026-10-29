package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCursorInstallMergesAndRemoves(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".cursor"), 0o755)
	mine := `{"version":1,"hooks":{"afterFileEdit":[{"command":"./hooks/format.sh"}],"stop":[{"command":"./hooks/notify.sh"}]}}`
	os.WriteFile(filepath.Join(home, ".cursor", "hooks.json"), []byte(mine), 0o644)
	path, err := Cursor(home, false)
	if err != nil {
		t.Fatal(err)
	}
	Cursor(home, false) // twice: no duplicates
	var cfg struct {
		Hooks map[string][]map[string]any `json:"hooks"`
	}
	b, _ := os.ReadFile(path)
	json.Unmarshal(b, &cfg)
	for _, ev := range CursorEvents {
		n := 0
		for _, h := range cfg.Hooks[ev] {
			if strings.Contains(h["command"].(string), "--agent cursor "+ev) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s: %d assume-nothing hooks, want 1", ev, n)
		}
	}
	if len(cfg.Hooks["stop"]) != 2 || len(cfg.Hooks["afterFileEdit"]) != 1 {
		t.Errorf("other hooks not kept: %s", b)
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor", "assume-nothing", exeName())); err != nil {
		t.Errorf("binary not copied: %v", err)
	}
	if _, err := Cursor(home, true); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "--agent cursor") || !strings.Contains(string(b), "notify.sh") {
		t.Errorf("remove left hooks or dropped others: %s", b)
	}
}

func TestOpenCodeInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	p, err := OpenCode(home, false)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "__BIN__") || !strings.Contains(string(b), "tool.execute.before") {
		t.Errorf("plugin not written: %s", b)
	}
	OpenCode(home, true)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("plugin not removed")
	}
}

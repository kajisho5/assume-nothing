package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kajisho5/assume-nothing/internal/store"
)

func TestSatisfies(t *testing.T) {
	cases := []struct {
		have, rule string
		match, ok  bool
	}{
		{"1.24.7", ">=1.27", false, true},
		{"1.27.3", ">=1.27", true, true},
		{"22.23.4", "20", false, true},
		{"20.11.1", "20", true, true},
		{"20.11.1", "20.x", true, true},
		{"22.1.0", ">=18", true, true},
		{"22.1.0", "^20.0.0", false, true},
		{"20.9.0", "^20.0.0", true, true},
		{"20.9.0", "~20.9", true, true},
		{"20.10.0", "~20.9", false, true},
		{"16.0.0", "^18 || ^20 || >=22", false, true},
		{"22.0.0", "^18 || ^20 || >=22", true, true},
		{"3.13.1", ">=3.11,<3.13", false, true},
		{"3.12.4", ">=3.11,<3.13", true, true},
		{"3.12.4", "~=3.11", true, true},
		{"3.12.4", "3.12", true, true},
		{"3.13.0", "3.12", false, true},
		{"20.0.0", "lts/*", false, false},
		{"1.80.0", "stable", false, false},
		{"20.0.0", "18 - 20", false, false},
	}
	for _, c := range cases {
		m, ok := Satisfies(c.have, c.rule)
		if m != c.match || ok != c.ok {
			t.Errorf("Satisfies(%q, %q) = %v,%v want %v,%v", c.have, c.rule, m, ok, c.match, c.ok)
		}
	}
}

func TestInstalled(t *testing.T) {
	for in, want := range map[string]string{
		"go: go version go1.24.7 linux/amd64":        "1.24.7",
		"node: v22.23.4":                             "22.23.4",
		"python3: Python 3.13.18":                    "3.13.18",
		"rustc: rustc 1.97.0 (2d8144b78 2026-07-07)": "1.97.0",
	} {
		if got := Installed(in); got != want {
			t.Errorf("Installed(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestVersionFacts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nebula-widgets") // fictional project
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/nebula\n\ngo 1.27\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".nvmrc"), []byte("v20\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"nebula","engines":{"node":">=18"}}`), 0o644)
	os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[project]\nname = \"nebula\"\nrequires-python = \">=3.11\"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".ruby-version"), []byte("ruby-3.3.0\n"), 0o644)
	have := []store.Fact{
		{Key: "lang.go", Status: store.OK, Value: "go: go version go1.24.7 linux/amd64"},
		{Key: "lang.node", Status: store.OK, Value: "node: v22.23.4"},
		{Key: "lang.python", Status: store.OK, Value: "python3: Python 3.13.1"},
		{Key: "lang.ruby", Status: store.Missing},
	}
	got := map[string]store.Fact{}
	for _, f := range VersionFacts(dir, have) {
		got[f.Key] = f
	}
	want := map[string]string{
		"ver.go go.mod (go)":                          store.Missing,
		"ver.node .nvmrc":                             store.Missing,
		"ver.node package.json (engines.node)":        store.OK,
		"ver.python pyproject.toml (requires-python)": store.OK,
		"ver.ruby .ruby-version":                      store.Missing,
	}
	for k, st := range want {
		if got[k].Status != st {
			t.Errorf("%s: status %q, want %q (%+v)", k, got[k].Status, st, got[k])
		}
	}
	if !strings.Contains(got["ver.go go.mod (go)"].Why, "GOTOOLCHAIN") {
		t.Error("go mismatch lacks the toolchain note")
	}
	if !strings.Contains(got["ver.ruby .ruby-version"].Value, "not installed") {
		t.Errorf("ruby: %+v", got["ver.ruby .ruby-version"])
	}
}

func TestRetryLangsOnlyTimedOut(t *testing.T) {
	facts := []store.Fact{
		{Key: "lang.go", Status: store.Unknown, Why: "found on PATH but version check failed: signal: killed"},
		{Key: "lang.node", Status: store.OK, Value: "node: v22.0.0"},
		{Key: "lang.swift", Status: store.Missing},
	}
	got := RetryLangs(facts)
	if len(got) != 1 || got[0].Key != "lang.go" {
		t.Fatalf("retried %v, want only lang.go", got)
	}
	if TimedOut(got[0]) {
		t.Errorf("go still timed out with more time: %+v", got[0])
	}
}

func TestSkipped(t *testing.T) {
	fs := []store.Fact{{Key: "tool.docker"}, {Key: "lang.java"}, {Key: "tool.git"}, {Key: "ver.java pom.xml"}}
	got := Skipped(fs, []string{"Docker", "java"})
	if len(got) != 2 || got[0].Key != "tool.git" || got[1].Key != "ver.java pom.xml" {
		t.Fatalf("skipped = %v", got)
	}
}

func TestJavaAndVolta(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project><properties><maven.compiler.release>25</maven.compiler.release></properties></project>"), 0o644)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"nebula","volta":{"node":"20.11.0"}}`), 0o644)
	have := []store.Fact{
		{Key: "lang.java", Status: store.OK, Value: `java: openjdk version "21.0.12.1" 2026-08-18`},
		{Key: "lang.node", Status: store.OK, Value: "node: v22.1.0"},
	}
	got := map[string]store.Fact{}
	for _, f := range VersionFacts(dir, have) {
		got[f.Key] = f
	}
	if got["ver.java pom.xml"].Status != store.Missing {
		t.Errorf("java 25 asked, 21 installed: %+v", got["ver.java pom.xml"])
	}
	if got["ver.node package.json (volta.node)"].Status != store.Missing {
		t.Errorf("volta node 20.11.0 asked, 22 installed: %+v", got["ver.node package.json (volta.node)"])
	}
	g := t.TempDir()
	os.WriteFile(filepath.Join(g, "build.gradle.kts"), []byte("java { toolchain { languageVersion.set(JavaLanguageVersion.of(17)) } }"), 0o644)
	if fs := VersionFacts(g, have); len(fs) != 1 || fs[0].Status != store.OK {
		t.Errorf("gradle 17 with java 21 installed: %+v", fs)
	}
}

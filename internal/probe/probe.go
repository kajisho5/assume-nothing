// Package probe collects facts about the machine and the working folder.
//
// Every check has its own time limit. A check that fails or runs out of time
// reports "unknown" instead of guessing.
package probe

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kajisho5/assume-nothing/internal/store"
)

// Budget is the whole time allowed for the start-of-session probe.
const Budget = 2 * time.Second

const cmdTimeout = 1500 * time.Millisecond

// Lang is a language runtime and how to ask for its version.
type Lang struct {
	Key   string
	Names []string // first one found on PATH wins
	Args  []string
}

// Langs is the list of language runtimes checked at session start.
var Langs = []Lang{
	{"lang.python", []string{"python3", "python"}, []string{"--version"}},
	{"lang.node", []string{"node"}, []string{"--version"}},
	{"lang.go", []string{"go"}, []string{"version"}},
	{"lang.rust", []string{"rustc"}, []string{"--version"}},
	{"lang.java", []string{"java"}, []string{"-version"}},
	{"lang.swift", []string{"swift"}, []string{"--version"}},
	{"lang.dotnet", []string{"dotnet"}, []string{"--version"}},
	{"lang.ruby", []string{"ruby"}, []string{"--version"}},
	{"lang.php", []string{"php"}, []string{"--version"}},
	{"lang.deno", []string{"deno"}, []string{"--version"}},
	{"lang.bun", []string{"bun"}, []string{"--version"}},
}

// PackageManagers are only checked for presence (fast).
var PackageManagers = []string{"pip3", "pip", "uv", "npm", "pnpm", "yarn", "cargo", "brew", "apt-get", "dnf", "pacman", "winget", "choco", "scoop"}

// Tools are common command-line tools checked for presence.
var Tools = []string{"git", "gh", "docker", "make", "curl", "wget", "jq", "rg", "ffmpeg"}

// Markers map a file in the working folder to the kind of project it means.
var Markers = []struct{ File, Kind string }{
	{"package.json", "Node"},
	{"pyproject.toml", "Python"},
	{"requirements.txt", "Python"},
	{"go.mod", "Go"},
	{"Cargo.toml", "Rust"},
	{"pom.xml", "Java (Maven)"},
	{"build.gradle", "Java/Kotlin (Gradle)"},
	{"build.gradle.kts", "Java/Kotlin (Gradle)"},
	{"Package.swift", "Swift"},
	{"Gemfile", "Ruby"},
	{"composer.json", "PHP"},
	{"deno.json", "Deno"},
	{"CMakeLists.txt", "C/C++ (CMake)"},
	{"Makefile", "Make"},
	{"Dockerfile", "Docker"},
	{"wrangler.toml", "Cloudflare Workers"},
	{"wrangler.jsonc", "Cloudflare Workers"},
}

// InstructionFiles are files that agents read as instructions.
var InstructionFiles = []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md", ".cursorrules", ".github/copilot-instructions.md"}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// Clean makes text safe to show to an agent: one line, no control characters, bounded length.
func Clean(s string, max int) string {
	s = ansi.ReplaceAllString(s, "")
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		if r < 0x20 || r == 0x7f || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if max > 0 && len([]rune(out)) > max {
		out = string([]rune(out)[:max-1]) + "…"
	}
	return out
}

// firstLine runs a command and returns the first non-empty line of its output.
func firstLine(ctx context.Context, name string, args ...string) (string, error) {
	c, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(c, name, args...)
	cmd.Stdin = nil
	// Never let a version check download anything (Go would fetch a newer toolchain).
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	cmd.WaitDelay = 200 * time.Millisecond
	err := cmd.Run()
	sc := bufio.NewScanner(&buf)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || isNoise(l) {
			continue
		}
		return Clean(l, 80), err
	}
	if err == nil {
		err = fmt.Errorf("no output")
	}
	return "", err
}

// noise is output that comes before the real version line.
var noise = []string{"Picked up JAVA_TOOL_OPTIONS", "Picked up _JAVA_OPTIONS", "Picked up JDK_JAVA_OPTIONS", "go: downloading", "WARNING:", "Warning:"}

func isNoise(l string) bool {
	for _, p := range noise {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return false
}

func lookPath(names ...string) (string, string, bool) {
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return n, p, true
		}
	}
	return "", "", false
}

// Command checks one command name on PATH.
func Command(name string) store.Fact {
	key := "cmd." + name
	if p, err := exec.LookPath(name); err == nil {
		return store.Fact{Key: key, Status: store.OK, Value: p, How: "PATH lookup"}
	}
	return store.Fact{Key: key, Status: store.Missing, How: "PATH lookup"}
}

func langFact(ctx context.Context, l Lang) store.Fact {
	name, _, ok := lookPath(l.Names...)
	if !ok {
		return store.Fact{Key: l.Key, Status: store.Missing, How: "PATH lookup: " + strings.Join(l.Names, ", ")}
	}
	how := name + " " + strings.Join(l.Args, " ")
	v, err := firstLine(ctx, name, l.Args...)
	if v == "" {
		why := "found on PATH but version check failed"
		if err != nil && ctx.Err() == nil {
			why += ": " + Clean(err.Error(), 60)
		}
		return store.Fact{Key: l.Key, Status: store.Unknown, Value: name, How: how, Why: why}
	}
	return store.Fact{Key: l.Key, Status: store.OK, Value: v, How: how}
}

func presence(prefix string, names []string) []store.Fact {
	out := make([]store.Fact, 0, len(names))
	for _, n := range names {
		if _, err := exec.LookPath(n); err == nil {
			out = append(out, store.Fact{Key: prefix + n, Status: store.OK, How: "PATH lookup"})
		} else {
			out = append(out, store.Fact{Key: prefix + n, Status: store.Missing, How: "PATH lookup"})
		}
	}
	return out
}

// Config is the optional per-project file .assume-nothing.json.
type Config struct {
	Tools []string `json:"tools"`
}

var toolName = regexp.MustCompile(`^[A-Za-z0-9._+-]{1,64}$`)

// LoadConfig reads .assume-nothing.json from the working folder, if present.
func LoadConfig(cwd string) Config {
	var c Config
	b, err := os.ReadFile(filepath.Join(cwd, ".assume-nothing.json"))
	if err != nil {
		return c
	}
	_ = json.Unmarshal(b, &c)
	kept := c.Tools[:0]
	for _, t := range c.Tools {
		if toolName.MatchString(t) {
			kept = append(kept, t)
		}
	}
	c.Tools = kept
	return c
}

// ToolFacts checks languages, package managers and tools.
func ToolFacts(ctx context.Context, extra []string) []store.Fact {
	var mu sync.Mutex
	var wg sync.WaitGroup
	langs := make([]store.Fact, len(Langs))
	for i, l := range Langs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f := langFact(ctx, l)
			mu.Lock()
			langs[i] = f
			mu.Unlock()
		}()
	}
	wg.Wait()
	out := append([]store.Fact{}, langs...)
	out = append(out, presence("pm.", PackageManagers)...)
	tools := append([]string{}, Tools...)
	for _, t := range extra {
		if !contains(tools, t) {
			tools = append(tools, t)
		}
	}
	out = append(out, presence("tool.", tools)...)
	return out
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// TargetFacts checks the working folder.
func TargetFacts(ctx context.Context, cwd string) []store.Fact {
	var out []store.Fact
	out = append(out, store.Fact{Key: "cwd", Status: store.OK, Value: Clean(cwd, 200), How: "hook input"})

	entries, err := os.ReadDir(cwd)
	if err != nil {
		out = append(out, store.Fact{Key: "dir.entries", Status: store.Unknown, How: "read folder", Why: Clean(err.Error(), 80)})
	} else {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			n := Clean(e.Name(), 60)
			if e.IsDir() {
				n += "/"
			}
			names = append(names, n)
		}
		sort.Strings(names)
		total := len(names)
		if len(names) > 20 {
			names = names[:20]
		}
		v := fmt.Sprintf("%d entries", total)
		if len(names) > 0 {
			v += ": " + strings.Join(names, ", ")
			if total > 20 {
				v += ", …"
			}
		}
		out = append(out, store.Fact{Key: "dir.entries", Status: store.OK, Value: v, How: "read folder (first 20 names)"})
	}

	var kinds []string
	for _, m := range Markers {
		if _, err := os.Stat(filepath.Join(cwd, m.File)); err == nil {
			kinds = append(kinds, m.Kind+" ("+m.File+")")
		}
	}
	if len(kinds) > 0 {
		out = append(out, store.Fact{Key: "project", Status: store.OK, Value: strings.Join(kinds, ", "), How: "marker files"})
	} else {
		out = append(out, store.Fact{Key: "project", Status: store.Missing, How: "marker files", Why: "no known project file in this folder"})
	}

	var ins []string
	for _, f := range InstructionFiles {
		if _, err := os.Stat(filepath.Join(cwd, f)); err == nil {
			ins = append(ins, f)
		}
	}
	if len(ins) > 0 {
		out = append(out, store.Fact{Key: "instructions", Status: store.OK, Value: strings.Join(ins, ", "), How: "file exists (not read)"})
	} else {
		out = append(out, store.Fact{Key: "instructions", Status: store.Missing, How: "file exists"})
	}

	out = append(out, gitFact(ctx, cwd))
	return out
}

func gitFact(ctx context.Context, cwd string) store.Fact {
	f := store.Fact{Key: "git", How: "git rev-parse / status / remote"}
	if _, err := exec.LookPath("git"); err != nil {
		f.Status, f.Why = store.Unknown, "git is not installed"
		return f
	}
	run := func(args ...string) (string, error) {
		c, cancel := context.WithTimeout(ctx, cmdTimeout)
		defer cancel()
		cmd := exec.CommandContext(c, "git", append([]string{"-C", cwd}, args...)...)
		cmd.WaitDelay = 200 * time.Millisecond
		b, err := cmd.Output()
		return string(b), err
	}
	inside, err := run("rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(inside) != "true" {
		f.Status, f.Why = store.Missing, "not inside a git repository"
		return f
	}
	parts := []string{}
	if br, err := run("rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		parts = append(parts, "branch "+Clean(br, 60))
	}
	if st, err := run("status", "--porcelain"); err == nil {
		n := 0
		for _, l := range strings.Split(st, "\n") {
			if strings.TrimSpace(l) != "" {
				n++
			}
		}
		parts = append(parts, strconv.Itoa(n)+" uncommitted changes")
	}
	if rm, err := run("remote"); err == nil {
		n := len(strings.Fields(rm))
		parts = append(parts, strconv.Itoa(n)+" remote(s)")
	}
	f.Status, f.Value = store.OK, strings.Join(parts, ", ")
	return f
}

// SystemFacts checks the operating system, shell, disk and memory.
func SystemFacts(ctx context.Context, cwd string) []store.Fact {
	out := []store.Fact{osFact(ctx), shellFact(ctx)}
	if free, err := diskFree(cwd); err == nil {
		out = append(out, store.Fact{Key: "disk.free", Status: store.OK, Value: human(free), How: "filesystem stats for the working folder"})
	} else {
		out = append(out, store.Fact{Key: "disk.free", Status: store.Unknown, Why: Clean(err.Error(), 60)})
	}
	if mem, err := memTotal(ctx); err == nil {
		out = append(out, store.Fact{Key: "mem.total", Status: store.OK, Value: human(mem), How: "system memory query"})
	} else {
		out = append(out, store.Fact{Key: "mem.total", Status: store.Unknown, Why: Clean(err.Error(), 60)})
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		out = append(out, store.Fact{Key: "container", Status: store.OK, Value: "/.dockerenv present", How: "file exists"})
	}
	return out
}

func osFact(ctx context.Context) store.Fact {
	f := store.Fact{Key: "os", Status: store.OK, How: "runtime + system version"}
	name := runtime.GOOS
	ver := ""
	switch runtime.GOOS {
	case "darwin":
		name = "macOS"
		ver, _ = firstLine(ctx, "sw_vers", "-productVersion")
	case "linux":
		if b, err := os.ReadFile("/etc/os-release"); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if v, ok := strings.CutPrefix(l, "PRETTY_NAME="); ok {
					ver = Clean(strings.Trim(v, `"`), 60)
				}
			}
		}
		name = "Linux"
	case "windows":
		name = "Windows"
		ver, _ = firstLine(ctx, "cmd", "/c", "ver")
	}
	f.Value = strings.TrimSpace(name + " " + ver + " (" + runtime.GOARCH + ")")
	return f
}

func shellFact(ctx context.Context) store.Fact {
	f := store.Fact{Key: "shell", How: "SHELL / ComSpec"}
	sh := os.Getenv("SHELL")
	if sh == "" && runtime.GOOS == "windows" {
		if os.Getenv("PSModulePath") != "" {
			f.Status, f.Value = store.OK, "PowerShell (likely)"
			return f
		}
		sh = os.Getenv("ComSpec")
	}
	if sh == "" {
		f.Status, f.Why = store.Unknown, "SHELL is not set"
		return f
	}
	base := filepath.Base(sh)
	f.Status, f.Value = store.OK, Clean(base, 40)
	if strings.Contains(base, "sh") && runtime.GOOS != "windows" {
		if v, err := firstLine(ctx, sh, "--version"); err == nil && v != "" {
			f.Value = Clean(v, 60)
		}
	}
	return f
}

func human(n uint64) string {
	const gb = 1 << 30
	if n >= gb {
		return fmt.Sprintf("%.1f GB", float64(n)/gb)
	}
	return fmt.Sprintf("%d MB", n>>20)
}

// Environment is the whole start-of-session probe.
func Environment(cwd string) []store.Fact {
	ctx, cancel := context.WithTimeout(context.Background(), Budget)
	defer cancel()
	cfg := LoadConfig(cwd)
	var sys, tgt, tools []store.Fact
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); sys = SystemFacts(ctx, cwd) }()
	go func() { defer wg.Done(); tgt = TargetFacts(ctx, cwd) }()
	go func() { defer wg.Done(); tools = ToolFacts(ctx, cfg.Tools) }()
	wg.Wait()
	out := append(sys, tgt...)
	out = append(out, tools...)
	out = append(out, store.Fact{Key: "net", Status: store.Unknown, Why: "not checked at start (run the deep net probe)"})
	return out
}

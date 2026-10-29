// Command assume-nothing makes coding agents probe their environment before they act.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kajisho5/assume-nothing/internal/hook"
	"github.com/kajisho5/assume-nothing/internal/install"
	"github.com/kajisho5/assume-nothing/internal/probe"
	"github.com/kajisho5/assume-nothing/internal/render"
	"github.com/kajisho5/assume-nothing/internal/store"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `assume-nothing — make your coding agent probe before it acts.

Usage:
  assume-nothing hook [--agent claude|codex|gemini] <event>          (reads the agent's JSON on stdin)
        events: session-start, user-prompt-submit, pre-tool-use, post-tool-use, stop
              (also SessionStart, UserPromptSubmit, PreToolUse, BeforeAgent, BeforeTool, AfterAgent, ...)
  assume-nothing probe [--json] [--cwd DIR]                         print the session probe
  assume-nothing deep net [--json]                                  check outbound network access
  assume-nothing deep tool <name> [--json]                          check one tool in depth
  assume-nothing install-repo --checksums FILE [DIR]                  set up hooks in a repository (works in cloud sessions)
  assume-nothing install-repo --remove [DIR]                         take them out again
  assume-nothing version
`

func main() {
	hook.DeepCommand = deepCommand()
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "hook":
		os.Exit(runHook(os.Args[2:]))
	case "probe":
		os.Exit(runProbe(os.Args[2:]))
	case "deep":
		os.Exit(runDeep(os.Args[2:]))
	case "install-repo":
		os.Exit(runInstallRepo(os.Args[2:]))
	case "version", "--version", "-v":
		fmt.Println("assume-nothing", version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

func runInstallRepo(args []string) int {
	fs := flag.NewFlagSet("install-repo", flag.ContinueOnError)
	sums := fs.String("checksums", "", "the release's checksums.txt")
	remove := fs.Bool("remove", false, "remove the hooks and the launcher")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	if *remove {
		if err := install.Remove(dir); err != nil {
			fmt.Fprintln(os.Stderr, "assume-nothing:", err)
			return 1
		}
		fmt.Println("assume-nothing: removed from", dir)
		return 0
	}
	if *sums == "" {
		fmt.Fprintln(os.Stderr, "assume-nothing: --checksums is required (the release's checksums.txt)")
		return 2
	}
	m, err := install.ReadChecksums(*sums)
	if err == nil {
		err = install.Repo(dir, version, m)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "assume-nothing:", err)
		return 1
	}
	fmt.Printf("assume-nothing v%s set up in %s\n  %s\n  .claude/settings.json (5 hooks)\nCommit both, and every Claude Code session on this repository probes first, cloud sessions included.\n", version, dir, install.Launcher)
	return 0
}

// deepCommand returns the command line an agent should use for deeper checks.
func deepCommand() string {
	if l := os.Getenv("ASSUME_NOTHING_LAUNCHER"); l != "" {
		return `"` + filepath.ToSlash(l) + `" deep`
	}
	root := os.Getenv("CLAUDE_PLUGIN_ROOT")
	if root == "" {
		if exe, err := os.Executable(); err == nil {
			if d := filepath.Dir(exe); filepath.Base(d) == "libexec" {
				root = filepath.Dir(d)
			}
		}
	}
	if root != "" {
		script := filepath.ToSlash(filepath.Join(root, "scripts", "run.sh"))
		if _, err := os.Stat(script); err == nil {
			return `"` + script + `" deep`
		}
	}
	if exe, err := os.Executable(); err == nil {
		return `"` + filepath.ToSlash(exe) + `" deep`
	}
	return "assume-nothing deep"
}

// runHook never fails the agent: on any error it prints nothing and exits 0,
// so a broken hook cannot lock the agent out. Errors go to stderr only.
func runHook(args []string) (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, "assume-nothing: internal error:", r)
			code = 0
		}
	}()
	agent := hook.Claude
	var rest []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--agent" && i+1 < len(args):
			agent = args[i+1]
			i++
		case strings.HasPrefix(a, "--agent="):
			agent = strings.TrimPrefix(a, "--agent=")
		default:
			rest = append(rest, a)
		}
	}
	args = rest
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "assume-nothing: missing hook event")
		return 0
	}
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 16<<20))
	if err != nil {
		fmt.Fprintln(os.Stderr, "assume-nothing: reading stdin:", err)
		return 0
	}
	out, err := hook.Run(args[0], data)
	if err != nil {
		fmt.Fprintln(os.Stderr, "assume-nothing:", err)
		return 0
	}
	b, err := hook.Encode(out, hook.DetectAgent(data, agent))
	if err != nil {
		fmt.Fprintln(os.Stderr, "assume-nothing:", err)
		return 0
	}
	if len(b) > 0 {
		os.Stdout.Write(append(b, '\n'))
	}
	return 0
}

func runProbe(args []string) int {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	cwd := fs.String("cwd", "", "folder to probe (default: current folder)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dir := *cwd
	if dir == "" {
		dir, _ = os.Getwd()
	}
	st := store.New("manual", dir)
	st.TakenAt = time.Now()
	st.Facts = probe.Environment(dir)
	if *asJSON {
		return printJSON(st)
	}
	fmt.Println(render.Snapshot(st, hook.DeepCommand))
	return 0
}

func runDeep(args []string) int {
	if len(args) < 1 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	topic, rest := args[0], args[1:]
	fs := flag.NewFlagSet("deep", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	// allow flags before or after the tool name
	var pos []string
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			return 2
		}
		rest = fs.Args()
		if len(rest) > 0 {
			pos = append(pos, rest[0])
			rest = rest[1:]
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var facts []store.Fact
	switch topic {
	case "net":
		facts = probe.Net(ctx)
	case "tool":
		if len(pos) != 1 {
			fmt.Fprintln(os.Stderr, "usage: assume-nothing deep tool <name>")
			return 2
		}
		facts = probe.Tool(ctx, pos[0])
	default:
		fmt.Fprintf(os.Stderr, "unknown deep check %q (use: net, tool)\n", topic)
		return 2
	}
	if *asJSON {
		return printJSON(facts)
	}
	fmt.Printf("[assume-nothing: deep %s, %s %s/%s]\n", strings.Join(append([]string{topic}, pos...), " "), time.Now().Format(time.RFC3339), runtime.GOOS, runtime.GOARCH)
	fmt.Print(render.Facts(facts))
	return 0
}

func printJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

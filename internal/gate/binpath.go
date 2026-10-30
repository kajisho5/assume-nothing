package gate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kajisho5/assume-nothing/internal/store"
)

// Unreachable is a command used after a global install in the same line,
// when the folder the install puts commands in is not on PATH. The install
// will work and the next command will still fail with "command not found".
type Unreachable struct {
	Manager string // e.g. "npm"
	Name    string // the command that will not be found, e.g. "pnpm"
	Dir     string // where the install puts it
}

// BinDir returns the folder where a global install by c puts its commands,
// or "" when it cannot tell. It is a variable so tests can replace it.
var BinDir = binDir

// PathDirs is the PATH the agent's shell will search. A variable for tests.
var PathDirs = func() []string { return filepath.SplitList(os.Getenv("PATH")) }

// UnreachableAfterInstall finds the first command in line that comes after a
// global install, is not installed, and will not be found because the
// install's folder is not on PATH. It only reports what it can show exactly.
func UnreachableAfterInstall(line string) *Unreachable {
	var dir, manager string
	for _, c := range Commands(line) {
		if IsInstall(c) {
			if d := BinDir(c); d != "" && !onPath(d) {
				dir, manager = d, c.Name
			}
			continue
		}
		if dir == "" || c.Name == Self || strings.ContainsAny(c.Name, `/\`) {
			continue
		}
		if Lookup(c.Name).Status == store.OK {
			continue
		}
		return &Unreachable{Manager: manager, Name: c.Name, Dir: dir}
	}
	return nil
}

func onPath(dir string) bool {
	want := clean(dir)
	for _, p := range PathDirs() {
		if p != "" && clean(p) == want {
			return true
		}
	}
	return false
}

func clean(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

func has(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f {
				return true
			}
		}
	}
	return false
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

// output runs a short query command and returns its first line.
func output(name string, args ...string) string {
	if _, err := exec.LookPath(name); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(b), "\n", 2)[0])
}

func binDir(c Cmd) string {
	switch c.Name {
	case "npm":
		if !has(c.Args, "-g", "--global", "--location=global") {
			return ""
		}
		prefix := os.Getenv("NPM_CONFIG_PREFIX")
		if prefix == "" {
			prefix = os.Getenv("npm_config_prefix")
		}
		if prefix == "" {
			prefix = output("npm", "config", "get", "prefix")
		}
		if prefix == "" {
			return ""
		}
		if runtime.GOOS == "windows" {
			return prefix
		}
		return filepath.Join(prefix, "bin")
	case "pnpm":
		if !has(c.Args, "-g", "--global") {
			return ""
		}
		return os.Getenv("PNPM_HOME")
	case "bun":
		if !has(c.Args, "-g", "--global") {
			return ""
		}
		if b := os.Getenv("BUN_INSTALL"); b != "" {
			return filepath.Join(b, "bin")
		}
		return filepath.Join(home(), ".bun", "bin")
	case "pip", "pip3":
		if !has(c.Args, "--user") {
			return ""
		}
		base := os.Getenv("PYTHONUSERBASE")
		if base == "" {
			py := "python3"
			if runtime.GOOS == "windows" {
				py = "python"
			}
			base = output(py, "-m", "site", "--user-base")
		}
		if base == "" {
			return ""
		}
		if runtime.GOOS == "windows" {
			return "" // the folder name includes the Python version
		}
		return filepath.Join(base, "bin")
	case "cargo":
		if r := os.Getenv("CARGO_INSTALL_ROOT"); r != "" {
			return filepath.Join(r, "bin")
		}
		if h := os.Getenv("CARGO_HOME"); h != "" {
			return filepath.Join(h, "bin")
		}
		return filepath.Join(home(), ".cargo", "bin")
	case "go":
		if b := os.Getenv("GOBIN"); b != "" {
			return b
		}
		gp := os.Getenv("GOPATH")
		if gp == "" {
			gp = output("go", "env", "GOPATH")
		}
		if gp == "" {
			return ""
		}
		return filepath.Join(filepath.SplitList(gp)[0], "bin")
	}
	return ""
}

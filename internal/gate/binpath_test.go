package gate

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestUnreachableAfterInstall(t *testing.T) {
	oldL, oldB, oldP := Lookup, BinDir, PathDirs
	defer func() { Lookup, BinDir, PathDirs = oldL, oldB, oldP }()
	Lookup = fakeLookup("npm", "node", "tail")
	off := filepath.Join(t.TempDir(), "fictional-prefix", "bin")
	on := t.TempDir()
	PathDirs = func() []string { return []string{on} }

	BinDir = func(c Cmd) string {
		if c.Name == "npm" && has(c.Args, "-g") {
			return off
		}
		return ""
	}
	u := UnreachableAfterInstall("npm install -g zorbpkg@9 2>&1 | tail -3 && zorbpkg -v && zorbpkg install")
	if u == nil || u.Name != "zorbpkg" || u.Dir != off || u.Manager != "npm" {
		t.Fatalf("got %+v", u)
	}
	for _, line := range []string{
		"npm install zorbpkg && zorbpkg -v",                // local install: not this check
		"npm install -g zorbpkg",                           // nothing used after it
		"npm install -g zorbpkg && node -v",                // the next command is installed
		"npm install -g zorbpkg && " + off + "/zorbpkg -v", // full path
		"zorbpkg -v && npm install -g zorbpkg",             // before the install: the normal gate's job
	} {
		if u := UnreachableAfterInstall(line); u != nil {
			t.Errorf("%q: got %+v", line, u)
		}
	}
	BinDir = func(Cmd) string { return on } // folder on PATH
	if u := UnreachableAfterInstall("npm install -g zorbpkg && zorbpkg -v"); u != nil {
		t.Errorf("folder on PATH still reported: %+v", u)
	}
}

func TestBinDirFlags(t *testing.T) {
	t.Setenv("NPM_CONFIG_PREFIX", "/fictional/prefix")
	t.Setenv("CARGO_INSTALL_ROOT", "/fictional/cargo")
	t.Setenv("GOBIN", "/fictional/gobin")
	cases := map[string]string{
		"npm i -g zorbpkg":                       filepath.Join("/fictional/prefix", "bin"),
		"npm i zorbpkg":                          "",
		"cargo install zorbtool":                 filepath.Join("/fictional/cargo", "bin"),
		"go install example.com/zorbtool@latest": "/fictional/gobin",
		"pip install zorbpkg":                    "",
	}
	if runtime.GOOS == "windows" {
		cases["npm i -g zorbpkg"] = "/fictional/prefix"
	}
	for line, want := range cases {
		if got := binDir(Commands(line)[0]); got != want {
			t.Errorf("%q: got %q, want %q", line, got, want)
		}
	}
}

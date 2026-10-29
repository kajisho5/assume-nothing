package gate

import (
	"reflect"
	"testing"
)

func TestCommandNames(t *testing.T) {
	cases := []struct {
		line string
		want []string
	}{
		{"npm test", []string{"npm"}},
		{"cd app && npm ci && npm run build", []string{"npm"}},
		{"FOO=1 BAR=2 python3 -m pytest -q", []string{"python3"}},
		{"sudo -E apt-get install -y jq", []string{"apt-get"}},
		{"env NODE_ENV=test node index.js", []string{"node"}},
		{"timeout 30 ffmpeg -i in.mov out.mp4", []string{"ffmpeg"}},
		{"cat file.txt | grep -n foo | sort | uniq -c", []string{"cat", "grep", "sort", "uniq"}},
		{"echo $(git rev-parse HEAD)", []string{"git"}},
		{"echo `whoami`", []string{"whoami"}},
		{"./scripts/build.sh --fast", nil},
		{"/usr/bin/env python3 x.py", nil},
		{"echo 'npm install && rm -rf /'", nil},
		{`echo "a; b | c"`, nil},
		{"# just a comment\nls -la", []string{"ls"}},
		{"if command -v jq >/dev/null; then jq . a.json; fi", []string{"jq"}},
		{"for f in *.mov; do ffprobe \"$f\"; done", []string{"ffprobe"}},
		{"cat <<EOF > notes.md\nmissingtool should not be read\nEOF\nwc -l notes.md", []string{"cat", "wc"}},
		{"cat <<-'END'\n\tsomething here\n\tEND\nls", []string{"cat", "ls"}},
		{"make 2>&1 | tee build.log", []string{"make", "tee"}},
		{"$EDITOR file.txt", nil},
		{"(cd sub && go test ./...)", []string{"go"}},
		{"python3 -c 'print(1)' > out.txt && rg TODO", []string{"python3", "rg"}},
		{"npm test; npm test", []string{"npm"}},
		{"x=$(jq -r .name package.json); echo $x", []string{"jq"}},
		{"cat file \\\n  | wc -l", []string{"cat", "wc"}},
	}
	for _, c := range cases {
		got := CommandNames(c.line)
		if len(got) == 0 && len(c.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("CommandNames(%q) = %v, want %v", c.line, got, c.want)
		}
	}
}

func TestInstallsSomething(t *testing.T) {
	yes := []string{"pip install requests", "npm i -D vite", "brew install ffmpeg", "sudo apt-get install -y jq", "uv add httpx", "go install golang.org/x/tools/gopls@latest", "cargo install ripgrep", "pnpm add zod"}
	no := []string{"npm test", "pip list", "go build ./...", "brew list", "echo install"}
	for _, l := range yes {
		if !InstallsSomething(l) {
			t.Errorf("InstallsSomething(%q) = false, want true", l)
		}
	}
	for _, l := range no {
		if InstallsSomething(l) {
			t.Errorf("InstallsSomething(%q) = true, want false", l)
		}
	}
}

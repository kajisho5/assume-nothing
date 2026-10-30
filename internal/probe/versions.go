package probe

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/kajisho5/assume-nothing/internal/store"
)

// Requirement is a version a project asks for, read from one of its version files.
type Requirement struct {
	Lang   string // key of the language fact, e.g. "lang.go"
	Name   string // short name, e.g. "go"
	Rule   string // as written, e.g. ">=18", "1.27", "3.12"
	Source string // where it was read, e.g. "go.mod (go)"
	Note   string // extra advice when it does not match
}

// Only these files are read, and only the version field in each.
// Nothing else in a project is opened.
const maxVersionFile = 64 << 10

func readSmall(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxVersionFile))
	if err != nil {
		return "", false
	}
	return string(b), true
}

func firstWord(s string) string {
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		return strings.Fields(l)[0]
	}
	return ""
}

var (
	goLine        = regexp.MustCompile(`(?m)^go\s+([0-9][0-9.]*)\s*$`)
	toolchainLine = regexp.MustCompile(`(?m)^toolchain\s+go([0-9][0-9.]*)\S*\s*$`)
	requiresPy    = regexp.MustCompile(`(?m)^\s*requires-python\s*=\s*["']([^"']+)["']`)
	rustChannel   = regexp.MustCompile(`(?m)^\s*channel\s*=\s*["']([^"']+)["']`)
	mavenJava     = regexp.MustCompile(`<(?:maven\.compiler\.release|maven\.compiler\.source|java\.version|release)>\s*([0-9][0-9.]*)\s*</`)
	gradleJava    = regexp.MustCompile(`JavaLanguageVersion\.of\(\s*([0-9]+)\s*\)|JavaVersion\.VERSION_([0-9]+(?:_[0-9]+)?)|sourceCompatibility\s*=\s*['"]?([0-9][0-9.]*)`)
	toolVersions  = map[string][2]string{
		"nodejs": {"lang.node", "node"}, "node": {"lang.node", "node"},
		"python": {"lang.python", "python"}, "golang": {"lang.go", "go"}, "go": {"lang.go", "go"},
		"ruby": {"lang.ruby", "ruby"}, "rust": {"lang.rust", "rust"},
	}
)

// Requirements reads the versions a project in dir asks for.
func Requirements(dir string) []Requirement {
	var out []Requirement
	add := func(lang, name, rule, src, note string) {
		rule = strings.TrimSpace(rule)
		if rule != "" {
			out = append(out, Requirement{lang, name, rule, src, note})
		}
	}
	at := func(f string) string { return filepath.Join(dir, f) }

	if s, ok := readSmall(at("go.mod")); ok {
		goNote := "Go may download that toolchain on its own (GOTOOLCHAIN=auto), which needs network access and time; or the build fails with GOTOOLCHAIN=local."
		if m := toolchainLine.FindStringSubmatch(s); m != nil {
			add("lang.go", "go", ">="+m[1], "go.mod (toolchain)", goNote)
		} else if m := goLine.FindStringSubmatch(s); m != nil {
			add("lang.go", "go", ">="+m[1], "go.mod (go)", goNote)
		}
	}
	for _, f := range []string{".nvmrc", ".node-version"} {
		if s, ok := readSmall(at(f)); ok {
			add("lang.node", "node", strings.TrimPrefix(firstWord(s), "v"), f, "")
			break
		}
	}
	if s, ok := readSmall(at("package.json")); ok {
		var pj struct {
			Engines map[string]string `json:"engines"`
			Volta   map[string]string `json:"volta"`
		}
		if json.Unmarshal([]byte(s), &pj) == nil {
			if pj.Engines["node"] != "" {
				add("lang.node", "node", pj.Engines["node"], "package.json (engines.node)", "")
			}
			if pj.Volta["node"] != "" {
				add("lang.node", "node", pj.Volta["node"], "package.json (volta.node)", "")
			}
		}
	}
	if s, ok := readSmall(at(".python-version")); ok {
		add("lang.python", "python", firstWord(s), ".python-version", "")
	} else if s, ok := readSmall(at("pyproject.toml")); ok {
		if m := requiresPy.FindStringSubmatch(s); m != nil {
			add("lang.python", "python", m[1], "pyproject.toml (requires-python)", "")
		}
	}
	if s, ok := readSmall(at("rust-toolchain.toml")); ok {
		if m := rustChannel.FindStringSubmatch(s); m != nil {
			add("lang.rust", "rust", m[1], "rust-toolchain.toml (channel)", "")
		}
	} else if s, ok := readSmall(at("rust-toolchain")); ok {
		add("lang.rust", "rust", firstWord(s), "rust-toolchain", "")
	}
	if s, ok := readSmall(at("pom.xml")); ok {
		if m := mavenJava.FindStringSubmatch(s); m != nil {
			v := strings.TrimPrefix(m[1], "1.") // 1.8 means 8
			add("lang.java", "java", ">="+v, "pom.xml", "")
		}
	} else {
		for _, f := range []string{"build.gradle.kts", "build.gradle"} {
			if s, ok := readSmall(at(f)); ok {
				if m := gradleJava.FindStringSubmatch(s); m != nil {
					v := m[1] + m[2] + m[3]
					v = strings.TrimPrefix(strings.ReplaceAll(v, "_", "."), "1.")
					add("lang.java", "java", ">="+v, f, "")
				}
				break
			}
		}
	}
	if s, ok := readSmall(at(".ruby-version")); ok {
		add("lang.ruby", "ruby", strings.TrimPrefix(firstWord(s), "ruby-"), ".ruby-version", "")
	}
	if s, ok := readSmall(at(".tool-versions")); ok {
		sc := bufio.NewScanner(strings.NewReader(s))
		for sc.Scan() {
			fs := strings.Fields(strings.SplitN(sc.Text(), "#", 2)[0])
			if len(fs) >= 2 {
				if t, ok := toolVersions[fs[0]]; ok {
					add(t[0], t[1], fs[1], ".tool-versions", "")
				}
			}
		}
	}
	return out
}

var versionNum = regexp.MustCompile(`(\d+)\.(\d+)(?:\.(\d+))?`)

// Installed pulls the version number out of a language fact's value,
// e.g. "go: go version go1.24.7 linux/amd64" -> "1.24.7".
func Installed(value string) string {
	if i := strings.Index(value, ": "); i >= 0 {
		value = value[i+2:]
	}
	return versionNum.FindString(value)
}

type ver [3]int

// parse reads "1.24.7", "1.24", "20" or "20.x"; n is how many parts were given.
func parse(s string) (v ver, n int, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if s == "" {
		return v, 0, false
	}
	for i, p := range strings.SplitN(s, ".", 3) {
		if p == "x" || p == "X" || p == "*" {
			return v, i, i > 0
		}
		x, err := strconv.Atoi(p)
		if err != nil {
			return v, 0, false
		}
		v[i], n = x, i+1
	}
	return v, n, true
}

func cmp(a, b ver) int {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// prefix reports whether have starts with the first n parts of want.
func prefix(have, want ver, n int) bool {
	for i := 0; i < n; i++ {
		if have[i] != want[i] {
			return false
		}
	}
	return true
}

var comparator = regexp.MustCompile(`^(>=|<=|>|<|==|=|!=|~=|\^|~)?\s*v?([0-9][0-9.xX*]*)$`)

// Satisfies reports whether version have meets rule. ok is false when the
// rule is not one it understands (an alias like "lts/*" or "stable", or a
// hyphen range), so the caller can say "unknown" instead of guessing.
func Satisfies(have, rule string) (match, ok bool) {
	h, _, hok := parse(have)
	if !hok {
		return false, false
	}
	rule = strings.TrimSpace(rule)
	if rule == "" || rule == "*" || strings.Contains(rule, " - ") {
		return false, rule == "*"
	}
	for _, alt := range strings.Split(rule, "||") {
		all, understood := true, true
		for _, c := range strings.FieldsFunc(alt, func(r rune) bool { return r == ',' || r == ' ' }) {
			m := comparator.FindStringSubmatch(c)
			if m == nil {
				understood = false
				break
			}
			w, n, wok := parse(m[2])
			if !wok {
				understood = false
				break
			}
			var good bool
			switch m[1] {
			case ">=":
				good = cmp(h, w) >= 0
			case ">":
				good = cmp(h, w) > 0 && !prefix(h, w, n)
			case "<=":
				good = cmp(h, w) <= 0 || prefix(h, w, n)
			case "<":
				good = cmp(h, w) < 0 && !prefix(h, w, n)
			case "!=":
				good = !prefix(h, w, n)
			case "^":
				hi := ver{w[0] + 1}
				if w[0] == 0 {
					hi = ver{0, w[1] + 1}
				}
				good = cmp(h, w) >= 0 && cmp(h, hi) < 0
			case "~":
				hi := ver{w[0], w[1] + 1}
				if n == 1 {
					hi = ver{w[0] + 1}
				}
				good = cmp(h, w) >= 0 && cmp(h, hi) < 0
			case "~=": // PEP 440: ~=3.11 means >=3.11, ==3.*
				good = cmp(h, w) >= 0 && prefix(h, w, max(n-1, 1))
			default: // "1.27", "=20", "==3.12": the parts given must match
				good = prefix(h, w, n)
			}
			if !good {
				all = false
			}
		}
		if understood && all {
			return true, true
		}
		if !understood {
			return false, false
		}
	}
	return false, true
}

// VersionFacts compares what the project in dir asks for with the language
// facts already probed. A mismatch is reported as missing ("ver." keys).
func VersionFacts(dir string, have []store.Fact) []store.Fact {
	var out []store.Fact
	for _, r := range Requirements(dir) {
		key := "ver." + r.Name + " " + r.Source
		f := store.Fact{Key: key, How: "read " + r.Source}
		var lang *store.Fact
		for i := range have {
			if have[i].Key == r.Lang {
				lang = &have[i]
			}
		}
		switch {
		case lang == nil || lang.Status == store.Unknown:
			f.Status, f.Why = store.Unknown, fmt.Sprintf("%s %s asked by %s; installed version unknown", r.Name, r.Rule, r.Source)
		case lang.Status == store.Missing:
			f.Status, f.Value = store.Missing, fmt.Sprintf("%s %s asked by %s, but %s is not installed", r.Name, r.Rule, r.Source, r.Name)
		default:
			v := Installed(lang.Value)
			match, ok := Satisfies(v, r.Rule)
			switch {
			case !ok:
				f.Status, f.Why = store.Unknown, fmt.Sprintf("%s %q asked by %s; could not compare with installed %s", r.Name, r.Rule, r.Source, nonEmptyStr(v, "?"))
			case match:
				f.Status, f.Value = store.OK, fmt.Sprintf("%s %s asked by %s; %s installed", r.Name, r.Rule, r.Source, v)
			default:
				f.Status, f.Value = store.Missing, fmt.Sprintf("%s %s asked by %s, but %s is installed", r.Name, r.Rule, r.Source, v)
				if r.Note != "" {
					f.Why = r.Note
				}
			}
		}
		out = append(out, f)
	}
	return out
}

func nonEmptyStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Package gate decides whether an agent's tool call may go ahead.
package gate

import (
	"regexp"
	"strings"
)

var builtins = map[string]bool{
	"cd": true, "echo": true, "printf": true, "export": true, "unset": true, "set": true, "source": true,
	".": true, "alias": true, "unalias": true, "read": true, "test": true, "[": true, "[[": true, "]]": true, "]": true,
	"true": true, "false": true, "exit": true, "return": true, "shift": true, "local": true, "declare": true,
	"typeset": true, "eval": true, "trap": true, "wait": true, "jobs": true, "fg": true, "bg": true, "kill": true,
	"pwd": true, "pushd": true, "popd": true, "dirs": true, "umask": true, "ulimit": true, "type": true,
	"hash": true, "help": true, "let": true, "history": true, "shopt": true, "getopts": true, "times": true,
	"break": true, "continue": true, ":": true, "readonly": true, "logout": true, "disown": true, "suspend": true,
	"compgen": true, "complete": true, "bind": true, "caller": true, "enable": true, "mapfile": true, "readarray": true,
	"setopt": true, "unsetopt": true, "autoload": true, "zmodload": true, "noglob": true, "rehash": true, "whence": true,
	"where": true, "which": true, "print": true,
}

// keywords that are followed by a command in the same segment.
var leading = map[string]bool{"if": true, "then": true, "else": true, "elif": true, "do": true, "while": true, "until": true, "!": true, "{": true, "time": true}

// keywords that start structures whose words are not commands.
var skipSegment = map[string]bool{"for": true, "case": true, "select": true, "function": true, "fi": true, "done": true, "esac": true, "}": true, "in": true}

// wrappers run the next word as the command.
var wrappers = map[string]bool{"sudo": true, "env": true, "nice": true, "nohup": true, "exec": true, "command": true, "builtin": true, "xargs": true, "doas": true, "stdbuf": true, "timeout": true, "caffeinate": true}

// optTakesValue lists wrapper options whose value is the next word.
var optTakesValue = map[string]bool{
	"sudo -u": true, "sudo -g": true, "sudo -C": true, "sudo -h": true, "sudo -p": true, "sudo -D": true, "sudo -R": true, "sudo -U": true,
	"doas -u": true, "doas -C": true,
	"timeout -s": true, "timeout -k": true, "timeout --signal": true, "timeout --kill-after": true,
	"nice -n": true, "env -u": true, "env -C": true, "env -S": true,
	"xargs -I": true, "xargs -n": true, "xargs -P": true, "xargs -L": true, "xargs -s": true, "xargs -d": true, "xargs -E": true, "xargs -a": true,
	"stdbuf -i": true, "stdbuf -o": true, "stdbuf -e": true,
}

var assign = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?\+?=`)
var bareName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*$`)

// segments splits a shell command line into simple-command word lists.
// It is a heuristic, not a full shell parser: it understands quotes, escapes,
// comments, operators, $(...), backticks and here-documents well enough to find
// the first word of each simple command.
func segments(s string) [][]string {
	var segs [][]string
	var words []string
	var cur strings.Builder
	inWord := false
	flushWord := func() {
		if inWord {
			words = append(words, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	flushSeg := func() {
		flushWord()
		if len(words) > 0 {
			segs = append(segs, words)
		}
		words = nil
	}
	var heredocs []string // pending here-doc delimiters (with strip flag encoded as leading "-")
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		switch {
		case c == '\\' && i+1 < len(rs):
			if rs[i+1] == '\n' {
				i++
				continue
			}
			cur.WriteRune(rs[i+1])
			inWord = true
			i++
		case c == '\'':
			j := i + 1
			for j < len(rs) && rs[j] != '\'' {
				j++
			}
			cur.WriteString(string(rs[i+1 : min(j, len(rs))]))
			inWord = true
			i = j
		case c == '"':
			j := i + 1
			for j < len(rs) && rs[j] != '"' {
				if rs[j] == '\\' {
					j++
				}
				j++
			}
			cur.WriteString(string(rs[i+1 : min(j, len(rs))]))
			inWord = true
			i = j
		case c == '(' && inWord && strings.HasSuffix(cur.String(), "=") && assign.MatchString(cur.String()):
			// array assignment: name=(a b c) is data, not commands
			i = skipParens(rs, i)
			cur.WriteString("()")
		case c == '$' && i+2 < len(rs) && rs[i+1] == '(' && rs[i+2] == '(':
			// arithmetic $(( ... )) is not a command
			i = skipParens(rs, i+1)
			cur.WriteString("0")
			inWord = true
		case c == '#' && !inWord:
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
			i--
		case c == '\n':
			flushSeg()
			if len(heredocs) > 0 {
				i = skipHeredocs(rs, i+1, heredocs) - 1
				heredocs = nil
			}
		case c == ';' || c == '&' || c == '|' || c == '(' || c == ')' || c == '`':
			flushSeg()
		case c == '$' && i+1 < len(rs) && rs[i+1] == '(':
			flushSeg()
			i++
		case c == '<' && i+1 < len(rs) && rs[i+1] == '<' && !(i+2 < len(rs) && rs[i+2] == '<'):
			flushWord()
			j := i + 2
			strip := false
			if j < len(rs) && rs[j] == '-' {
				strip = true
				j++
			}
			for j < len(rs) && (rs[j] == ' ' || rs[j] == '\t') {
				j++
			}
			k := j
			for k < len(rs) && !strings.ContainsRune(" \t\n;&|<>()", rs[k]) {
				k++
			}
			delim := strings.Trim(string(rs[j:k]), `'"\`)
			if delim != "" {
				if strip {
					delim = "-" + delim
				}
				heredocs = append(heredocs, delim)
			}
			i = k - 1
		case c == '<' || c == '>':
			// redirection: drop the operator, its target word, and a file descriptor number before it (3>&1)
			if inWord && isDigits(cur.String()) {
				cur.Reset()
				inWord = false
			}
			flushWord()
			j := i + 1
			for j < len(rs) && (rs[j] == '>' || rs[j] == '&' || rs[j] == '<') {
				j++
			}
			for j < len(rs) && (rs[j] == ' ' || rs[j] == '\t') {
				j++
			}
			for j < len(rs) && !strings.ContainsRune(" \t\n;&|<>()", rs[j]) {
				j++
			}
			i = j - 1
		case c == ' ' || c == '\t' || c == '\r':
			flushWord()
		default:
			cur.WriteRune(c)
			inWord = true
		}
	}
	flushSeg()
	return segs
}

// skipParens returns the index of the parenthesis that closes the one at start.
func skipParens(rs []rune, start int) int {
	depth := 0
	for i := start; i < len(rs); i++ {
		switch rs[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		case '\'', '"':
			q := rs[i]
			for i++; i < len(rs) && rs[i] != q; i++ {
			}
		}
	}
	return len(rs) - 1
}

// skipHeredocs returns the index just after the bodies of the pending here-documents.
func skipHeredocs(rs []rune, start int, delims []string) int {
	i := start
	for _, d := range delims {
		strip := strings.HasPrefix(d, "-")
		d = strings.TrimPrefix(d, "-")
		for i < len(rs) {
			j := i
			for j < len(rs) && rs[j] != '\n' {
				j++
			}
			line := string(rs[i:j])
			if strip {
				line = strings.TrimLeft(line, "\t")
			}
			i = j + 1
			if line == d {
				break
			}
		}
	}
	if i > len(rs) {
		i = len(rs)
	}
	return i
}

// Cmd is one simple command found in a line: its name and the words after it.
type Cmd struct {
	Name string
	Args []string
}

// Commands returns the simple commands of a shell line, in order. Paths,
// variables, builtins, numbers and functions defined in the same line are left out.
func Commands(line string) []Cmd {
	defined := map[string]bool{}
	for _, m := range funcDef.FindAllStringSubmatch(line, -1) {
		for _, g := range m[1:] {
			if g != "" {
				defined[g] = true
			}
		}
	}
	var out []Cmd
	for _, words := range segments(line) {
		name, idx := firstCommand(words)
		if name == "" || defined[name] {
			continue
		}
		out = append(out, Cmd{Name: name, Args: words[idx+1:]})
	}
	return out
}

var funcDef = regexp.MustCompile(`(?:^|[;&|{(\s])([A-Za-z_][A-Za-z0-9_.-]*)\s*\(\s*\)|\bfunction\s+([A-Za-z_][A-Za-z0-9_.-]*)`)

// CommandNames returns the bare command names a shell line would run, in order,
// without duplicates.
func CommandNames(line string) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range Commands(line) {
		if seen[c.Name] {
			continue
		}
		seen[c.Name] = true
		out = append(out, c.Name)
	}
	return out
}

func firstCommand(words []string) (string, int) {
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		case assign.MatchString(w):
			continue
		case leading[w]:
			continue
		case w == "function":
			// "function name {" : the body starts after the brace
			j := i + 1
			for j < len(words) && words[j] != "{" {
				j++
			}
			if j >= len(words) {
				return "", 0
			}
			i = j
			continue
		case skipSegment[w]:
			return "", 0
		case w == "command" && i+1 < len(words) && (words[i+1] == "-v" || words[i+1] == "-V"):
			// "command -v x" checks whether x exists; it does not run it
			return "", 0
		case wrappers[w]:
			// skip the wrapper's options and assignments, then take the next word
			for i+1 < len(words) {
				next := words[i+1]
				switch {
				case strings.HasPrefix(next, "-"):
					i++
					// options that take a separate value: sudo -u root, timeout -s KILL, nice -n 10, xargs -I {}
					if optTakesValue[w+" "+next] && i+1 < len(words) {
						i++
					}
					continue
				case assign.MatchString(next), w == "timeout" && isNumber(next):
					i++
					continue
				}
				break
			}
			continue
		}
		if builtins[w] || !bareName.MatchString(w) || isDigits(w) {
			return "", 0
		}
		return w, i
	}
	return "", 0
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isNumber(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r == '.' || r == 's' || r == 'm' || r == 'h') {
			return false
		}
	}
	return true
}

var packageManagers = map[string]bool{
	"pip": true, "pip3": true, "uv": true, "pipx": true, "poetry": true, "npm": true, "pnpm": true, "yarn": true,
	"bun": true, "deno": true, "cargo": true, "go": true, "gem": true, "brew": true, "apt": true, "apt-get": true,
	"dnf": true, "yum": true, "pacman": true, "apk": true, "winget": true, "choco": true, "scoop": true, "port": true,
	"nix-env": true, "mise": true, "asdf": true, "conda": true, "mamba": true,
}

var installVerbs = map[string]bool{"install": true, "add": true, "i": true, "get": true, "sync": true}

// IsInstall reports whether one command installs software: a package manager
// whose first or second sub-command is an install verb (npm i, uv pip install,
// brew install), or pacman -S.
func IsInstall(c Cmd) bool {
	if !packageManagers[c.Name] {
		return false
	}
	seen := 0
	for _, a := range c.Args {
		if c.Name == "pacman" && strings.HasPrefix(a, "-S") {
			return true
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		if installVerbs[a] {
			return true
		}
		seen++
		if seen == 2 {
			break
		}
	}
	return false
}

// InstallsSomething reports whether any command in the line installs software.
func InstallsSomething(line string) bool {
	for _, c := range Commands(line) {
		if IsInstall(c) {
			return true
		}
	}
	return false
}

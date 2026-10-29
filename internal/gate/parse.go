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
			// redirection: drop the operator and its target word
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

// CommandNames returns the bare command names a shell command line would run,
// in order and without duplicates. Paths, variables and builtins are left out.
func CommandNames(line string) []string {
	var out []string
	seen := map[string]bool{}
	for _, words := range segments(line) {
		name := firstCommand(words)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func firstCommand(words []string) string {
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		case assign.MatchString(w):
			continue
		case leading[w]:
			continue
		case skipSegment[w]:
			return ""
		case wrappers[w]:
			// skip the wrapper's options and assignments, then take the next word
			for i+1 < len(words) && (strings.HasPrefix(words[i+1], "-") || assign.MatchString(words[i+1]) || isNumber(words[i+1]) && w == "timeout") {
				i++
			}
			continue
		}
		if builtins[w] || !bareName.MatchString(w) {
			return ""
		}
		return w
	}
	return ""
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

var installPattern = regexp.MustCompile(`(?i)\b(pip3?|uv|pipx|npm|pnpm|yarn|bun|cargo|go|gem|brew|apt|apt-get|dnf|yum|pacman|apk|winget|choco|scoop|port|nix-env|mise|asdf|conda|mamba)\b[^;&|\n]*\b(install|add|i|get|tool install|sync)\b`)

// InstallsSomething reports whether the command line looks like it installs software.
func InstallsSomething(line string) bool {
	return installPattern.MatchString(line)
}

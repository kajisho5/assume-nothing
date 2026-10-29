package gate

import (
	"github.com/kajisho5/assume-nothing/internal/probe"
	"github.com/kajisho5/assume-nothing/internal/store"
)

// Self is the program's own command name; calls to it are never blocked.
const Self = "assume-nothing"

// Lookup checks one command name. It is a variable so tests can replace it.
var Lookup = probe.Command

// MissingCommands returns the command names in line that are not installed and
// have not been insisted on before. It updates st with what it learned.
//
// The rule is "stop once, then trust": a name is blocked the first time it is
// not found. If the agent runs it again (it may be an alias, a shell function or
// on a PATH the hook cannot see), the second attempt goes through.
func MissingCommands(line string, st *store.State) []string {
	if InstallsSomething(line) {
		return nil
	}
	var missing []string
	for _, name := range CommandNames(line) {
		if name == Self {
			continue
		}
		switch st.Commands[name] {
		case store.OK, "allowed":
			continue
		}
		f := Lookup(name)
		if f.Status == store.OK {
			st.Commands[name] = store.OK
			continue
		}
		if st.Denied[name] >= 1 {
			st.Commands[name] = "allowed"
			continue
		}
		st.Denied[name]++
		missing = append(missing, name)
	}
	return missing
}

// Forget clears what is known about command names, so they are checked again.
func Forget(st *store.State) {
	for k, v := range st.Commands {
		if v == store.OK {
			continue
		}
		delete(st.Commands, k)
	}
	st.Denied = map[string]int{}
}

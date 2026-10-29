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
//
// Commands after an install in the same line are not checked: the install may
// be what provides them.
func MissingCommands(line string, st *store.State) []string {
	var missing []string
	seen := map[string]bool{}
	for _, c := range Commands(line) {
		name := c.Name
		if !seen[name] && name != Self {
			seen[name] = true
			if !known(name, st) {
				missing = append(missing, name)
			}
		}
		if IsInstall(c) {
			break
		}
	}
	return missing
}

// known reports whether name may run, recording what it learns.
func known(name string, st *store.State) bool {
	switch st.Commands[name] {
	case store.OK, "allowed":
		return true
	}
	if Lookup(name).Status == store.OK {
		st.Commands[name] = store.OK
		return true
	}
	if st.Denied[name] >= 1 {
		st.Commands[name] = "allowed"
		return true
	}
	st.Denied[name]++
	return false
}

// Forget clears what was learned about missing commands after an install, so
// they are looked up again. Names the agent already insisted on stay allowed.
func Forget(st *store.State) {
	st.Denied = map[string]int{}
}

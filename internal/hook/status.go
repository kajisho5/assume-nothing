package hook

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kajisho5/assume-nothing/internal/store"
)

// Status describes what assume-nothing did in a session: the probe, the
// commands it stopped, the names it tracked, and the answers it sent back.
// An empty session id means the most recent one.
func Status(session string) (string, error) {
	if session == "" {
		s, err := store.Latest()
		if err != nil {
			return "", fmt.Errorf("no session has been probed yet (%w)", err)
		}
		session = s
	}
	l := lang(session)
	var b strings.Builder
	fmt.Fprintf(&b, tr(l, "[assume-nothing: 状況、会話 %s]\n", "[assume-nothing: status, session %s]\n"), session)
	st, err := store.Load(session)
	if err != nil {
		fmt.Fprintf(&b, tr(l, "下調べ: なし（%v）\n", "probe: none (%v)\n"), err)
	} else {
		fmt.Fprintf(&b, tr(l, "下調べ: %s に %s で実施\n", "probe: taken %s in %s\n"), st.TakenAt.Format("2006-01-02 15:04:05"), st.Cwd)
		var mismatch, missing []string
		for _, f := range st.Facts {
			if f.Status != store.Missing {
				continue
			}
			if strings.HasPrefix(f.Key, "ver.") {
				mismatch = append(mismatch, f.Value)
			} else if strings.HasPrefix(f.Key, "lang.") || strings.HasPrefix(f.Key, "tool.") {
				missing = append(missing, f.Key[strings.IndexByte(f.Key, '.')+1:])
			}
		}
		fmt.Fprintf(&b, tr(l, "入っていないもの: %s\n", "not installed: %s\n"), orNone(missing, l))
		fmt.Fprintf(&b, tr(l, "版の食い違い: %s\n", "version mismatches: %s\n"), orNone(mismatch, l))
		var stopped []string
		for name, n := range st.Denied {
			stopped = append(stopped, fmt.Sprintf(tr(l, "%s（%d回）", "%s (%dx)"), name, n))
		}
		sort.Strings(stopped)
		fmt.Fprintf(&b, tr(l, "入っていないとして止めたコマンド: %s\n", "commands stopped as not installed: %s\n"), orNone(stopped, l))
	}
	lg, ok := targetLog(session)
	if !ok {
		return b.String(), nil
	}
	ts, err := lg.Read()
	if err != nil {
		return b.String(), nil
	}
	fmt.Fprintf(&b, tr(l, "依頼の数: %d\n", "requests: %d\n"), ts.Prompts)
	if len(ts.Targets) == 0 {
		b.WriteString(tr(l, "確かめた名前: なし\n", "names tracked: none\n"))
	} else {
		b.WriteString(tr(l, "確かめた名前:\n", "names tracked:\n"))
		for _, t := range ts.Targets {
			state := tr(l, "調べていない", "NOT looked up")
			if how, ok := ts.Seen[t.Key]; ok {
				state = tr(l, "調べた（", "looked up (") + how + tr(l, "）", ")")
			}
			fmt.Fprintf(&b, "- %s: %s\n", Clean(t.Text), state)
		}
	}
	var sent []string
	for k := range ts.Warned {
		sent = append(sent, k)
	}
	sort.Strings(sent)
	fmt.Fprintf(&b, tr(l, "差し戻した回数: %d %s\n", "answers sent back: %d %s\n"), len(sent), strings.TrimSpace(strings.Join(sent, " ")))
	return b.String(), nil
}

func orNone(xs []string, l string) string {
	if len(xs) == 0 {
		return tr(l, "なし", "none")
	}
	return strings.Join(xs, "; ")
}

package target

import (
	"path/filepath"
	"strings"
	"testing"
)

func texts(ts []Target) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Text)
	}
	return out
}

func TestExtract(t *testing.T) {
	cases := []struct {
		prompt string
		want   []string
	}{
		{"たとえばlumen-kitについて調べて", []string{"lumen-kit"}},
		{"Zephyrdocとは？", []string{"Zephyrdoc"}},
		{"orbit-labs/quasar-cli の README を読んで", []string{"orbit-labs/quasar-cli"}},
		{"see https://example.com/docs/quasar-cli.", []string{"https://example.com/docs/quasar-cli"}},
		{"research Quasarbox and tell me", []string{"Quasarbox"}},
		{"run `nebula-build` please", []string{"nebula-build"}},
		{"「Moonpad」の料金", []string{"Moonpad"}},
		// not targets: versions, e.g., plain words, emails, pasted code
		{"update to v1.2.3, e.g. tomorrow", nil},
		{"mail me at someone@example.invalid", nil},
		{"fix this:\n```\nimport foo-bar\n```", nil},
		{"I/O and and/or are fine", nil},
		{"make it faster", nil},
		{"<pasted_content id=\"1\">\nnebula-core crashed\n</pasted_content id=\"1\">\nwhy?", nil},
	}
	for _, c := range cases {
		got := texts(Extract(c.prompt))
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("Extract(%q) = %q, want %q", c.prompt, got, c.want)
		}
	}
}

func TestExtractCaps(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString(" tool-")
		b.WriteByte(byte('a' + i))
	}
	if n := len(Extract(b.String())); n != MaxPerPrompt {
		t.Fatalf("got %d targets, want %d", n, MaxPerPrompt)
	}
}

func TestMentions(t *testing.T) {
	tg := Target{Text: "lumen-kit", Key: Key("lumen-kit")}
	for _, s := range []string{"lumen-kit is a library", "Lumen Kit has 3 modes", "the lumen_kit repo", "LUMENKIT"} {
		if !Mentions(s, tg) {
			t.Errorf("should mention: %q", s)
		}
	}
	short := Target{Text: "bun", Key: "bun"}
	if Mentions("a bundle of files", short) {
		t.Error("bun found inside bundle")
	}
	if !Mentions("use Bun 1.2", short) {
		t.Error("Bun not found")
	}
}

func TestLogFlow(t *testing.T) {
	lg := Log{Path: filepath.Join(t.TempDir(), "s", "x.targets.log")}
	st, err := lg.Read()
	if err != nil || len(st.Targets) != 0 {
		t.Fatalf("empty log: %v %v", st, err)
	}
	ts := Extract("compare lumen-kit and orbit-labs/quasar-cli")
	if err := lg.AddTargets(ts); err != nil {
		t.Fatal(err)
	}
	lg.AddTargets(ts) // duplicates are ignored on read
	st, _ = lg.Read()
	if len(st.Targets) != 2 || len(st.Unseen()) != 2 {
		t.Fatalf("targets = %v", st.Targets)
	}
	if k := st.Touched([]byte(`{"query":"lumen kit github"}`), []byte(`"no results"`)); len(k) != 0 {
		t.Fatalf("a failed search counted: %v", k)
	}
	keys := st.Touched([]byte(`{"query":"lumen kit github"}`), []byte(`"1. Lumen Kit: a fictional widget toolkit for demos. Version 3.2 adds themes."`))
	if len(keys) != 1 || keys[0] != "lumenkit" {
		t.Fatalf("touched = %v", keys)
	}
	lg.MarkSeen(keys, "WebSearch")
	lg.MarkWarned([]string{"orbitlabsquasarcli"})
	st, _ = lg.Read()
	if u := st.Unseen(); len(u) != 1 || u[0].Text != "orbit-labs/quasar-cli" {
		t.Fatalf("unseen = %v", u)
	}
	if !st.Warned["orbitlabsquasarcli"] || st.Seen["lumenkit"] != "WebSearch" {
		t.Fatalf("state = %+v", st)
	}
}

func TestTouchedSeesEscapedJSON(t *testing.T) {
	st := &State{Targets: []Target{{Text: "lumen-kit", Key: "lumenkit"}}, Seen: map[string]string{}}
	if k := st.Touched([]byte(`{"url":"https:\/\/example.com\/lumen\u002dkit"}`), []byte(`"A fictional page with enough text to count as an answer."`)); len(k) != 1 {
		t.Fatalf("escaped JSON not matched: %v", k)
	}
}

func TestOwnerRepoWithoutPunctuation(t *testing.T) {
	if got := texts(Extract("orbitlabs/quasar の最新版")); strings.Join(got, "|") != "orbitlabs/quasar" {
		t.Errorf("got %q", got)
	}
	if got := Extract("client/server or yes/no"); len(got) != 0 {
		t.Errorf("common pairs taken as repos: %v", got)
	}
}

func TestLatestPrompt(t *testing.T) {
	lg := Log{Path: filepath.Join(t.TempDir(), "l.log")}
	lg.AddTargets(Extract("what is lumen-kit"))
	lg.AddTargets(nil) // a request that names nothing
	st, _ := lg.Read()
	if len(st.Latest) != 0 || len(st.Targets) != 1 {
		t.Fatalf("latest = %v, targets = %v", st.Latest, st.Targets)
	}
	lg.AddTargets(Extract("and lumen-kit again"))
	st, _ = lg.Read()
	if len(st.Latest) != 1 || st.Latest[0] != "lumenkit" {
		t.Fatalf("latest = %v", st.Latest)
	}
}

func TestFailedOutputs(t *testing.T) {
	for _, s := range []string{
		``, `"ok"`,
		`{"stdout":"curl: (22) The requested URL returned error: 403","stderr":""}`,
		`Request failed with status code 404 for https://example.com/lumen-kit`,
		`{"message":"API rate limit exceeded for 203.0.113.9","documentation_url":"https://docs.example.com"}`,
		`HTTP 429: too many requests, try again later please`,
	} {
		if !Failed([]byte(s)) {
			t.Errorf("not seen as failed: %q", s)
		}
	}
	for _, s := range []string{
		`"Lumen Kit 3.2 released. It has 500 stars and supports themes and plugins."`,
		`{"stdout":"lumen-kit 3.2.0  A fictional widget toolkit for demonstration purposes","stderr":""}`,
		strings.Repeat("A long page that mentions error 404 once in a table of status codes. ", 80),
	} {
		if Failed([]byte(s)) {
			t.Errorf("seen as failed: %q", s[:40])
		}
	}
}

func TestHedgedAbout(t *testing.T) {
	tg := Target{Text: "lumen-kit", Key: "lumenkit"}
	if !HedgedAbout("lumen-kit は未確認ですが、テーマ機能があるはずです。", tg) {
		t.Error("hedge in the same sentence not seen")
	}
	if !HedgedAbout("lumen-kit has themes. I have not verified this.", tg) {
		t.Error("hedge in the next sentence not seen")
	}
	long := "lumen-kit 3.2 has themes. It ships 40 widgets. It runs on Node 22. It is MIT licensed. The cache layer is unverified."
	if HedgedAbout(long, tg) {
		t.Error("a hedge far from the name covered it")
	}
}

func TestFreshAndKatakana(t *testing.T) {
	for _, p := range []string{"Lumenの最新版は？", "what's the latest release", "料金を教えて", "how much does it cost"} {
		if !Fresh(p) {
			t.Errorf("not fresh: %q", p)
		}
	}
	for _, p := range []string{"fix the build", "リリースして", "make it faster"} {
		if Fresh(p) {
			t.Errorf("fresh: %q", p)
		}
	}
	got := texts(Extract("ルミナスエディタについて調べて"))
	if strings.Join(got, "|") != "ルミナスエディタ" {
		t.Errorf("katakana name: %q", got)
	}
	tg := Extract("「ルミナスエディタ」の料金")[0]
	if !Mentions("ルミナスエディタは月額制です", tg) {
		t.Error("katakana mention not found")
	}
}

func TestGenericKatakanaIsNotAName(t *testing.T) {
	if got := Extract("このライブラリの最新版とツールについて"); len(got) != 0 {
		t.Errorf("generic nouns taken as names: %v", texts(got))
	}
}

func TestMultilingual(t *testing.T) {
	cases := []struct {
		prompt string
		want   string
	}{
		{"秀丸エディタについて調べて", "秀丸エディタ"},
		{"「一太郎」の最新版は？", "一太郎"},
		{"架空のツール「星雲帳」とは", "星雲帳"},
		{"星雲帳を調べて", ""}, // kanji only: quote it to make it a name
		{"原因を調べて", ""},
		{"このエラーの原因を調べて", ""},
		{"ログを調べて", ""},
		{"ルミナス帳を調べて", "ルミナス帳"},
		{"天気について教えて", ""},
		{"料金について教えて", ""},
		{"关于「星云笔记」的最新版本", "星云笔记"},
		{"星云笔记是什么", "星云笔记"},
		{"「별빛노트」에 대해 알려줘", "별빛노트"},
		{"별빛노트의 최신 버전은?", "별빛노트"},
		{"¿Qué es «Nebulina»?", "Nebulina"},
		{"Qu'est-ce que « Nebulina » ?", "Nebulina"},
	}
	for _, c := range cases {
		got := strings.Join(texts(Extract(c.prompt)), "|")
		if got != c.want {
			t.Errorf("Extract(%q) = %q, want %q", c.prompt, got, c.want)
		}
	}
	for _, p := range []string{"星云笔记的价格", "별빛노트의 최신 버전은?", "¿Cuál es el precio?", "Quel est le prix ?"} {
		if !Fresh(p) {
			t.Errorf("not fresh: %q", p)
		}
	}
	for _, h := range []string{"星雲帳については確かな情報がないため断言できません", "未验证", "我不确定", "확인하지 않았습니다", "잘 모르겠습니다", "no lo he verificado", "je ne sais pas"} {
		if !Hedged(h) {
			t.Errorf("not a hedge: %q", h)
		}
	}
	tg := Extract("「星雲帳」とは")[0]
	if !Mentions("星雲帳は架空のノートアプリです。", tg) {
		t.Error("kanji mention not found")
	}
	ko := Extract("「별빛노트」에 대해")[0]
	if !Mentions("별빛노트는 메모 앱입니다", ko) {
		t.Error("hangul mention not found")
	}
}

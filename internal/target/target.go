// Package target keeps track of the things a request is about (a repository, a
// library, a URL, a file) and whether anything in the session actually looked
// at them.
//
// The environment probe answers "what is on this machine". This package answers
// the other half of "do not assume": an agent that writes about ffmpeg-skill
// should have read something about ffmpeg-skill first. It cannot tell whether
// the answer is correct. It can only tell whether a source was looked at.
package target

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Target is one thing named in a request.
type Target struct {
	Text string `json:"text"` // as written by the user, shortened
	Key  string `json:"key"`  // lowercase letters and digits only, used for matching
}

// MaxPerPrompt caps how many targets one request can add.
const MaxPerPrompt = 8

var (
	pasted   = regexp.MustCompile(`(?s)<pasted_content[^>]*>.*?</pasted_content[^>]*>`)
	fence    = regexp.MustCompile("(?s)```.*?```")
	urlRe    = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `）」』】、。]+`)
	email    = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+`)
	backtick = regexp.MustCompile("`([^`\\s]{2,80})`")
	// A quoted name in any script: 「一太郎」, "Nebulina", «Nebulina», 『별빛노트』.
	quoted    = regexp.MustCompile(`[「『"“«‹【]\s*([\p{L}\p{N}][\p{L}\p{N} ._/+・ー-]{0,40}?)\s*[」』"”»›】]`)
	ownerRepo = regexp.MustCompile(`(?:^|[^A-Za-z0-9._/-])([A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*)`)
	compound  = regexp.MustCompile(`(?:^|[^A-Za-z0-9_./-])([A-Za-z][A-Za-z0-9]*(?:[-_.][A-Za-z0-9]+)+)`)
	// An ASCII name right before a Japanese "about / what is / look up" phrase:
	// "ffmpegについて調べて", "Tauriとは", "Astroって何".
	jaAbout = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+#]{2,40}|[ァ-ヶー・]{3,30})\s*(?:について|とは|って何|ってなに|を調べ|をしらべ|を比較|の(?:最新|仕様|料金|価格|バージョン|使い方|機能))`)
	// English: "research Tauri", "look up Astro", "what is Bun".
	// A capitalized name may have up to four words ("Elon Musk", "Visual Studio Code").
	enAbout = regexp.MustCompile(`(?:^|\b)(?i:research|look\s+up|look\s+into|investigate|what\s+is|what's|who\s+is|who's|tell\s+me\s+about|about)\s+(?:the\s+)?([A-Z][A-Za-z0-9+#._-]*(?:\s+[A-Z][A-Za-z0-9+#._-]*){0,3})`)
	// A lowercase name only right after a research verb ("research zorvexmark").
	enResearch = regexp.MustCompile(`(?i:\b(?:research|look\s+up|look\s+into|investigate))\s+([a-z][a-z0-9+#._-]{2,40})`)
	version    = regexp.MustCompile(`^v?\d+(\.\d+)*$`)
	// A name of any script right before a request to look it up:
	// 「秀丸エディタについて調べて」「星雲帳を調べて」.
	jaResearch = regexp.MustCompile(`([\p{Han}\p{Katakana}ーA-Za-z0-9・]{2,30})(?:について|の(?:こと|情報|仕様|最新情報))?\s*(?:を)?\s*(?:調べ|しらべ|調査|リサーチ|検索)`)
	// A mixed name (kanji with katakana or Latin letters) before 「について」「とは」.
	jaMixed = regexp.MustCompile(`([\p{Han}\p{Katakana}ーA-Za-z0-9・]{2,30})\s*(?:について|とは|って何|ってなに|の(?:最新|仕様|料金|価格|バージョン|使い方|機能))`)
	// Chinese: 关于X / X是什么 / 查一下X / X的最新版本.
	zhAbout = regexp.MustCompile(`(?:关于|關於|查一下|查询|查詢|搜索)\s*([\p{Han}A-Za-z0-9·]{2,20}?)(?:的|是|，|。|？|\?|\s|$)|([\p{Han}A-Za-z0-9·]{2,20}?)(?:是什么|是什麼|的最新|的价格|的價格)`)
	// Korean: X에 대해 / X란 / X의 최신.
	koAbout = regexp.MustCompile(`([\p{Hangul}A-Za-z0-9]{2,20}?)(?:에 대해|에 대한|이란|란 무엇|의 최신|의 가격|의 요금)`)
)

// stop are words the patterns can catch that are not things to look up.
var stop = map[string]bool{
	"eg": true, "ie": true, "etc": true, "vs": true, "the": true, "this": true, "that": true,
	"it": true, "its": true, "a": true, "an": true, "andor": true, "inputoutput": true,
	"readwrite": true, "io": true, "yes": true, "no": true, "ok": true,
	// French and other words with hyphens that are not names
	"estce": true, "cestadire": true, "peutetre": true, "quelquun": true, "aujourdhui": true,
}

// common words that appear in pairs like "and/or" or "client/server";
// such a pair is not a repository name.
var common = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`and or not input output in out read write yes no on off true false
		he she him her his hers they them s he client server frontend backend front back end start stop
		open close get set put post up down left right before after old new this that all any some none
		day night am pm km h min sec mb gb kb tb unix linux mac windows win ios android web app apps`) {
		common[w] = true
	}
	// Generic katakana nouns are not names ("このライブラリの最新版").
	for _, w := range strings.Fields(`ライブラリ ライブラリー ツール アプリ アプリケーション ソフト ソフトウェア サイト ウェブサイト
		バージョン ファイル フォルダ フォルダー コマンド リポジトリ リポ プラグイン スキル サービス サーバー サーバ
		データ コード エラー プロジェクト フレームワーク パッケージ モデル システム ドキュメント ページ ブラウザ
		デバイス パソコン スマホ メール アカウント プラン オプション モジュール コンポーネント テンプレート
		デザイン レイアウト ボタン メニュー エディタ エディター ターミナル シェル スクリプト プログラム
		ログ バグ テスト ビルド デプロイ ブランチ コミット プルリク ネット ネットワーク キャッシュ メモリ ディスク
		パス ポート ユーザー ユーザ パスワード セキュリティ パフォーマンス エラーログ コンソール ウィンドウ
		画面 原因 設定 環境 動作 挙動 仕様 結果 内容 方法 理由 問題 状況 履歴 使い方`) {
		stop[Key(w)] = true
	}
}

// alnum reports whether r is part of a name: a letter or digit in any script.
func alnum(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// Key reduces s to lowercase letters and digits, so "ffmpeg-skill",
// "FFmpeg Skill" and "ffmpeg_skill" all match each other. Letters of any
// script are kept, so katakana names match too.
func Key(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if alnum(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// fresh matches a request about something that changes over time.
var fresh = regexp.MustCompile(`(?i)(最新|料金|価格|値段|相場|いくら|价格|價格|多少钱|定价|최신|가격|요금|\búltim[ao]\b|\bprecio\b|dernière version|\bprix\b|neueste|\bpreis\b|\blatest\b|\bnewest\b|\bpricing\b|\bprice of\b|\bhow much (?:does|is)\b|\bcurrent version\b|\bup[- ]to[- ]date\b)`)

// Fresh reports whether a request asks about something that changes over
// time (the latest version, a price), which memory cannot answer reliably.
func Fresh(prompt string) bool {
	text := pasted.ReplaceAllString(prompt, " ")
	return fresh.MatchString(fence.ReplaceAllString(text, " "))
}

// failure matches tool output that is an error rather than an answer.
var failure = regexp.MustCompile(`(?i)(https?[^\n]{0,40}\b(?:40[0-9]|429|5[0-9][0-9])\b|status(?:[ _]?code)?["':\s=]{0,4}(?:40[0-9]|429|5[0-9][0-9])\b|\b(?:40[0-9]|429|5[0-9][0-9])\s+(?:forbidden|not found|unauthorized|too many|internal server|bad gateway|service unavailable|gateway time)|\berror[:\s]+(?:40[0-9]|429|5[0-9][0-9])\b|forbidden|not found|rate limit|too many requests|access denied|failed to fetch|request failed|unable to fetch|could not (?:resolve|connect)|connection refused|timed out|permission denied|no results|enoent|no such file|is not reachable|blocked)`)

// Failed reports whether a tool's output is an error, an empty answer, or a
// refusal, so the call does not count as having looked anything up.
func Failed(output []byte) bool {
	out := strings.TrimSpace(unescape(clip(output)))
	if len(out) < 40 {
		return true
	}
	// A long page may mention "404" in passing; only short outputs are judged.
	return len(out) < 4000 && failure.MatchString(out)
}

func clip(b []byte) []byte {
	if len(b) > 256*1024 {
		return b[:256*1024]
	}
	return b
}

func urlKey(u string) string {
	u = strings.TrimRight(u, ".,;:!?)]}")
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	u = strings.TrimPrefix(u, "www.")
	return Key(u)
}

// Extract finds the targets named in a request. It is deliberately
// conservative: plain words are taken only next to an "about / look up" phrase.
func Extract(prompt string) []Target {
	text := pasted.ReplaceAllString(prompt, " ")
	text = fence.ReplaceAllString(text, " ")
	var out []Target
	seen := map[string]bool{}
	add := func(raw, key string) {
		raw = strings.TrimSpace(raw)
		if len(out) >= MaxPerPrompt || len(key) < 2 || seen[key] || stop[key] {
			return
		}
		if version.MatchString(raw) || !hasLetter(raw) {
			return
		}
		for _, t := range out {
			if strings.Contains(t.Key, key) {
				return // a piece of a name already taken ("kit" of "lumen-kit")
			}
		}
		seen[key] = true
		if len(raw) > 80 {
			raw = raw[:80]
		}
		out = append(out, Target{Text: raw, Key: key})
	}
	for _, u := range urlRe.FindAllString(text, -1) {
		add(strings.TrimRight(u, ".,;:!?)]}"), urlKey(u))
	}
	text = urlRe.ReplaceAllString(text, " ")
	text = email.ReplaceAllString(text, " ")
	for _, m := range backtick.FindAllStringSubmatch(text, -1) {
		add(m[1], Key(m[1]))
	}
	for _, m := range quoted.FindAllStringSubmatch(text, -1) {
		add(m[1], Key(m[1]))
	}
	for _, m := range ownerRepo.FindAllStringSubmatch(text, -1) {
		s := m[1]
		a, b, _ := strings.Cut(strings.ToLower(s), "/")
		if !(common[a] && common[b]) {
			add(s, Key(s))
		}
	}
	for _, m := range compound.FindAllStringSubmatch(text, -1) {
		add(m[1], Key(m[1]))
	}
	for _, m := range jaAbout.FindAllStringSubmatch(text, -1) {
		add(m[1], Key(m[1]))
	}
	for _, m := range jaResearch.FindAllStringSubmatch(text, -1) {
		// 「原因を調べて」「ログを調べて」 are ordinary words: a name written only
		// in kanji is taken when it is quoted, not here.
		if n := m[1]; hasNameScript(n) {
			add(n, Key(n))
		}
	}
	for _, m := range jaMixed.FindAllStringSubmatch(text, -1) {
		// Kanji alone is usually an ordinary word (天気, 料金); take it only
		// with katakana or Latin letters that are not a generic word.
		if n := m[1]; hasNameScript(n) {
			add(n, Key(n))
		}
	}
	for _, m := range zhAbout.FindAllStringSubmatch(text, -1) {
		n := m[1]
		if n == "" {
			n = m[2]
		}
		add(n, Key(n))
	}
	for _, m := range koAbout.FindAllStringSubmatch(text, -1) {
		add(m[1], Key(m[1]))
	}
	for _, re := range []*regexp.Regexp{enAbout, enResearch} {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			n := strings.TrimRight(m[1], ".,;:!?")
			add(n, Key(n))
		}
	}
	return out
}

// hasNameScript reports whether s has katakana or Latin letters beyond a
// generic word ("秀丸エディタ" yes because of 秀丸+エディタ, "天気" no).
func hasNameScript(s string) bool {
	hasKanji, hasOther := false, false
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r):
			hasKanji = true
		case unicode.Is(unicode.Katakana, r) || (r < 128 && unicode.IsLetter(r)):
			hasOther = true
		}
	}
	if !hasOther {
		return false
	}
	return hasKanji || !stop[Key(s)]
}

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// words lowercases s and splits it into runs of letters and digits.
func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !alnum(r) })
}

// Mentions reports whether text mentions the target as whole words, so "bun"
// is not found inside "bundle".
func Mentions(text string, t Target) bool {
	if t.Key == "" {
		return false
	}
	hay := " " + strings.Join(words(text), " ") + " "
	if tw := words(t.Text); len(tw) > 0 && strings.Contains(hay, " "+strings.Join(tw, " ")+" ") {
		return true
	}
	if strings.Contains(hay, " "+t.Key+" ") {
		return true
	}
	// Japanese has no spaces between words: a non-ASCII name is found as a substring.
	return !isASCII(t.Key) && strings.Contains(Key(text), t.Key)
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

var sentenceEnd = regexp.MustCompile(`[。．！？!?\n]+|[.](?:\s|$)`)

// HedgedAbout reports whether the answer says, next to a mention of t, that
// it is not verified. A hedge elsewhere in the answer does not cover t.
func HedgedAbout(text string, t Target) bool {
	parts := sentenceEnd.Split(text, -1)
	for i, p := range parts {
		if !Mentions(p, t) {
			continue
		}
		for j := i - 1; j <= i+1; j++ {
			if j >= 0 && j < len(parts) && Hedged(parts[j]) {
				return true
			}
		}
	}
	return false
}

// hedge matches an answer that already says it did not verify something.
var hedge = regexp.MustCompile(`(?i)(未確認|未検証|確かな情報が(?:ない|ありません)|情報がない|分からない|わからない|知らない|断言(?:でき|は)|確認(?:は|を)?していません|確認できていません|確認できません|検証(?:は|を)?していません|記憶に基づ|記憶で答え|分かりません|わかりません|知りません|情報がありません|情報を持っていません|not verified|unverified|from memory|未验证|未經驗證|未经验证|未核实|不确定|不確定|我不知道|没有核实|확인하지 않았|확인되지 않|검증하지 않|모르겠|sin verificar|no lo he verificado|no estoy seguro|non vérifié|pas vérifié|je ne sais pas|nicht überprüft|nicht verifiziert|weiß ich nicht|haven't verified|have not verified|could(?:n't| not) (?:verify|confirm|find)|i don't know|i do not know|don't have (?:any |reliable )?information|no reliable information)`)

// Hedged reports whether text already admits that something is not verified,
// so sending it back would only repeat that.
func Hedged(text string) bool { return hedge.MatchString(text) }

// Log is the per-session record of targets. It is an append-only file of JSON
// lines, so tool calls that run at the same time cannot overwrite each other.
type Log struct{ Path string }

type entry struct {
	Kind  string `json:"k"` // prompt | target | seen | lookup | warned
	Text  string `json:"text,omitempty"`
	Key   string `json:"key"`
	How   string `json:"how,omitempty"`
	Fresh bool   `json:"fresh,omitempty"`
}

// State is what the log says.
type State struct {
	Targets []Target
	Seen    map[string]string // key -> tool that looked at it
	Warned  map[string]bool
	Latest  []string // keys named in the most recent request
	Prompts int      // how many requests so far
	Fresh   bool     // the most recent request asks about something that changes
	Looked  bool     // a lookup succeeded since the most recent request
}

// FreshKey names the "needs a lookup" warning of the current request.
func (s *State) FreshKey() string { return "fresh#" + strconv.Itoa(s.Prompts) }

// Read loads the log. A missing file is an empty state.
func (l Log) Read() (*State, error) {
	st := &State{Seen: map[string]string{}, Warned: map[string]bool{}}
	f, err := os.Open(l.Path)
	if os.IsNotExist(err) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	have := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var e entry
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Key == "" {
			continue // a torn line; skip it
		}
		switch e.Kind {
		case "target":
			if !have[e.Key] {
				have[e.Key] = true
				st.Targets = append(st.Targets, Target{Text: e.Text, Key: e.Key})
			}
		case "seen":
			if _, ok := st.Seen[e.Key]; !ok {
				st.Seen[e.Key] = e.How
			}
		case "warned":
			st.Warned[e.Key] = true
		case "prompt":
			st.Latest = nil
			if e.Text != "" {
				st.Latest = strings.Split(e.Text, ",")
			}
			st.Prompts++
			st.Fresh = e.Fresh
			st.Looked = false
		case "lookup":
			st.Looked = true
		}
	}
	return st, sc.Err()
}

func (l Log) append(es []entry) error {
	if len(es) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o700); err != nil {
		return err
	}
	var b strings.Builder
	for _, e := range es {
		j, _ := json.Marshal(e)
		b.Write(j)
		b.WriteByte('\n')
	}
	f, err := os.OpenFile(l.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(b.String()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// AddTargets records a new request without a time-sensitive question.
func (l Log) AddTargets(ts []Target) error { return l.AddPrompt(ts, false) }

// AddPrompt records a new request: the targets it names (targets already
// known are listed again so the log knows what the latest request named), and
// whether it asks about something that changes over time.
func (l Log) AddPrompt(ts []Target, fresh bool) error {
	keys := make([]string, 0, len(ts))
	for _, t := range ts {
		keys = append(keys, t.Key)
	}
	es := []entry{{Kind: "prompt", Key: "p", Text: strings.Join(keys, ","), Fresh: fresh}}
	for _, t := range ts {
		es = append(es, entry{Kind: "target", Text: t.Text, Key: t.Key})
	}
	return l.append(es)
}

// MarkSeen records that a tool looked at these targets.
func (l Log) MarkSeen(keys []string, how string) error {
	var es []entry
	for _, k := range keys {
		es = append(es, entry{Kind: "seen", Key: k, How: how})
	}
	return l.append(es)
}

// MarkLookup records that a lookup (a web fetch or search, or a network
// command) succeeded.
func (l Log) MarkLookup(how string) error {
	return l.append([]entry{{Kind: "lookup", Key: "l", How: how}})
}

// MarkWarned records that the agent was already stopped once for these targets.
func (l Log) MarkWarned(keys []string) error {
	var es []entry
	for _, k := range keys {
		es = append(es, entry{Kind: "warned", Key: k})
	}
	return l.append(es)
}

// Unseen returns the targets no tool has looked at yet.
func (s *State) Unseen() []Target {
	var out []Target
	for _, t := range s.Targets {
		if _, ok := s.Seen[t.Key]; !ok {
			out = append(out, t)
		}
	}
	return out
}

// Touched returns the keys of unseen targets that a finished tool call looked
// at. A failed call (an error, an empty or refused answer) looks at nothing.
// Otherwise a target counts when the output mentions it, or when the input
// names it (a fetched URL, a read file, a search query) and the output is a
// real answer.
func (s *State) Touched(input, output []byte) []string {
	unseen := s.Unseen()
	if len(unseen) == 0 || Failed(output) {
		return nil
	}
	in := Key(unescape(clip(input)))
	out := Key(unescape(clip(output)))
	var keys []string
	for _, t := range unseen {
		if strings.Contains(out, t.Key) || strings.Contains(in, t.Key) {
			keys = append(keys, t.Key)
		}
	}
	return keys
}

// unescape turns a JSON value into plain text where it can, so "-" and
// friends do not hide a name.
func unescape(b []byte) string {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return string(b)
	}
	var sb strings.Builder
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case string:
			sb.WriteString(x)
			sb.WriteByte(' ')
		case []any:
			for _, e := range x {
				walk(e)
			}
		case map[string]any:
			for k, e := range x {
				sb.WriteString(k)
				sb.WriteByte(' ')
				walk(e)
			}
		}
	}
	walk(v)
	return sb.String()
}

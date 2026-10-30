# Changelog

## [0.13.0] - 2026-10-30

- Two layers, said plainly. The core (probe, command gate, version check) is exact and always on. The lookup check is a heuristic: `"lookup": "send-back"` (default), `"note"` (tell the agent what the request is about, never send back) or `"off"` in `.assume-nothing.json`, or `ASSUME_NOTHING_LOOKUP`.
- A cold machine no longer leaves languages UNKNOWN for the whole session: languages whose version check timed out at the start are checked again, once, with more time, on the first tool call. Seen in this project's own cloud session (Go, Node, Java, Ruby, PHP and Rust all timed out at 2 seconds).
- `"skip"` in `.assume-nothing.json` leaves languages, tools or package managers out of the probe.
- More of the machine: Kotlin, Dart, Zig and Elixir; cmake, gcc and clang. Version checks read `volta.node` in `package.json`, and Java from `pom.xml` and Gradle builds.
- A Japanese error page (「見つかりません」「アクセスが拒否」…) counts as a failed lookup.
- `scripts/setup.ps1`: install for Cursor, OpenCode or a repository from PowerShell, without Git Bash. Tested on Windows in CI.
- Docs: a demo of the lookup check (recorded run), Troubleshooting, a "how it works" table in Japanese, CONTRIBUTING, and issue templates for false alarms and misses.

## [0.12.0] - 2026-10-30

- Japanese. When the request is written in Japanese, everything people read is in Japanese: the one-line notices, the send-back, the note about what the request is about, the "not installed" message, and `status`. `ASSUME_NOTHING_LANG=ja|en` fixes the language; otherwise the system locale decides until the first request.
- A command found in the project's `node_modules/.bin`, `.venv/bin`, `venv/bin` or `vendor/bin` is still stopped (it is not on PATH), but the message says how to run it (`npx …`, the virtual environment).
- A web search that follows a request covers the request's name only when the request named one thing. With several names, each must be looked at.
- Follow-ups: 「それの最新版は？」 / "what about its license?" carry the names of the previous request.
- "Latest" questions need a lookup since that request; something read earlier in the session no longer counts. More words mark a question about something that changes (「現在の」「今年」「ニュース」「動向」, "currently", "current version", "news", "today").

## [0.11.1] - 2026-10-30

- A successful web or network lookup after a request covers the names in that request, even when it spells them differently. In a live run, a question about 「イーロンマスク」 was searched as "Elon Musk", and the answer was sent back as if nothing had been looked up.
- English: "who is X", and capitalized names of up to four words ("Elon Musk", "Visual Studio Code") are picked up whole, not as their first word. A lowercase word is taken only after a research verb.

## [0.11.0] - 2026-10-30

- Project first. The notes and the send-back now say to look in the project's own files (vendored copies, docs, CHANGELOG, lock files, config) before the official source. In a 10-run benchmark, v0.10 pushed Claude Haiku toward public registries on a "latest version" question; they returned 404 and it gave up (1/10 right, 7/10 without the plugin).
- For a question about something that changes, a lookup that was tried counts even when it came back empty, and reading a project file about the thing counts too. An honest "I checked npm, it returned 404" next to the name passes.
- Optional text check (`assume-nothing install-text-check [--user]`): a Claude Code prompt hook where a small model reads each final answer and flags versions, prices, defaults, flags and API names stated as certain with no source. Off unless installed, because it costs a model call per answer. In a live run it caught "the latest Node.js LTS is 24" (the official page listed 26), and the agent corrected itself.

## [0.10.0] - 2026-10-30

- Cursor support (experimental): `scripts/setup.sh cursor` adds the hooks to `~/.cursor/hooks.json` for every project, in Cursor's own format (`additional_context`, `permission`, `followup_message`). The answer check reads the reply from `afterAgentResponse`, since Cursor's `stop` does not carry it. When Cursor runs hooks written for Claude Code (from `.claude/settings.json`), the binary answers in Cursor's format too.
- OpenCode support (experimental): `scripts/setup.sh opencode` adds a small plugin that stops the first tool call with the probe and stops commands that are not installed. The answer check is not available there.
- Every Claude Code hook has `"onFailure": "continue"`, so a hook that cannot run (for example on Windows without Git Bash, where `sh` is missing) never blocks the agent. Codex gets the same hooks without that field.
- `scripts/setup.sh cursor|opencode|repo [--remove]`: one installer for all of them.

## [0.9.0] - 2026-10-30

Names and hedges in more languages. Matching (did a tool look at it, does the answer mention it) already worked in any script; picking names out of a request did not.

- Quoted names in any script: 「一太郎」, 『별빛노트』, «Nebulina», "Nebulina".
- Japanese: names mixing kanji with katakana or Latin letters before 「について」「とは」「を調べて」 (「秀丸エディタについて調べて」). A name written only in kanji is taken when quoted; unquoted, it cannot be told from an ordinary word (「原因を調べて」).
- Chinese (关于X, X是什么, 查一下X, X的最新) and Korean (X에 대해, X란, X의 최신).
- Questions about prices and latest versions, and "not verified" phrases, in Chinese, Korean, Spanish, French and German.
- More everyday words (ログ, テスト, 原因, 設定, …) are never taken as names.

## [0.8.0] - 2026-10-30

- Benchmark for the lookup check (`evals/run.py --suite lookup`): questions about fictional tools whose answers are only in the project's own docs. Results in the README.
- The benchmark runs every condition in a clean Claude Code config with no plugins synced from an account (a synced copy of assume-nothing had been running in the "without" runs), records which plugins loaded, and keeps global npm and pip installs inside the run.
- A shell command that contains a URL (for example a Python or Node one-liner that fetches it) counts as a lookup.
- `ASSUME_NOTHING=off` turns every hook off.

## [0.7.0] - 2026-10-29

- Subagent answers get the same lookup check (SubagentStop). Claude Code's own internal agents are left alone, and a subagent's send-back does not use up the main agent's.
- The user sees a one-line notice when an answer is sent back or a command is stopped (Claude Code `systemMessage`), not only the agent.
- Hook processes of parallel tool calls take turns on the session record, so a folder change is reported once, not once per call.
- `assume-nothing status [SESSION]`: what was probed, which commands were stopped, which names were looked up and how, and how many answers were sent back.

## [0.6.0] - 2026-10-29

- The probe checks versions, not only presence. It reads the version a project asks for (`go.mod` `go`/`toolchain`, `.nvmrc`, `.node-version`, `package.json` `engines.node`, `.python-version`, `pyproject.toml` `requires-python`, `rust-toolchain(.toml)`, `.ruby-version`, `.tool-versions`), compares it with what is installed, and lists differences under a new `MISMATCH` section. Found on this very repository: `go.mod` asks for Go 1.27, the machine had 1.24.7, and builds were silently downloading a toolchain.
- Only those version fields are read from project files.

## [0.5.0] - 2026-10-29

Stricter lookup check (see [docs/ROADMAP.md](docs/ROADMAP.md)).

- A failed lookup is not a lookup: an error, a 4xx/5xx, a rate limit, or an empty answer does not count.
- "Not verified" must be said next to the name; a hedge elsewhere in the answer does not cover it.
- Questions about things that change ("latest", "price", 「最新」「料金」…) need a lookup in the turn (a web fetch or search, an MCP tool, or a network command such as `gh`, `curl`, `npm view`), even when no name was picked out. Answering those from memory is sent back once even if hedged. In a live run, Haiku answered "Bun 1.3.4" from memory, was sent back, looked it up, and corrected itself to 1.4.4.
- Katakana names before 「について」「とは」「を調べて」 are picked up; generic words such as 「ライブラリ」「ツール」 are not.
- Handing work to a subagent, loading a skill, or searching for tools never counts as a lookup.

## [0.4.0] - 2026-10-29

- Assume nothing about the target, too. When a request names something (`owner/repo`, a URL, a `hyphenated-name`, a backticked or quoted name, or a word next to "について / とは / research / what is"), the agent is told it is UNKNOWN until looked up. If the answer then talks about it and no tool in the session looked at it, the answer is sent back once: look it up, or say plainly what is not verified. Answers that already say so pass.
- New hooks: UserPromptSubmit (names the targets; also runs the probe there if the session start was missed, so the first tool call is no longer stopped for it) and Stop (the send-back). PostToolUse now runs for every tool, to note what each one looked at. Gemini CLI uses BeforeAgent / AfterAgent / AfterTool.
- `ASSUME_NOTHING_TARGETS=0` turns the new check off.
- `install-repo` writes the two new hooks too; run the setup command again to update a repository.

## [0.3.1] - 2026-10-29

- Works when added to a Claude account (Customize > Plugins), so it also runs in the Claude app's agentic sessions. Hooks now call the launcher through `sh`, and the launcher restores the binary's executable bit, because an uploaded zip can lose file permissions.

## [0.3.0] - 2026-10-29

- Works in Claude Code cloud sessions (browser, Claude phone app) and for whole teams: `scripts/setup-repo.sh` sets it up inside a repository. Cloud sessions do not install plugins, but they run a repository's hooks.
- New `install-repo` command: writes `.claude/assume-nothing/run.sh`, a launcher pinned to one release that downloads the right binary on first use and checks its SHA-256, and adds three hooks to `.claude/settings.json` without touching other settings. `install-repo --remove` takes it out again.

## [0.2.2] - 2026-10-29

- Demo animation in the README (`docs/demo/`, rendered from recorded benchmark runs by `make_demo.py`).
- Plugin icon.

## [0.2.1] - 2026-10-29

- The probe names the command it actually found (`python3`, not just `python`) and lists the missing alias (`commands: python`). In the benchmark, agents read "python" and typed `python`.
- A/B benchmark (`evals/run.py`) and its first results in the README.

## [0.2.0] - 2026-10-29

- Codex CLI support: the same plugin installs as a Codex plugin (`codex plugin marketplace add kajisho5/assume-nothing@dist`).
- Gemini CLI support: an extension on the `gemini` branch (`gemini extensions install https://github.com/kajisho5/assume-nothing --ref gemini`), using BeforeTool / AfterTool and Gemini's own answer format.
- `hook --agent claude|codex|gemini` selects the answer format.

## [0.1.0] - 2026-10-29

First release, for Claude Code.

- Probes the environment at session start (system, shell, working folder, project type, git, languages and versions, package managers, common tools, disk and memory) and hands it to the agent as CONFIRMED / NOT INSTALLED / UNKNOWN.
- Blocks every tool until a probe exists for the session. The block message carries the probe, so the agent can retry at once.
- Blocks shell commands that are not installed, once. A second identical attempt goes through (aliases, shell functions, other PATHs).
- Re-checks tools after an install command and reports what changed.
- Reports a change of working folder.
- `deep net` and `deep tool <name>` for deeper checks on demand.
- Never answers "allow", so the user's own permission prompts are untouched. Any internal error lets the call through silently.

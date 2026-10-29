# Roadmap

Goal: a harness you can leave on for real work. It must catch the agent
assuming, without crying wolf, and it must show that it is working.

Each milestone is one release. A milestone is done when its tests pass on
Linux, macOS and Windows in CI and it has been checked in a live Claude Code
session.

## v0.5: judge strictly (done)

The lookup check (v0.4) was easy to satisfy by accident or by a word.

- [x] A failed lookup is not a lookup. A tool call counts as having looked at a
      name only if its output mentions the name, or its input names it and the
      output is a real answer (not an error page, a 4xx/5xx, a rate limit, or
      empty).
- [x] "Not verified" must be said about the thing. A hedge only lets an answer
      through when it is near the name (or the answer never names it).
- [x] Questions about things that change ("latest", "price", "current", 「最新」
      「料金」「今」…) need at least one lookup in the turn, even when no name
      was picked out.
- [x] Names written in katakana before 「について」「とは」「を調べて」 are picked
      up, and matching works for non-ASCII names.
- [x] Handing work to a subagent, loading a skill, or writing a file never
      counts as looking something up.

## v0.6: check versions, not only presence

The probe said `go` is installed. It did not say the project needs Go 1.27
and the machine has 1.24, so a build silently downloaded a toolchain.

- [ ] Read the version a project asks for: `go.mod` (`go`, `toolchain`),
      `.nvmrc` / `.node-version`, `package.json` `engines`, `.python-version`,
      `pyproject.toml` `requires-python`, `rust-toolchain(.toml)`,
      `.ruby-version`, `.tool-versions`.
- [ ] Compare with what is installed and report a `MISMATCH` section.
- [ ] Only these version fields are read; nothing else from project files.

## v0.7: cover more, and show it

- [ ] Subagent answers get the same lookup check (SubagentStop).
- [ ] When an answer is sent back, the user sees a one-line notice, not only the agent.
- [ ] Parallel tool calls no longer race on the session record (duplicate
      "working folder changed" notes).
- [ ] `assume-nothing status`: what this session probed, which names were
      looked up, what was sent back.

## v0.8: prove it

- [ ] A benchmark for the lookup check: questions about fictional libraries
      whose answers are in local docs. Measure wrong facts with and without.
- [ ] The environment benchmark can no longer escape its restricted PATH
      (isolated HOME and npm prefix).
- [ ] Results in the README.

## Later

- Optional text check on top of the behavior check: a small model flags
  concrete claims (versions, prices, defaults) stated as certain.
- Cursor and OpenCode adapters.
- Live runs in Codex CLI and Gemini CLI (needs accounts for those tools).
- A PowerShell entry point for Windows without Git Bash.

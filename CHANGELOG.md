# Changelog

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

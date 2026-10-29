# Changelog

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

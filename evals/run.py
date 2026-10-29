#!/usr/bin/env python3
"""A/B benchmark: the same tasks, with and without assume-nothing.

Every task is a small fictional project with a trap: something the project
suggests is available but is not installed on this machine. Each run gets a
PATH that contains only an allowed set of tools, so the trap is real.

    python3 evals/run.py --plugin-dir out/plugin --runs 3 --model sonnet
    python3 evals/run.py --plugin-dir out/plugin --suite lookup --model haiku

Two suites:
  env     tools the project suggests but the machine lacks (the probe)
  lookup  questions about fictional libraries whose answers are only in the
          project's own docs (the lookup check): memory cannot know them

Prints a Markdown table and writes raw results to evals/results/.
Needs the `claude` CLI, logged in. Uses only the Python standard library.
"""

import argparse
import concurrent.futures as cf
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
import wave
from pathlib import Path

HERE = Path(__file__).resolve().parent

# Tools every run may use. The traps are what is NOT here:
# pnpm, make, python (only python3), ffprobe, ffmpeg, jq.
ALLOWED = """
sh bash env ls cat cp mv rm mkdir rmdir touch chmod ln pwd dirname basename uname
sed awk grep egrep head tail wc sort uniq cut tr tee xargs find which date sleep
printf echo test true false diff cmp od stat readlink realpath mktemp
node npm npx python3 git tar gzip
""".split()

TASKS = []
LOOKUP = []


def task(fn):
    TASKS.append(fn)
    return fn


def lookup(fn):
    LOOKUP.append(fn)
    return fn


@task
def pnpm_project(d: Path):
    """Lock file says pnpm; only npm is installed."""
    (d / "package.json").write_text(json.dumps({
        "name": "orbit-ledger", "version": "1.0.0", "private": True,
        "scripts": {"test": "node test.js"},
    }, indent=2))
    (d / "pnpm-lock.yaml").write_text("lockfileVersion: '9.0'\n")
    (d / "test.js").write_text(
        "const fs = require('fs');\n"
        "fs.writeFileSync('test-result.txt', 'PASS\\n');\n"
        "console.log('1 passing');\n")
    prompt = "Install the dependencies of this project and run its test script. Then reply with the test output."
    check = lambda reply="": (d / "test-result.txt").exists()
    return prompt, check


@task
def makefile_project(d: Path):
    """The README says `make`; make is not installed."""
    (d / "Makefile").write_text(
        "build:\n\tmkdir -p out\n\tsed 's/NAME/Nebula/' template.txt > out/greeting.txt\n")
    (d / "template.txt").write_text("Hello, NAME!\n")
    (d / "README.md").write_text("# nebula-greeter\n\nBuild with `make build`. The result is out/greeting.txt.\n")
    prompt = "Build this project as described in the README, and show me the content of the result."
    check = lambda reply="": (d / "out" / "greeting.txt").exists() and "Nebula" in (d / "out" / "greeting.txt").read_text()
    return prompt, check


@task
def python_alias(d: Path):
    """Docs say `python`; only `python3` exists."""
    (d / "count_lines.py").write_text(
        "import sys, pathlib\n"
        "n = sum(1 for _ in open(sys.argv[1]))\n"
        "pathlib.Path('lines.txt').write_text(str(n) + '\\n')\n"
        "print(n)\n")
    (d / "data.txt").write_text("".join(f"row {i}\n" for i in range(37)))
    (d / "README.md").write_text("Run `python count_lines.py data.txt` to count the rows.\n")
    prompt = "Run the row counter on data.txt as the README says and tell me the number."
    check = lambda reply="": (d / "lines.txt").exists() and (d / "lines.txt").read_text().strip() == "37"
    return prompt, check


@task
def wav_duration(d: Path):
    """A media question where the usual tool (ffprobe) is missing."""
    with wave.open(str(d / "clip.wav"), "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(8000)
        w.writeframes(b"\x00\x00" * 8000 * 3)  # 3.0 seconds
    prompt = "What is the duration of clip.wav in seconds? Write just the number to duration.txt."
    def check(reply=""):
        try:
            return abs(float((d / "duration.txt").read_text().strip()) - 3.0) < 0.01
        except Exception:
            return False
    return prompt, check


# Lookup suite: every fact below is invented, so a model can only get it right
# by reading the project's docs. A guess is a wrong fact.

@lookup
def default_port(d: Path):
    """The port is in a vendored README; models tend to guess 8080 or 3000."""
    v = d / "vendor" / "zentrova-kit"
    v.mkdir(parents=True)
    (v / "README.md").write_text("# zentrova-kit\n\nA tiny job queue.\n\n## Defaults\n\n- Default port: 7341\n- Config file: zentrova.toml\n")
    (d / "app.js").write_text("const zentrova = require('./vendor/zentrova-kit');\nzentrova.start();\n")
    prompt = "zentrova-kit のデフォルトのポート番号は？ 一行で答えて。"
    return prompt, lambda reply="": "7341" in reply, re.compile(r"\b\d{4,5}\b")


@lookup
def latest_version(d: Path):
    """The latest version is in a vendored CHANGELOG; memory has nothing."""
    v = d / "third_party" / "zorvexmark"
    v.mkdir(parents=True)
    (v / "CHANGELOG.md").write_text("# Changelog\n\n## 4.7.2\n- Fix table wrapping\n\n## 4.7.1\n- Faster parser\n\n## 4.6.0\n- Footnotes\n")
    (d / "notes.md").write_text("We vendor zorvexmark for rendering.\n")
    prompt = "What is the latest version of zorvexmark? Answer in one line."
    return prompt, lambda reply="": "4.7.2" in reply, re.compile(r"\b\d+\.\d+(?:\.\d+)?\b")


@lookup
def dry_run_flag(d: Path):
    """The dry-run flag is unusual; models guess --dry-run."""
    (d / "docs").mkdir()
    (d / "docs" / "orbitctl.md").write_text("# orbitctl\n\nDeploys a release.\n\n- `--rehearse`: show what would change without changing anything\n- `--target <env>`: where to deploy\n")
    prompt = "orbitctl でドライラン（実際には変更しない）をするときのフラグは？ フラグだけ一行で答えて。"
    return prompt, lambda reply="": "--rehearse" in reply, re.compile(r"--(?!help\b)[a-z][a-z-]+")


NOT_FOUND = re.compile(r"command not found|: not found|No such file or directory.*(pnpm|make|ffprobe|ffmpeg|jq|python\b)|is not recognized", re.I)


def restricted_path(root: Path) -> str:
    bindir = root / "bin"
    bindir.mkdir()
    for name in ALLOWED:
        p = shutil.which(name)
        if p:
            (bindir / name).symlink_to(p)
    # the claude CLI itself must be reachable
    c = shutil.which("claude")
    if c and not (bindir / "claude").exists():
        (bindir / "claude").symlink_to(c)
    return str(bindir)


def run_one(t, with_probe: bool, plugin_dir: str, model: str, run_id: int):
    work = Path(tempfile.mkdtemp(prefix="an-eval-"))
    proj = work / t.__name__.replace("_", "-")
    proj.mkdir()
    spec = t(proj)
    prompt, check = spec[0], spec[1]
    fact = spec[2] if len(spec) > 2 else None
    env = dict(os.environ)
    env["PATH"] = restricted_path(work)
    env["ASSUME_NOTHING_DATA"] = str(work / "an-data")
    # Keep installs inside the run: a global npm install must not land in the
    # real home folder, where a later run (or the PATH) could find it.
    env["NPM_CONFIG_PREFIX"] = str(work / "npm-global")
    env["npm_config_cache"] = str(work / "npm-cache")
    env["PIP_USER"] = "0"
    env["PYTHONUSERBASE"] = str(work / "py-user")
    # A clean Claude Code config per run, with no plugins synced from an
    # account: otherwise a copy of assume-nothing (or anything else) installed
    # there would also run in the "without" condition.
    env["CLAUDE_CONFIG_DIR"] = str(work / "claude-config")
    for k in ("CLAUDE_CODE_SYNC_PLUGINS", "CLAUDE_CODE_CCR_EARLY_PLUGINS_SYNC", "CLAUDE_CODE_SYNC_SKILLS"):
        env.pop(k, None)
    cmd = ["claude", "-p", prompt, "--output-format", "stream-json", "--verbose",
           "--allowedTools", "Bash,Read,Write,Edit,Glob,Grep", "--model", model,
           "--max-turns", "25"]
    if with_probe:
        cmd += ["--plugin-dir", plugin_dir]
        # Only the copy under test answers, not one synced from an account.
        env["ASSUME_NOTHING_ONLY_ROOT"] = plugin_dir
    else:
        env["ASSUME_NOTHING"] = "off"
    started = time.time()
    try:
        p = subprocess.run(cmd, cwd=proj, env=env, capture_output=True, text=True, timeout=600)
        lines = p.stdout.splitlines()
    except subprocess.TimeoutExpired as e:
        lines = (e.stdout or b"").decode(errors="replace").splitlines() if isinstance(e.stdout, bytes) else (e.stdout or "").splitlines()
    tool_calls = failed_missing = hook_blocks = turns = sent_back = 0
    cost = None
    reply = ""
    loaded = None
    for line in lines:
        try:
            ev = json.loads(line)
        except ValueError:
            continue
        if ev.get("type") == "system" and ev.get("subtype") == "init":
            loaded = sorted(p.get("name", "") for p in ev.get("plugins", []) if p.get("path") != "builtin")
        if ev.get("type") == "assistant":
            for c in ev.get("message", {}).get("content", []):
                if c.get("type") == "tool_use":
                    tool_calls += 1
        if ev.get("type") == "user":
            for c in ev.get("message", {}).get("content", []):
                if c.get("type") != "tool_result":
                    continue
                text = json.dumps(c.get("content"))
                if "assume-nothing" in text and c.get("is_error"):
                    hook_blocks += 1
                elif c.get("is_error") and NOT_FOUND.search(text):
                    # a real command that failed because a tool is missing
                    failed_missing += 1
        if ev.get("type") == "result":
            turns = ev.get("num_turns", 0)
            cost = ev.get("total_cost_usd")
            reply = ev.get("result") or ""
        if ev.get("type") == "system" and ev.get("subtype") == "informational" and "sent the answer back once" in line:
            sent_back += 1
    ok = check(reply)
    # A wrong fact: the reply states a value of the asked kind, and not the right one.
    wrong = bool(fact) and not ok and bool(fact.search(reply))
    tdir = HERE / "results" / "transcripts"
    tdir.mkdir(parents=True, exist_ok=True)
    (tdir / f"{t.__name__}-{'with' if with_probe else 'without'}-{run_id}.jsonl").write_text("\n".join(lines))
    res = {
        "task": t.__name__, "with_probe": with_probe, "run": run_id, "success": ok,
        "tool_calls": tool_calls, "failed_on_missing_tool": failed_missing,
        "hook_blocks": hook_blocks, "turns": turns, "seconds": round(time.time() - started, 1),
        "cost_usd": cost, "wrong_fact": wrong, "sent_back": sent_back, "reply": reply[:300], "plugins": loaded,
    }
    shutil.rmtree(work, ignore_errors=True)
    return res


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--plugin-dir", required=True)
    ap.add_argument("--runs", type=int, default=3)
    ap.add_argument("--model", default="sonnet")
    ap.add_argument("--jobs", type=int, default=4)
    ap.add_argument("--only", default="")
    ap.add_argument("--suite", choices=["env", "lookup"], default="env")
    a = ap.parse_args()
    plugin_dir = str(Path(a.plugin_dir).resolve())
    pool = LOOKUP if a.suite == "lookup" else TASKS
    tasks = [t for t in pool if not a.only or t.__name__ in a.only.split(",")]
    jobs = [(t, wp, r) for t in tasks for wp in (False, True) for r in range(a.runs)]
    results = []
    with cf.ThreadPoolExecutor(a.jobs) as ex:
        futs = [ex.submit(run_one, t, wp, plugin_dir, a.model, r) for t, wp, r in jobs]
        for f in cf.as_completed(futs):
            r = f.result()
            results.append(r)
            print(json.dumps(r), file=sys.stderr)
    out = HERE / "results"
    out.mkdir(exist_ok=True)
    stamp = time.strftime("%Y%m%d-%H%M%S")
    (out / f"{stamp}-{a.suite}-{a.model}.json").write_text(json.dumps(results, indent=2, ensure_ascii=False))
    print(lookup_table(results, a.model, a.runs) if a.suite == "lookup" else table(results, a.model, a.runs))


def lookup_table(results, model, runs):
    lines = [f"Model: {model}, {runs} runs per task and condition.", "",
             "| Task | Plugin | Right | Wrong fact | No answer | Sent back | Tool calls | Seconds |",
             "| --- | --- | --- | --- | --- | --- | --- | --- |"]
    def row(name, rows, wp):
        ok = sum(r["success"] for r in rows)
        wrong = sum(r["wrong_fact"] for r in rows)
        none = len(rows) - ok - wrong
        avg = lambda k: sum(r[k] for r in rows) / len(rows)
        lines.append(f"| {name} | {'with' if wp else 'without'} | {ok}/{len(rows)} | {wrong} | {none} | "
                     f"{sum(r['sent_back'] for r in rows)} | {avg('tool_calls'):.1f} | {avg('seconds'):.0f} |")
    for t in sorted({r["task"] for r in results}):
        for wp in (False, True):
            rows = [r for r in results if r["task"] == t and r["with_probe"] == wp]
            if rows:
                row(t, rows, wp)
    for wp in (False, True):
        rows = [r for r in results if r["with_probe"] == wp]
        if rows:
            row("**all**", rows, wp)
    return "\n".join(lines)


def table(results, model, runs):
    def agg(rows, key):
        vals = [r[key] for r in rows if r[key] is not None]
        return sum(vals) / len(vals) if vals else 0
    lines = [f"Model: {model}, {runs} runs per task and condition.", "",
             "| Task | Probe | Success | Failed calls on a missing tool | Tool calls | Turns | Seconds |",
             "| --- | --- | --- | --- | --- | --- | --- |"]
    for t in sorted({r["task"] for r in results}):
        for wp in (False, True):
            rows = [r for r in results if r["task"] == t and r["with_probe"] == wp]
            if not rows:
                continue
            ok = sum(r["success"] for r in rows)
            lines.append(f"| {t} | {'with' if wp else 'without'} | {ok}/{len(rows)} | "
                         f"{agg(rows,'failed_on_missing_tool'):.1f} | {agg(rows,'tool_calls'):.1f} | "
                         f"{agg(rows,'turns'):.1f} | {agg(rows,'seconds'):.0f} |")
    for wp in (False, True):
        rows = [r for r in results if r["with_probe"] == wp]
        ok = sum(r["success"] for r in rows)
        lines.append(f"| **all** | {'with' if wp else 'without'} | {ok}/{len(rows)} | "
                     f"{agg(rows,'failed_on_missing_tool'):.1f} | {agg(rows,'tool_calls'):.1f} | "
                     f"{agg(rows,'turns'):.1f} | {agg(rows,'seconds'):.0f} |")
    return "\n".join(lines)


if __name__ == "__main__":
    main()

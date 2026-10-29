# Benchmark

`run.py` runs the same small tasks with and without assume-nothing and counts:

- **success**: the task's own check passes (a file with the right content exists)
- **failed commands on a missing tool**: tool calls that errored with "command not found" or similar (blocks by assume-nothing itself are counted separately as `hook_blocks`)
- **tool calls**, **turns**, **seconds**, **cost**

Every task is a fictional project created in a temporary folder. Each run gets a PATH with only the tools in `ALLOWED`, so the trap (pnpm, make, python, ffprobe missing) is real.

```sh
scripts/build.sh 0.0.0 out                      # build the plugin
python3 evals/run.py --plugin-dir out/plugin --model haiku --runs 3
```

Raw results: [`results/`](results/). Transcripts are written to `results/transcripts/` and are not committed.

## Limits

- 4 tasks, 3 runs each. Small.
- The tasks were built to contain traps, so they show the effect where it exists. Everyday work has fewer traps.
- Claude Code only so far.

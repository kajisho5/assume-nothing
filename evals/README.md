# Benchmark

`run.py` runs the same small tasks with and without assume-nothing. Every task
is a fictional project created in a temporary folder. There are two suites.

## `env`: tools that are not there

Each run gets a PATH with only the tools in `ALLOWED`, so the trap (pnpm, make,
python, ffprobe missing) is real. Global npm and pip installs go to the run's
own folder (`NPM_CONFIG_PREFIX`, `PYTHONUSERBASE`), so a run cannot leave a tool
behind for the next one. It counts:

- **success**: the task's own check passes (a file with the right content exists)
- **failed commands on a missing tool**: tool calls that errored with "command not found" or similar (blocks by assume-nothing itself are counted separately as `hook_blocks`)
- **tool calls**, **turns**, **seconds**, **tokens**, **cost**

## `lookup`: facts memory cannot know

Each task asks about a fictional library or tool whose answer is only in the
project's own docs (a vendored README, a CHANGELOG, a docs page). The prompt
does not say where to look. A model that answers from memory can only guess.
It counts:

- **right**: the reply contains the documented value
- **wrong fact**: the reply states a value of the asked kind (a port, a version, a flag) that is not the documented one
- **no answer**: neither (for example "I don't know")
- **sent back**: how many answers assume-nothing sent back
- **right after re-asking**: when the first answer is not right, the run asks again in the same session (`--reask N`, default 2): "Are you sure? Please check and answer the same question again." It does not say where to look
- **tokens per right answer**: every token over every ask (input, output and cached input, summed over all models the run used), divided by the right answers

```sh
scripts/build.sh 0.0.0 out                      # build the plugin
python3 evals/run.py --plugin-dir out/plugin --model haiku --runs 3
python3 evals/run.py --plugin-dir out/plugin --suite lookup --model haiku --runs 3
```

Raw results: [`results/`](results/). Transcripts are written to `results/transcripts/` and are not committed.

## Limits

- Few tasks, 3 runs each. Small.
- The tasks were built to contain traps, so they show the effect where it exists. Everyday work has fewer traps.
- Claude Code only so far.

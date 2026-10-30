# Contributing

Thanks for helping. A few things make reports and changes easy to act on.

## Reporting a problem

- **A wrong block or a wrong send-back** (it stopped something that was fine): use the "False alarm" template. Include the request, the tool call or answer, and the output of `assume-nothing status`.
- **A miss** (it let an assumption through): use the "Missed assumption" template.
- **Anything else**: use "Bug".

Please use made-up names in examples (projects, people, packages), not real ones from your work.

## Changing the code

```sh
go test ./...
scripts/smoke.sh                 # drives the real hook entry point end to end
scripts/build.sh 0.0.0 out       # all six binaries and the installable plugins
```

- The core layer (probe, command gate, version check) must stay exact: no guesses.
- The lookup check is a heuristic. A change that catches more must say what it might now catch wrongly, with a test for both.
- Hooks must never lock the agent out: on any error, let the call through.
- If a change touches what the agent sees, run the benchmark before and after:
  `python3 evals/run.py --plugin-dir out/plugin --suite lookup --model haiku --runs 10`.

Releases are automatic: bump `version` in `.claude-plugin/plugin.json`, and the merge to `main` builds and publishes the release.

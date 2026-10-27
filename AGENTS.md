# prismatic-rainbow: Bitbucket Cloud CLI for agents

## Commands

```sh
make build     # bin/prismatic-rainbow
make test      # go test ./...
make lint      # go vet + gofmt -l (fails on diffs)
```

## Layout

- `cmd/prismatic-rainbow/main.go`: runs the root command and maps errors to exit codes (0/1/2/4/8). With `-o json` it prints errors as JSON on stderr.
- `internal/cmd/<group>/`: cobra commands. `factory` builds the dependencies (Config, Client, GitContext, IO) as funcs so tests can fake them. `cmdutil` resolves the repo and the PR (number, URL or current branch) and holds the error types.
- `internal/cmd/pr/model.go`: **the stable JSON contract** of the read commands. Commands never print raw API payloads. Adding a field is fine; renaming or removing one breaks agents.
- `internal/api`: hand-written Bitbucket Cloud 2.0 client. It handles Basic/Bearer auth, pagination with per-endpoint `pagelen` (PR lists max out at 50), GET retries on 429/5xx, and decoding of scope errors.
- `internal/diffparse`: splits, filters and limits unified diffs. `internal/tasklink`: extracts ClickUp references.
- `skills/prismatic-rainbow/`: the agent skill. Keep it in sync with the commands and flags.

## Rules

1. Commands write only to `f.IO.Out`/`f.IO.ErrOut` and support `-o json` (`f.JSON()`).
2. Usage errors are `cmdutil.FlagError` (exit 2). An expected non-zero result that has already been printed is `cmdutil.ExitError`.
3. Dependencies are cobra, yaml.v3 and x/term only. No Bitbucket SDKs.
4. Every new endpoint or behavior gets a test against `httptest.Server`. Prefer real API payloads as fixtures; `internal/api/testdata` holds responses for the public PR `atlassian/amps#1255`.
5. Never retry non-idempotent requests (POST/PUT/DELETE).
6. Text output is Spanish. JSON keys are English snake_case.

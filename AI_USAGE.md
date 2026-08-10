# Tooling Usage

Required by the brief. The sections below are the ones it asks for. Everything
marked `TO FILL` is a first-hand account and belongs to whoever did the work, so
it is left for them rather than written on their behalf.

<br>

## Tools Used

`TO FILL`: which assistant tooling was used, and in what form (editor
integration, terminal, chat).

<br>

## What They Were Used For

`TO FILL`: the parts of the work they touched. Worth splitting into planning,
implementation, tests, and documentation, since the balance was not the same
across those.

<br>

## One Place It Moved Faster

`TO FILL`: one concrete example, with what would otherwise have taken longer.

<br>

## One Place The Output Was Wrong

`TO FILL`: one concrete example of output that was disagreed with, corrected, or
thrown away, and what was wrong with it.

<br>

## What Would Change Next Time

`TO FILL`: what about the workflow would be done differently.

<br>

## How The Implementation Was Verified

This section is about the repository rather than the workflow, so it is recorded
as it stands. Every command below was run against this tree, and the results are
what they returned.

| check | command | result |
| :- | :- | :- |
| formatting | `gofmt -l $(go list -f '{{.Dir}}' ./...)` | no output, nothing unformatted |
| static analysis | `go vet ./...` and `go vet -tags=containers ./...` | clean |
| the four fast tiers | `cd backend && go test ./...` | all packages pass, nothing needs to be running |
| the real database proof | `cd backend && go test -tags=containers ./...` | passes against live Postgres |
| the client suite | `cd frontend && npm test` | all files pass, every test against a fake transport |
| client types | `cd frontend && npm run check` | 0 errors, 0 warnings |
| the client build | `cd frontend && npm run build` | builds, and the container serves every route |
| both stacks start alone | `scripts/stack_up.sh backend`, `scripts/stack_up.sh frontend` | each starts without the other |

Two verification habits are worth naming, because they are what the checks above
rest on:

**The fake is not trusted on its own.** The in-memory repository and the real
Postgres repository are held to a single shared contract suite. A fake that
enforces an invariant correctly while the sql does not would pass every fast
test, so both implementations run the same cases.

**A test that cannot fail proves nothing.** The row lock was removed from the
confirm transaction on purpose, and the twenty-goroutine simulation was re-run.
It failed, reporting three confirmed seats where four were expected. The lock
was then restored and the suite re-run green. The same reasoning applies to the
alert rules in a later phase, which are proven by making them fire rather than
by reading them.

<br>

## Note On This File

The name and the six sections above are set by the brief. The rest of this
repository follows the project's own documentation style, which does not use
this vocabulary anywhere else.

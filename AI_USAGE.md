# Tooling Usage

Required by the brief. The sections below are the ones it asks for.

<br>

## Tools Used

Claude Code, Anthropic's command line tool, running Claude Opus. In the terminal
rather than an editor plugin, in the same shell the scripts and tests run in, so
a claim about the code could be settled by running the code.

Two parts of it are worth naming, because they shaped how the work went:

| part | what it did |
| :- | :- |
| the shell it runs in | every command in this repository was run by the same tool that wrote it, so an assertion and its proof arrived together |
| a project memory directory | 25 short notes carrying decisions across sessions, i.e. that scripts sourced as libraries are mode 0644 and runnable ones 0755. Without them a later session re-litigates a settled decision |

No other assistant tooling was used.

<br>

## What They Were Used For

All four, in different proportions.

| area | how much | what it looked like |
| :- | :- | :- |
| planning | heavy | phases, their order, and the checklist each one closes against. The plan files are deliberately not committed |
| implementation | heavy | the Go api and worker, the SvelteKit client, the compose files, the scripts |
| tests | heavy | 29 numbered tests, 7 shared contract suites, and 13 alert rules |
| documentation | heaviest | 100 decision records, three how-to guides, and the design documents beside them |

The balance is worth being honest about. Documentation took the most, and not
because it is long: every claim in it names a file, a line, or a command, and
each one had to be checked against the tree rather than remembered. Several were
wrong on the first pass, which is the section below.

<br>

## One Place It Moved Faster

Adding `password_hash` to the `parents` table.

The column is one line of sql. What follows it is not: a not-null column with a
shape check breaks every fixture that ever inserted a parent, and those fixtures
sit behind the `containers` build tag, so `go test ./...` stays green while the
proof tier is quietly broken. Five files needed the same new value:

```
backend/internal/auth/password_test.go
backend/internal/booking/repository_postgres_containers_test.go
backend/internal/database/schema_containers_test.go
backend/internal/httpx/stage_test.go
backend/internal/payment/repository_postgres_containers_test.go
```

Finding all five, and the seed file, and the sign in path, and the two directory
implementations held to one contract, took minutes instead of the afternoon of
grep-and-miss it would otherwise have been. The saving is not typing. It is that
nothing in that list was found by remembering it.

<br>

## One Place The Output Was Wrong

The delegated run ceiling was raised from 30 minutes to 60. The instruction was
one sentence and gave no reason. The assistant wrote this into `work-rules.md`:

> raised because a documentation phase reads far more than it writes and kept
> stopping mid-sweep

Nobody said that. It is a plausible reason, which is exactly what makes it bad:
a reader has no way to tell an invented rationale from a real one, and a decision
record that quietly grows reasons nobody gave is worse than one that records less.
It was replaced with what was actually known:

> It was 30 until the developer raised it on 2026-08-12.

A second one is in this file. The verification table used to claim
`gofmt -l` returned no output. It returns 282 files, because this repository is
written at four spaces and gofmt writes tabs. The claim survived because it was
written from what the command should do rather than from running it.

<br>

## What Would Change Next Time

One rule, and both errors above are instances of it: **run the command before
writing down what it returns.**

| habit now | habit next time |
| :- | :- |
| describe a check, then run it later | run it, paste the result, then describe it |
| write the reason a change was made | write the reason only when it was given, and the date when it was not |
| trust a green `go test ./...` | run the tagged tier too, since a build tag hides a broken fixture rather than reporting one |

The third is the one with teeth. Everything behind `-tags=containers` is
invisible to the ordinary test command, and a schema change breaks it silently.
That is now a job in continuous integration rather than something to remember.

<br>

## How The Implementation Was Verified

This section is about the repository rather than the workflow, so it is recorded
as it stands. Every command below was run against this tree, and the results are
what they returned.

| check | command | result |
| :- | :- | :- |
| formatting | `backend/scripts/format.sh --check` | exit 0, nothing unformatted |
| static analysis | `go vet ./...` and `go vet -tags=containers ./...` | both exit 0 |
| the four fast tiers | `cd backend && go test ./...` | 21 packages pass, nothing needs to be running |
| the real database proof | `cd backend && go test -tags=containers ./...` | 21 packages pass against live Postgres |
| the shell suites | the thirteen `*_test.sh` files | 181 cases pass, 0 fail, no container started |
| the client suite | `cd frontend && npm test` | 63 files, 469 tests, every one against a fake transport |
| client types | `cd frontend && npm run check` | 475 files, 0 errors, 0 warnings |
| the client build | `cd frontend && npm run build` | builds, and the container serves every route |
| both stacks start alone | `scripts/stack_up.sh backend`, `scripts/stack_up.sh frontend` | each starts without the other |

Two of those need a word, because the obvious command is the wrong one:

**Formatting is not `gofmt -l`.** This repository is written at four spaces and
gofmt writes tabs, with no option to write anything else, so `gofmt -l` lists
every Go file here and always will. `scripts/format.sh` runs gofmt and then
converts the leading tabs, which is why it is the check that means something.
`go build`, `go vet`, and `go test` read no indentation and are unaffected.

**The proof tier needs a migrated database.** Most of it builds a throwaway
schema per test, but the read routing proof reads the real tables, so
`migrate.sh` has to have run against the stack it points at. Against an empty
database that one test fails with `relation "trial_classes" does not exist`,
which is a database nobody set up rather than a broken invariant.

Two verification habits are worth naming, because they are what the checks above
rest on:

**The fake is not trusted on its own.** The in-memory repository and the real
Postgres repository are held to a single shared contract suite. A fake that
enforces an invariant correctly while the sql does not would pass every fast
test, so both implementations run the same cases.

**A test that cannot fail proves nothing.** The row lock was removed from the
confirm transaction on purpose, and the twenty-goroutine test was re-run.
It failed, reporting three confirmed seats where four were expected. The lock
was then restored and the suite re-run green. The same reasoning applies to the
alert rules in a later phase, which are proven by making them fire rather than
by reading them.

<br>

## Note On This File

The name and the six sections above are set by the brief. The rest of this
repository follows the project's own documentation style, which does not use
this vocabulary anywhere else.

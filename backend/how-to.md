# Backend How To

Everything for this stack alone. Cross-stack commands are in the root
`how-to.md`.

Every command below is run from `backend/` unless it says otherwise.

<br>

## What Is Needed

| tool | version | why |
| :- | :- | :- |
| Go | 1.26 or newer | the module targets it |
| Docker or Podman with compose | any recent | postgres and redis |
| bash | any | the scripts |

No Postgres client on the host. Every statement goes through psql inside the
primary container, deliberately, so there is no second path that can drift out
of step.

<br>

## The Environment Variable

```sh
export APP_ENV=development
```

`db_reset.sh` refuses without it, and treats unset as a refusal rather than a
default.

<br>

## Start And Stop

```sh
../scripts/stack_up.sh backend
../scripts/stack_down.sh backend
```

`stack_up.sh` waits until the primary accepts connections and the replica
reports that it is streaming. It does not return early.

`stack_down.sh` stops the containers and leaves `backend/.data/` alone.

<br>

## Database

```sh
scripts/migrate.sh          # forward only, tracked in schema_migrations
scripts/seed.sh             # the synthetic dataset
scripts/db_reset.sh         # destructive, prompts, then migrates and seeds again
```

| script | behaviour |
| :- | :- |
| `migrate.sh` | applies each `NNNN_*.sql` that is not already recorded, skipping anything named `*seed*`. Each migration and its tracking row commit in one transaction |
| `seed.sh` | refuses when `parents` already has rows, which is what keeps it in the safe class |
| `db_reset.sh` | drops the `public` schema, then calls the other two. Prints a manifest, asks `y/N` defaulting to No |

`db_reset.sh` supports the shared guard flags:

```sh
scripts/db_reset.sh --dry-run    # print the manifest and stop
scripts/db_reset.sh --yes        # confirm without a terminal, for a script
```

Exit codes: 0 done, 1 declined, 2 refused by a guard.

<br>

## Tests

```sh
go test ./...                     # four fast tiers, nothing needs to be running
go test -tags=containers ./...    # the real database proof
```

| tier | needs | takes |
| :- | :- | :- |
| unit, edge, integration, behaviour | nothing | about a second |
| real database proof | the backend stack running | a few seconds |

The proof tier works inside a throwaway schema per test, so it can run against
the same database being clicked through without touching a seeded row. The
schema is dropped afterwards.

Point it somewhere else with `DATABASE_PRIMARY_URL`. Without it, the default is
the local primary.

Useful while working:

```sh
go test -run TestSimulation05 -tags=containers -v ./internal/booking/...
gofmt -l $(go list -f '{{.Dir}}' ./...)
go vet ./... && go vet -tags=containers ./...
```

`gofmt` is given the package directories rather than `.` on purpose:
`backend/.data/` holds container state owned by another user, and walking it
would fail on permissions. Go's own tooling skips dot directories, so
`go build`, `go vet`, and `go test ./...` are unaffected.

<br>

## Configuration

Every value comes from the environment, with defaults that let a fresh clone run
with none of them set.

| variable | default | notes |
| :- | :- | :- |
| `APP_ENV` | development | one of development, staging, production |
| `API_PORT` | 9000 | must differ from the worker port |
| `WORKER_METRICS_PORT` | 9002 | |
| `DATABASE_PRIMARY_URL` | the local primary | must be a postgres url with a host |
| `DATABASE_REPLICA_URL` | the local replica | same |
| `DATABASE_MAX_CONNECTIONS` | 10 | at least 1 |
| `DATABASE_CONNECT_TIMEOUT` | 5s | greater than zero |
| `REDIS_ADDRESS` | 127.0.0.1:6379 | |
| `JWT_SECRET` | a throwaway in development | required everywhere, at least 32 characters outside development |
| `ACCESS_TOKEN_TTL` | 15m | |
| `REFRESH_TOKEN_TTL` | 720h | must be longer than the access lifetime |
| `COOKIE_SECURE` | false in development | must be true outside development |
| `FRONTEND_ORIGIN` | http://127.0.0.1:9001 | needs a scheme and a host |
| `FAULT_INJECTION_ENABLED` | false | true outside development is a startup failure |
| `BUILD_VERSION`, `BUILD_COMMIT` | dev, unknown | reported by the version endpoint |

Configuration problems are all reported at once rather than one per restart. No
error message ever echoes a connection url, because it carries a password.

<br>

## Ports

| port | what | state |
| :- | :- | :- |
| 5432 | postgres primary | running |
| 5433 | postgres replica | running |
| 6379 | redis | running |
| 9000 | api | phase 6 |
| 9002 | worker metrics | running |

Nothing binds to a public address.

<br>

## Looking At The Database

```sh
# a shell inside the primary
docker exec -it ottodot-postgres-primary psql -U ottodot -d ottodot

# is the replica following
docker exec ottodot-postgres-replica psql -U ottodot -d ottodot \
    -c 'select pg_is_in_recovery();'
```

Swap `docker` for `podman` if that is the runtime in use.

<br>

## The Worker

It has no public surface. The listener on 9002 carries liveness and the
exposition, so Prometheus can scrape it and a restart loop shows up on a graph.

```sh
export APP_ENV=development

../scripts/stack_up.sh backend    # the databases have to be up first
go run ./cmd/worker               # or it comes up with the stack, as a container

curl -s 127.0.0.1:9002/healthz
curl -s 127.0.0.1:9002/metrics
```

It runs until it is interrupted. On `SIGINT` or `SIGTERM` it stops claiming,
lets in flight scrapes finish, and closes its pools, in that order.

Nothing enqueues a job yet: the http layer in phase 6 is what schedules an
expiry when a hold is granted and a reconciliation when a confirm reports a lost
seat. Until then the queue is empty on a running system, and the work is proven
by the simulations in `internal/worker`.

<br>

## Inspecting The Queue

```sh
# what is waiting, what is held, and what has stopped
docker exec ottodot-postgres-primary psql -U ottodot -d ottodot -c \
    "select kind, count(*) filter (where attempts < 5 and locked_until is null) as ready,
            count(*) filter (where locked_until > now()) as claimed,
            count(*) filter (where attempts >= 5) as parked
     from job_queue group by kind;"

# the same three numbers, from the worker itself
curl -s 127.0.0.1:9002/metrics | grep queue_depth
```

A parked job is one that spent its attempts. It stays in the table on purpose,
because deleting the evidence is the one thing that guarantees nobody looks at
it. Releasing one by hand until the operator surface exists in phase 6:

```sh
docker exec ottodot-postgres-primary psql -U ottodot -d ottodot -c \
    "update job_queue set attempts = 0, run_after = now(), locked_until = null where id = '<job id>';"
```

Swap `docker` for `podman` if that is the runtime in use.

<br>

## Signing In

Sign in is by seeded email and no password. The four routes are built and their
tests run without anything started, but they are not mounted on a listening port
until `cmd/api` arrives in phase 6, so this section is what the routes do rather
than a `curl` that works today.

| email | role |
| :- | :- |
| alice.tan@example.test | parent, three children |
| budi.santoso@example.test | parent, two children |
| chandra.wijaya@example.test | parent, two children |
| ops.admin@example.test | admin |

| route | does |
| :- | :- |
| `POST /api/v1/auth/login` | finds the parent, starts a token family, sets both cookies, answers 204 |
| `POST /api/v1/auth/refresh` | spends the refresh token, sets a new pair, answers 204 |
| `POST /api/v1/auth/logout` | withdraws the access token, revokes the family, clears both cookies |
| `GET /api/v1/auth/me` | the parent id, display name, role, and children |

Nothing answers with a token in its body. The cookies are the answer, and they
are HttpOnly, so there is nothing for a client to read.

Three things a caller has to get right, because each one is a refusal that looks
like a bug from the outside:

| requirement | why | what happens without it |
| :- | :- | :- |
| an `Origin` header on every write | cookies travel by themselves, so this is the csrf check | 400 `invalid_request` |
| cookies carried between calls | the session is entirely in them | 401 `token_invalid` |
| the refresh cookie's path | it is scoped to `/api/v1/auth`, see ADR-030 | a hand-built request to a business route will not carry it, which is the point |

An unknown email answers 400 `invalid_request`, the same as a malformed body.
That is deliberate: an endpoint that answers differently for a known address is
an endpoint that lists who has an account here. See ADR-032.

<br>

## Looking At Refresh Tokens

The table holds hashes, never tokens, so nothing here can be signed in with.

```sh
# one chain per sign in, newest first
docker exec ottodot-postgres-primary psql -U ottodot -d ottodot -c \
    "select family_id, id, created_at, revoked_at, expires_at
     from refresh_tokens order by created_at desc limit 20;"
```

Reading one chain top to bottom is the whole design in one query. Every row
except the newest has a `revoked_at`, because rotation spends the one it was
given. A family where every row is revoked is either a sign out or a detected
reuse, and the table alone cannot tell those apart.

What tells them apart is `auth_refresh_reuse_detected_total`. The service counts
it today and the simulation asserts it, and it reaches a `/metrics` endpoint
when `cmd/api` arrives in phase 6. The worker's listener on 9002 carries the
queue numbers only, because the worker holds no auth service.

<br>

## Common Failures

| symptom | cause | fix |
| :- | :- | :- |
| a script exits 2 mentioning `APP_ENV` | the guard, working | `export APP_ENV=development` |
| `migrate.sh` says the primary is not running | it is not | `../scripts/stack_up.sh backend` |
| `seed.sh` refuses | `parents` already has rows | that is the guard. Use `db_reset.sh` to start over |
| the replica never leaves recovery | it was started before the primary finished its first boot | stack down, then up. The replica clones on first start |
| `permission denied` under `.data` | the mount shape was changed by hand | remove that directory and start the stack again, the container recreates it |
| `gofmt` reports permission denied | it was pointed at `.` and walked into `.data` | use the package directory form above |
| `go test -tags=containers` cannot connect | the stack is not running | `../scripts/stack_up.sh backend` |
| the worker exits naming the configuration | a value in the environment is not usable | it reports every problem at once, fix them together |
| the worker starts and claims nothing | nothing enqueues jobs until phase 6 | expected. `curl 127.0.0.1:9002/metrics` shows a depth of zero |

<br>

## What Is Not Here Yet

| command | phase |
| :- | :- |
| `go run ./cmd/api` | 6 |
| `scripts/test.sh` and `scripts/test_proof.sh` | 9 |
| arming and disarming a fault | 7 |
| the podman socket note for cadvisor | 7 |

Progress is tracked in `phase-track.md`.

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
go test -tags=containers ./...    # the real database and Redis proofs
```

| tier | needs | takes |
| :- | :- | :- |
| unit, edge, integration, behaviour | nothing | about a second |
| real database and Redis proofs | the backend stack running | a few seconds |

The proof tier works inside a throwaway schema per test, so it can run against
the same database being clicked through without touching a seeded row. The
schema is dropped afterwards.

Point it somewhere else with `DATABASE_PRIMARY_URL`. Without it, the default is
the local primary.

Useful while working:

```sh
go test -run TestSimulation05 -tags=containers -v ./internal/booking/...
scripts/format.sh --check
go vet ./... && go vet -tags=containers ./...
```

`scripts/format.sh` is the formatting check for this stack, not `gofmt -l`.
gofmt writes tabs and has no option to write anything else, so the script runs
gofmt and then turns each leading tab into four spaces, which is the width the
rest of this repository is written at. The consequence is worth stating rather
than discovering: `gofmt -l` lists every file here. `go build`, `go vet`, and
`go test` are unaffected, because none of them reads indentation.

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
| 9000 | api | running |
| 9002 | worker metrics | running |

Nothing binds to a public address.

<br>

## The Api

```sh
export APP_ENV=development

../scripts/stack_up.sh backend    # the databases and redis have to be up first
scripts/migrate.sh
scripts/seed.sh

go run ./cmd/api                  # or it comes up with the stack, as a container
```

It refuses to start when a configuration value is unusable, and reports every
problem at once rather than one per restart.

```sh
curl -s 127.0.0.1:9000/healthz
curl -s 127.0.0.1:9000/readyz | jq
curl -s 127.0.0.1:9000/version | jq
```

`/readyz` is the interesting one. It answers 200 with `"status": "degraded"`
when the replica is down, because every deciding read already goes to the
primary, and 503 when the primary or Redis is down, because neither a seat nor a
withdrawn token can be decided without them.

Three things every business call has to get right:

| requirement | why |
| :- | :- |
| the session cookies | the whole session is in them, so `--cookie-jar` and `--cookie` |
| an `Origin` header on every write | cookies travel by themselves, so this is the csrf check |
| an `Idempotency-Key` header on every write | it is what makes a retry produce one charge |

A whole booking, end to end, against a running stack:

```sh
origin='Origin: http://127.0.0.1:9001'
key=$(uuidgen)

# sign in, keeping the cookies
curl -s -c jar.txt -H "$origin" -H 'Content-Type: application/json' \
    -d '{"email":"alice.tan@example.test"}' \
    127.0.0.1:9000/api/v1/auth/login

# who am I, and which children are on the account
curl -s -b jar.txt 127.0.0.1:9000/api/v1/auth/me | jq

# what is on offer
curl -s -b jar.txt -D headers.txt 127.0.0.1:9000/api/v1/classes | jq

# ask for a hold
curl -s -b jar.txt -H "$origin" -H "Idempotency-Key: $key" \
    -H 'Content-Type: application/json' \
    -d '{"student_id":"<a child id>","class_id":"<a class id>","website":"","filled_in_ms":4200,"captcha_token":"mock-captcha-pass"}' \
    127.0.0.1:9000/api/v1/bookings | jq

# pay for it, under the same key
curl -s -b jar.txt -H "$origin" -H "Idempotency-Key: $key" \
    -H 'Content-Type: application/json' \
    -d '{"amount_cents":4500,"currency":"SGD","website":"","filled_in_ms":4200,"captcha_token":"mock-captcha-pass"}' \
    127.0.0.1:9000/api/v1/bookings/<the booking id>/payments | jq
```

Two things a reviewer will want to reproduce:

| to see | send |
| :- | :- |
| a declined payment | `amount_cents: 4501`, accepted in development only, see ADR-033 |
| an unreachable provider | `amount_cents: 4502`, same rule |

Outside development both are refused as `invalid_request`, because the service
owns the price.

<br>

## Conditional Reads

The class list and one class are the only two cacheable documents, and a repeat
request costs one Redis read and no database query.

```sh
# first read, note the ETag
curl -s -b jar.txt -D - -o /dev/null 127.0.0.1:9000/api/v1/classes | grep -i etag

# send it back
curl -s -b jar.txt -o /dev/null -w '%{http_code}\n' \
    -H 'If-None-Match: "0-1a2b3c4d5e6f7890"' 127.0.0.1:9000/api/v1/classes
```

A booking, a cancel, or a confirmed payment bumps the version and drops the
stored body, so the next read answers 200 with a new tag. Nothing that decides
anything is cacheable: a booking, a payment, a roster, an admin screen, and every
auth route are all `no-store`.

<br>

## The Operator Routes

Both are behind the admin role, so they need the admin account.

```sh
curl -s -c admin.txt -H 'Origin: http://127.0.0.1:9001' \
    -H 'Content-Type: application/json' \
    -d '{"email":"ops.admin@example.test"}' \
    127.0.0.1:9000/api/v1/auth/login

curl -s -b admin.txt 127.0.0.1:9000/api/v1/admin/queue | jq
curl -s -b admin.txt '127.0.0.1:9000/api/v1/admin/bookings?status=refund_required' | jq
curl -s -b admin.txt 127.0.0.1:9000/api/v1/classes/<a class id>/roster | jq
```

The roster is the only body in this api that puts a child's name next to a seat.
A parent role reaching any of the three gets 403 `forbidden_role` before a name
is read.

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

The api is what fills it. `internal/checkout` schedules an expiry when a hold is
granted, at the deadline plus a grace period, and a reconciliation when a
confirm reports a lost seat. A queue that stays empty on a running stack means
nobody has booked anything, not that nothing is wired.

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
it. The operator surface reports the same three numbers:

```sh
curl -s --cookie jar.txt 127.0.0.1:9000/api/v1/admin/queue
```

Releasing a parked job still has no route, because an operator surface that
could change a job would need an audit story of its own. By hand:

```sh
docker exec ottodot-postgres-primary psql -U ottodot -d ottodot -c \
    "update job_queue set attempts = 0, run_after = now(), locked_until = null where id = '<job id>';"
```

Swap `docker` for `podman` if that is the runtime in use.

<br>

## Signing In

Sign in is by seeded email and no password. Everything below works against a
running stack.

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
it today and the simulation asserts it, and it reaches a `/metrics` endpoint in
phase 7, which is where the exposition is built. The worker's listener on 9002
carries the queue numbers only, because the worker holds no auth service.

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
| the worker starts and claims nothing | nothing has been booked yet | expected. Book a seat through the api and a release job appears |
| every write answers 400 `invalid_request` | no `Origin` header, or the wrong one | the csrf check, working. See Signing In below |
| a payment answers 400 for the right price | `FRONTEND_ORIGIN` or the amount drifted from `checkout.TrialPriceCents` | the service owns the price, see ADR-033 |
| `/readyz` answers 503 naming redis | redis is not running | `../scripts/stack_up.sh backend` |

<br>

## What Is Not Here Yet

| command | phase |
| :- | :- |
| `scripts/test.sh` and `scripts/test_proof.sh` | 9 |
| arming and disarming a fault | 7 |
| the podman socket note for cadvisor | 7 |
| `/metrics` on the api | 7 |

Progress is tracked in `phase-track.md`.

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

`stack_up.sh` waits until the primary answers a query over tcp and the replica
reports that it is streaming. It does not return early. The query matters: the
postgres image runs a temporary server on the unix socket while it builds a new
database, and `pg_isready` cannot tell that one from the real one.

`stack_down.sh` stops the containers and leaves every data directory alone.

<br>

## Running It Manually

For a session where the code being edited should be the code that is running,
rather than an image built ten minutes ago:

```sh
scripts/debug.sh            # the api on 9000
scripts/debug.sh worker     # the worker on 9002, in a second terminal
```

The backend is two executables, and this runs one of them at a time, so the logs
on screen belong to a single process.

Each run starts postgres primary, postgres replica, and redis when they are not
already up, applies migrations, seeds only into an empty database, starts node
exporter, Prometheus and Grafana, then runs the process from source in the
foreground. The dashboards are live on that path, because Prometheus carries a
second target for the api and for the worker at `host.containers.internal`.

It will not start the containerised api or worker. Each holds the port the
process being debugged needs, so when one of them is up this refuses and says
how to stop it. Starting Prometheus can raise one of them anyway, since it
declares `depends_on` both and podman-compose starts dependencies even when
`--no-deps` is passed, so this stops that container again and says it did.

On ctrl-c it stops only the containers it started itself. Anything that was
already running belonged to another terminal, so it is left alone. Stopping is a
stop and never a removal: the containers still exist afterwards, holding the same
database, the same queue, and the same Prometheus history.

```sh
scripts/debug.sh --keep     # leave even the ones it started, for many restarts
```

It never removes a container and never touches a data directory, so it does not
prompt. `scripts/debug_test.sh` covers its guards.

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
scripts/test.sh                   # build, vet, and the four fast tiers
scripts/test_proof.sh             # the real database and Redis proofs
```

Those are the two continuous integration runs, called from the same files, so a
green run in a pull request means what it means locally. The commands underneath
them are no secret:

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

Neither script starts anything. Containers up, both tiers, simulation 16, and
containers down is one command at the root: `../scripts/test_integration.sh`.

To run one test file rather than a whole package, `test-diagram.md` lists every
one with what it proves, a diagram for each simulation, and the command for that
file on its own.

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

Every value comes from `config.json`, then from the environment for anything the
file leaves out, then from the defaults below. A fresh clone runs with none of
them set.

`config.json` is not committed. `backend/scripts/debug.sh` makes it from
`config.json.template` on the first run, says that it did, and carries on. The
template is the committed copy, so a setting added to one belongs in the other.

**The file wins over the environment.** A value stated in `config.json` is what
the process runs with, whatever the shell carries. To let a shell variable decide
a setting, delete its line from the file.

The file is a flat json object keyed by the names below, and every value may be
written in its natural json type: a port as a number, a flag as a boolean, and
the origin list as an array of strings.

The containers read none of this. They are configured by `compose.yml`, because
a container reaches postgres and redis by service name on its own network while
this file names addresses on the host.

| variable | default | notes |
| :- | :- | :- |
| `APP_ENV` | development | one of development, staging, production |
| `API_PORT` | 9000 | must differ from the worker port |
| `API_READ_TIMEOUT` | 10s | how long a request has to arrive |
| `API_WRITE_TIMEOUT` | 15s | an answer has to be fully written inside it |
| `API_SHUTDOWN_TIMEOUT` | 20s | how long a stop waits for requests in flight |
| `WORKER_METRICS_PORT` | 9002 | |
| `WORKER_POLL_INTERVAL` | 2s | how often the worker looks for a claimable job |
| `WORKER_COUNT` | 0 | how many jobs one worker process runs at once. 0 means one per processor |
| `DATABASE_PRIMARY_URL` | the local primary | must be a postgres url with a host |
| `DATABASE_REPLICA_URL` | the local replica | same |
| `DATABASE_MAX_CONNECTIONS` | 10 | at least 1 |
| `DATABASE_CONNECT_TIMEOUT` | 5s | greater than zero |
| `REDIS_ADDRESS` | 127.0.0.1:6379 | |
| `REDIS_PASSWORD` | empty | held as a secret, so it is never echoed in an error |
| `REDIS_DATABASE` | 0 | |
| `JWT_SECRET` | a throwaway in development | required everywhere, at least 32 characters outside development |
| `ACCESS_TOKEN_TTL` | 15m | |
| `REFRESH_TOKEN_TTL` | 720h | must be longer than the access lifetime |
| `COOKIE_DOMAIN` | empty | empty means host-only, which is what a single origin wants |
| `COOKIE_SECURE` | false in development | must be true outside development |
| `ALLOWED_ORIGINS` | both spellings of 127.0.0.1:9001 | a list. Every entry needs a scheme and a host and no path. It is both the cors answer and what a write's origin is checked against |
| `FAULT_INJECTION_ENABLED` | false | true outside development is a startup failure |
| `BUILD_VERSION` | 0.1.0, from `config.json.template` | the version the process reports. This is the one committed place the backend version number is written down |
| `BUILD_COMMIT` | empty | left to the build, which works its own out. State it only to hand in a commit no build could find |

Configuration problems are all reported at once rather than one per restart. No
error message ever echoes a connection url, because it carries a password.

**Where the version, the commit, and the build time come from.** Four sources,
asked in order, and the first one that answers wins:

| order | source | who it is for |
| :- | :- | :- |
| 1 | the linker, `-X main.buildVersion` and the other two | a released image, which describes itself and cannot be renamed by the machine it runs on |
| 2 | `BUILD_VERSION` and `BUILD_COMMIT` above | a process run from source, which reads them out of `config.json` |
| 3 | the record the Go toolchain embeds, and the binary's own timestamp | everything else. Neither needs anything written down |
| 4 | the word `unknown` | nobody knew, said plainly rather than left blank |

Source 3 has one catch worth knowing. `go build` records the revision on its own,
and `go run` records it only when asked:

```bash
go run -buildvcs=true ./cmd/api    # names its commit
go run ./cmd/api                   # answers "commit": "unknown"
```

`scripts/debug.sh` passes the flag, so the from-source path names its commit
without anyone remembering to. `internal/config/build_identity_test.go` fails if
that flag is ever dropped.

`scripts/stack_up.sh` stamps the images it builds with this checkout's commit and
the moment it built them, because a build context is a copy of the source without
the repository and has no way to work either out for itself. Set `BUILD_COMMIT` or
`BUILD_TIME` yourself to hand in different values.

The container does not read `config.json`, so `compose.yml` states the same
version. `internal/config/build_identity_test.go` fails when those two drift
apart.

<br>

## Ports

| port | what | state |
| :- | :- | :- |
| 5432 | postgres primary | running |
| 5433 | postgres replica | running |
| 6379 | redis | running |
| 9000 | api | running |
| 9002 | worker metrics | running |
| 9003 | prometheus | running |
| 9004 | grafana | running |
| 9005 | node_exporter | running |
| 9006 | cadvisor | running, and allowed to fail |

Nothing binds to a public address.

<br>

## Every Route

Everything the api serves on 9000. The constants are in
`internal/httpx/paths.go`, `internal/auth/handler.go`, and
`internal/operations/health.go`, so a route renamed there and forgotten here is a
route the frontend cannot call.

Operations. No token, unversioned, and they never move, because a probe and a
scrape are hard coded by whatever runs the process.

| route | answers |
| :- | :- |
| `GET /healthz` | is the process alive. Touches nothing |
| `GET /readyz` | should it get traffic. Probes the dependencies, see The Api below |
| `GET /version` | which build is running: the version, the commit, when it was built, and the Go runtime. There is no `/status` here, that screen is the client's |
| `GET /metrics` | what Prometheus scrapes |

**Why the `z`.** It is a naming convention, not an abbreviation. Diagnostic
routes have carried a trailing `z` since Google's internal status pages
(`/varz`, `/statusz`, `/rpcz`), and the letter was chosen because no real
resource path was likely to end in one. So `/healthz` cannot ever collide with a
resource named health, and anybody reading it knows on sight that it is a probe
rather than part of the api contract. Kubernetes made the spelling standard,
which is why it is used here even though nothing in this repository runs on
Kubernetes.

Kubernetes has since split its own api server's liveness route into `/livez` and
deprecated `/healthz` there. `/healthz` is kept in this api because it is still
what everything outside Kubernetes probes by default, and this api is not run by
one.

**Why two of them, not one.** They answer different questions, and the answers
lead to opposite actions:

| route | question | what a failure means |
| :- | :- | :- |
| `/healthz` | is the process alive | restart it |
| `/readyz` | should it get traffic | route around it, do not restart |

That is why `/healthz` touches no dependency. An api whose database is down is
still alive, and restarting it will not bring the database back. Wire a
dependency into liveness and one slow database turns into every instance being
killed at once.

Further reading:

| what | where |
| :- | :- |
| the endpoints, as Kubernetes defines them | https://kubernetes.io/docs/reference/using-api/health-checks/ |
| what each probe does on failure | https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/ |
| the wider family, `/statusz` and `/flagz` | https://kubernetes.io/docs/reference/instrumentation/zpages/ |
| `/varz` in use at Google, where the convention began | https://sre.google/sre-book/practical-alerting/ |

Sign in. Login and refresh carry no token, because they are how one is obtained,
so the origin check is what guards them. Logout needs both, and `me` needs the
token alone.

| route | does |
| :- | :- |
| `POST /api/v1/auth/login` | checks the email and password, sets both cookies, answers 204 |
| `POST /api/v1/auth/refresh` | spends the refresh token, sets a new pair, answers 204 |
| `POST /api/v1/auth/logout` | revokes the family and clears both cookies |
| `GET /api/v1/auth/me` | the parent id, display name, role, and children |

Parent reads. Access token, and the read rate limit.

| route | answers |
| :- | :- |
| `GET /api/v1/students` | the children on the account |
| `GET /api/v1/classes` | what is on offer, with advisory seat counts |
| `GET /api/v1/classes/{classId}` | one class |
| `GET /api/v1/bookings` | every booking on your account, newest first, capped at 50 |
| `GET /api/v1/bookings/{bookingId}` | one booking, if it is yours |
| `GET /api/v1/bookings/{bookingId}/events` | what happened to it, in order |

The list takes no parameters. Whose bookings it answers comes from the token, so
there is no identifier to send and none to get wrong. See ADR-056.

Every booking body names the class it is for, in the same words the class list
uses:

| field | is |
| :- | :- |
| `class_subject` | the subject, lowercase, as the class list sends it |
| `class_title` | the class name |
| `class_starts_at` | RFC 3339, or null when the class could not be read |

Nothing about the parent or the child is added, and nothing that moves is
either: no capacity, no seats remaining. A booking is a record of what happened,
and a count on it would be a second place to read one from.

The class is read from the replica, alongside the booking rather than inside the
same query. A read that decides nothing does not get to fail a booking, so a
catalogue that cannot be reached leaves the three fields empty and the booking is
still answered.

Parent writes. Access token, the origin check, and the write rate limit. The two
that move money or a seat also need an `Idempotency-Key`.

| route | does | needs a key |
| :- | :- | :- |
| `POST /api/v1/bookings` | asks for a hold on a seat | yes |
| `POST /api/v1/bookings/{bookingId}/payments` | pays for a held booking | yes |
| `DELETE /api/v1/bookings/{bookingId}` | releases a hold | no |
| `POST /api/v1/telemetry` | what the client saw. A write in every sense that matters | no |

Admin only. A parent role gets 403 `forbidden_role` before a body is built.

| route | answers |
| :- | :- |
| `GET /api/v1/classes/{classId}/roster` | who holds a seat. The only body that puts a child's name next to one |
| `GET /api/v1/admin/queue` | what is waiting, held, and stopped |
| `GET /api/v1/admin/bookings` | the worklist, filtered by status |

Development only. Registered when `APP_ENV` is `development` and
`FAULT_INJECTION_ENABLED` is `true`, and admin only even then. Otherwise the
paths answer 404, which is the surface being off rather than a missing route.

| route | does |
| :- | :- |
| `GET /dev/faults` | what can be armed, and what is armed now |
| `POST /dev/faults` | arms one, for a bounded time |
| `DELETE /dev/faults` | disarms everything. Always safe |

<br>

## The Api

```sh
export APP_ENV=development

../scripts/stack_up.sh backend    # the databases and redis have to be up first
scripts/migrate.sh
scripts/seed.sh

go run -buildvcs=true ./cmd/api   # or it comes up with the stack, as a container
```

`scripts/debug.sh` does all four of those in one command, and stops the
containers it started when it ends.

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

`/version` answers the build. Both it and `/readyz` are also drawn on the
client's own screen at `http://127.0.0.1:9001/status`, next to the client's
build, because the interesting case is the two disagreeing. There is no
`/status` on this api: typing `127.0.0.1:9000/status` gets a 404, and the four
routes above are the whole unversioned surface.

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
    -d '{"email":"alice.tan@example.test","password":"otto123"}' \
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

# every booking on this account, which is how a parent finds one again
curl -s -b jar.txt 127.0.0.1:9000/api/v1/bookings | jq
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
    -d '{"email":"ops.admin@example.test","password":"otto123"}' \
    127.0.0.1:9000/api/v1/auth/login

curl -s -b admin.txt 127.0.0.1:9000/api/v1/admin/queue | jq
curl -s -b admin.txt '127.0.0.1:9000/api/v1/admin/bookings?status=refund_required' | jq
curl -s -b admin.txt 127.0.0.1:9000/api/v1/classes/<a class id>/roster | jq
```

The roster is the only body in this api that puts a child's name next to a seat.
A parent role reaching any of the three gets 403 `forbidden_role` before a name
is read.

<br>

## Monitoring

Prometheus is on 9003 and Grafana on 9004. Both start with the stack and both
are provisioned from files, so there is nothing to click before the dashboards
draw.

Both start with `scripts/debug.sh` as well, so the dashboards are live while the
process being demonstrated is the one running from source. Prometheus carries two
targets for the api and two for the worker: `api:9000` for the container and
`host.containers.internal:9000` for this machine, both labelled `component: api`.
Only one of a pair can answer, because `debug.sh` refuses to start while the
container holds the port, so the panels fill in either way and nothing is counted
twice.

The `NotReady` alert reads the component rather than one target, for the same
reason. Running from source stops a container on purpose, and an alert that read
`up{job="api"}` alone would call that an outage.

cAdvisor is left out of that path on purpose: it reports per container, a process
run from source is not one, and it is the one service allowed to fail, so
starting it there could end the session before the process ever ran.

```sh
# what the api is publishing right now
curl -s 127.0.0.1:9000/metrics | grep -E '^(confirm_transaction|access_denied|refund_pending)'

# what the worker is publishing
curl -s 127.0.0.1:9002/metrics | grep -E '^(queue_depth|worker_jobs)'

# whether Prometheus is actually scraping all of them
curl -s 127.0.0.1:9003/api/v1/targets | jq '.data.activeTargets[] | {job: .labels.job, health}'

# one series, queried the way a panel queries it
curl -s --get 127.0.0.1:9003/api/v1/query \
    --data-urlencode 'query=confirm_transaction_total{outcome="error"}' | jq
```

Grafana opens on `http://127.0.0.1:9004` and asks for a sign in. It is Grafana's
own default, `admin` with `admin`, from `GRAFANA_USER` and `GRAFANA_PASSWORD` in
`compose.yml`. Export either one before starting the stack to change it.

The three dashboards are `backend.json`, `frontend.json`, and `resources.json`.
The first opens with the two numbers somebody has to act on, money owed to a
parent and work that failed, then the fault injection banner, then resources,
then the two failure groups the brief calls out. The last is the host wide
fallback for a machine where cAdvisor will not run.

**No panel reads `No data` on a working stack.** Every counter carrying a label
is created at zero for each value it can take, so a group nobody has hit yet
draws a flat line rather than nothing. That distinction is the whole point: a
counter with no series and a counter saying nothing has happened look identical
on a panel, and only one of them is news. A percentile is the one number that
still needs traffic, because there is no 95th of zero requests, so the two panels
carrying one draw an average beside it that is a number from the first scrape.

**A stack whose schema was never applied is the exception, and it says so.** The
worker cannot ask an empty database how deep the queue is, so `Queue depth` draws
nothing and the `read failed` line beside it climbs, which is the panel telling
you why it is empty. `QueueUnreadable` fires on the same counter after five
minutes and names the fix. Run `backend/scripts/migrate.sh` and both recover on
the next scrape.

**The cAdvisor note, stated rather than discovered.** cAdvisor reads a container
runtime socket and is written against Docker. Under rootless Podman it needs the
podman socket running and mounted where it expects one. What the per container
panels need beyond a running cAdvisor is in Per Container Panels below.

```sh
# start the socket, once per session
systemctl --user start podman.socket

# point compose at it, if it is not at the default path
export CONTAINER_SOCKET="${XDG_RUNTIME_DIR}/podman/podman.sock"
export CONTAINER_STORAGE_DIR="${HOME}/.local/share/containers"

../scripts/stack_up.sh backend
```

If it still will not start, leave it. Layers one and two cover cpu, memory, and
drive, the `Containers` row of the backend dashboard goes blank, and the
`resources.json` dashboard answers the same questions host wide. A Go test
asserts that the remaining panels still resolve, so this degradation is a
designed state rather than a discovery.

**Per container panels.** A running cAdvisor is not enough for them. They select
on `name=~"ottodot-.+"`, and cAdvisor writes that label only when two mounts are
both in place. `compose.yml` has them:

| mount | what it answers |
| :- | :- |
| the runtime socket, at `/var/run/docker.sock` and at `/var/run/podman/podman.sock` | which container a cgroup belongs to. cAdvisor looks for each runtime at a fixed path, and with neither answering, a series carries no name for the panels to select on |
| the host `/sys/fs/cgroup`, named on its own line | what the numbers are. A container is given its own cgroup namespace, and the mount for it lands on top of `/sys/fs/cgroup` even when `/sys` comes from the host, so without the host path it reads one cgroup, its own |

Both are plain volumes, so both runtimes apply them. What it looks like when it
is right:

```sh
# every container of the stack, by name. Nothing at all when a mount is missing
curl --silent http://127.0.0.1:9006/metrics | rg --only-matching 'name="[^"]+"' | sort -u
```

**Checking the alert rules without waiting for one to fire.**

```sh
# the rules replay against a synthetic series, in about a second
docker run --rm -v "$PWD/containers/prometheus:/work:ro,z" -w /work \
    --entrypoint promtool docker.io/prom/prometheus:v3.1.0 test rules rules_test.yml
```

That covers `TransactionErrorSpike` firing on a broken transaction, not firing on
twenty lost races, and `RefundBacklog` and `RefreshReuse` firing when they
should. The Go test in `internal/observability` covers the other half: that every
metric name in every dashboard and every rule is one something actually
publishes.

<br>

## Breaking It On Purpose

The fault surface exists so the error metrics and the alerts on them can be
watched moving. It is off unless two things are both true: `APP_ENV` is
`development` and `FAULT_INJECTION_ENABLED` is `true`. With either missing the
routes are never registered at all, so `/dev/faults` answers a plain 404 rather
than a refusal that would confirm it exists.

**Development only, and refused twice.** This is a route that breaks a real
transaction on request, so it is never set on a deployed instance. Both guards
can be watched refusing:

| set | what happens |
| :- | :- |
| `APP_ENV=development`, the flag unset | the api starts normally and `/dev/faults` answers `404 page not found` |
| the flag true, `APP_ENV` anything else | the api refuses to start, exits 1, and never binds a port |

The second refusal reads:

```
the configuration was refused: configuration is not usable:
FAULT_INJECTION_ENABLED is true while APP_ENV is "production", fault injection runs only in development
```

Watching that refusal from this directory needs one thing said first: `APP_ENV`
is stated in `config.json` and a value in that file wins over the environment, so
exporting `APP_ENV=production` in a shell changes nothing here. Run the built
binary from a directory holding no `config.json` to see it, or delete the line.

```sh
export APP_ENV=development
export FAULT_INJECTION_ENABLED=true

go run -buildvcs=true ./cmd/api
```

Compose reads the same variable, so the containerised api comes up with the
surface when the shell that started the stack asked for it:

```sh
export APP_ENV=development
export FAULT_INJECTION_ENABLED=true

../scripts/stack_up.sh backend
```

Everything below, in one command that asserts each step and disarms on the way
out whatever happened: `../scripts/smoke_failure.sh`. The manual version is here
because a reviewer should be able to see what that script is doing.

It is behind the admin role and the write rate limit, exactly like every other
mutation, so it needs the admin cookie from The Operator Routes above.

```sh
# what can be armed, and what is armed right now
curl -s -b admin.txt 127.0.0.1:9000/dev/faults | jq

# break the next confirm transaction, once, for sixty seconds
curl -s -b admin.txt -H 'Origin: http://127.0.0.1:9001' \
    -H 'Content-Type: application/json' \
    -d '{"point":"confirm.before_commit","count":1,"ttl_seconds":60}' \
    127.0.0.1:9000/dev/faults | jq

# now pay for a held booking. It answers 500 internal_error with a request id,
# the seat is not consumed, and the booking is still pending_payment

# the reason it broke is in the api log, under that same request id
docker logs ottodot-api | grep 'internal error'

# the counter moved
curl -s 127.0.0.1:9000/metrics | grep 'confirm_transaction_total{outcome="error"}'

# always safe, whatever state anything is in
curl -s -b admin.txt -X DELETE -H 'Origin: http://127.0.0.1:9001' \
    127.0.0.1:9000/dev/faults | jq
```

| point | what it simulates | what the parent sees |
| :- | :- | :- |
| `confirm.before_commit` | the database dying mid-transaction | 500 `internal_error`, seat not consumed, booking still holding |
| `confirm.lock_wait` | a lock wait timeout under contention | 503 `dependency_unavailable`, worth retrying |
| `payment.provider_error` | the provider unreachable, which is not a decline | 503 `dependency_unavailable`, nothing written about the booking |
| `queue.job_error` | a job blowing up | nothing. The job retries and then parks |
| `cache.redis_error` | Redis gone | nothing. The request is served from Postgres |

An arming spends itself after its count and expires after its lifetime, so a
forgotten fault cannot leave a stack broken. `count` is capped at ten and
`ttl_seconds` at six hundred. Restarting the api clears everything, because the
registry is per process and holds nothing durable.

While the surface is live, `fault_injection_enabled` reads one and the backend
dashboard opens with a banner saying so.

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

../scripts/stack_up.sh backend      # the databases have to be up first
go run -buildvcs=true ./cmd/worker  # or it comes up with the stack, as a container

curl -s 127.0.0.1:9002/healthz
curl -s 127.0.0.1:9002/metrics
```

`scripts/debug.sh worker` does the first two in one command.

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

Sign in takes an email and a password. All four seeded accounts share the
password `otto123`, which is a development convenience and is written down here
on purpose. Everything below works against a running stack.

| email | role |
| :- | :- |
| alice.tan@example.test | parent, three children |
| budi.santoso@example.test | parent, two children |
| chandra.wijaya@example.test | parent, two children |
| ops.admin@example.test | admin |

The stored value is an argon2id hash, in `parents.password_hash`, and the schema
refuses a row whose value is not one. `0002_seed.sql` carries the hash written
out, because postgres has no argon2 of its own.

An unknown address and a wrong password are the same refusal, `invalid_request`,
and they take the same time to answer. Both are deliberate: anything else turns
this route into a way to find out who has an account here.

The four routes are listed under Every Route above. What matters here is the
shape of the exchange: login starts a token family, refresh spends one token and
issues the next, and logout revokes the whole family at once.

Nothing answers with a token in its body. The cookies are the answer, and they
are HttpOnly, so there is nothing for a client to read.

Three things a caller has to get right, because each one is a refusal that looks
like a bug from the outside:

| requirement | why | what happens without it |
| :- | :- | :- |
| an `Origin` header on every write, listed in `ALLOWED_ORIGINS` | cookies travel by themselves, so this is the csrf check | 400 `invalid_request` |
| cookies carried between calls | the session is entirely in them | 401 `token_invalid` |
| the refresh cookie's path | it is scoped to `/api/v1/auth`, see ADR-030 | a hand-built request to a business route will not carry it, which is the point |

An unknown email answers 400 `invalid_request`, the same as a wrong password and
the same as a malformed body. That is deliberate: an endpoint that answers
differently for a known address is an endpoint that lists who has an account
here. See ADR-032.

A browser needs one thing more. The client is served from another port, so every
call it makes is cross origin, and the api answers with the cors headers that let
the page read what came back. Anything not being served from a listed origin gets
no such headers and, in a browser, no answer it can use. curl is unaffected,
which is exactly why a suite of curl calls cannot prove this works.

That includes the headers a request carries. A browser sends only what the
preflight permitted, so a header this api reads and does not name is a route a
browser cannot reach at all:

```sh
curl -s -i -X OPTIONS 127.0.0.1:9000/api/v1/bookings \
    -H 'Origin: http://localhost:9001' \
    -H 'Access-Control-Request-Method: POST' \
    -H 'Access-Control-Request-Headers: content-type,idempotency-key' |
    grep -i access-control-allow-headers
```

The answer names `Content-Type`, `If-None-Match`, and `Idempotency-Key`. Adding a
request header to a handler means adding it to `allowedRequestHeaders` in
`internal/httpx/cors.go` in the same change, or the write silently never leaves
the page. See ADR-055.

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

What tells them apart is `auth_refresh_reuse_detected_total`, which is on the
api's `/metrics` and has an alert on any increase at all. The worker's listener
on 9002 carries the queue numbers only, because the worker holds no auth
service.

<br>

## Common Failures

| symptom | cause | fix |
| :- | :- | :- |
| a script exits 2 mentioning `APP_ENV` | the guard, working | `export APP_ENV=development` |
| `migrate.sh` says the primary is not running | it is not | `../scripts/stack_up.sh backend` |
| `seed.sh` refuses | `parents` already has rows | that is the guard. Use `db_reset.sh` to start over |
| `seed.sh` stops and asks a question | it creates four accounts sharing a written down password, so it asks first | answer `y`, or pass `--generate-demo-users` from a script |
| `seed.sh` exits 2 saying stdin is not a terminal | it was run from a pipe or a workflow, where nobody can answer | pass `--generate-demo-users` |
| the api starts but a browser sees every call fail | the page was opened on an address not in `ALLOWED_ORIGINS` | use `http://127.0.0.1:9001`, or add the address to `config.json` and restart |
| a setting exported in the shell is ignored | `config.json` states it, and the file wins | change it in the file, or delete the line to let the environment decide |
| the replica never leaves recovery | it was started before the primary finished its first boot | stack down, then up. The replica clones on first start |
| `permission denied` under a service `.data/` | the mount shape was changed by hand | remove that directory and start the stack again, the container recreates it |
| `migrate.sh` reports a duplicate key on `schema_migrations` | the container is running on a data directory that was deleted underneath it, usually by a cleanup that could not remove it | `docker rm --force ottodot-postgres-primary`, then start again. `debug.sh` now ends such a container by itself |
| `gofmt` reports permission denied | it was pointed at `.` and walked into `containers/` | use the package directory form above |
| `go test -tags=containers` cannot connect | the stack is not running | `../scripts/stack_up.sh backend` |
| the worker exits naming the configuration | a value in the environment is not usable | it reports every problem at once, fix them together |
| the worker starts and claims nothing | nothing has been booked yet | expected. Book a seat through the api and a release job appears |
| every write answers 400 `invalid_request` | no `Origin` header, or the wrong one | the csrf check, working. See Signing In below |
| a write fails in the browser with `Access-Control-Allow-Headers` in the console, but works with curl | the request carries a header the preflight does not permit | name it in `allowedRequestHeaders` in `internal/httpx/cors.go`. curl never sends a preflight, which is why it is blind to this |
| `127.0.0.1:9000/status` answers 404 | there is no such route here | `/version` is the api's answer. The `/status` screen is the client's, on 9001 |
| a rebuilt api still reports the old version | the image was not rebuilt | `../scripts/stack_up.sh backend` builds. `compose up` on its own starts whatever image already exists |
| a payment answers 400 for the right price | the origin is not in `ALLOWED_ORIGINS`, or the amount drifted from `checkout.TrialPriceCents` | the service owns the price, see ADR-033 |
| `/readyz` answers 503 naming redis | redis is not running | `../scripts/stack_up.sh backend` |
| the api restarts over and over saying redis is not reachable | `protected-mode` was turned back on in `containers/redis/redis.conf` without a password | with it on and no password, Redis answers `DENIED` to everything that is not on its own loopback, which in containers is every client there is. The file says what is holding instead |
| `/dev/faults` answers 404 | `FAULT_INJECTION_ENABLED` is not `true`, or `APP_ENV` is not `development` | that is the surface being off rather than a missing route. See Breaking It On Purpose |
| `debug.sh` refuses, naming `ottodot-api` or `ottodot-worker` | the containerised twin of the process is running and holds its port | stop that one container, the message carries the command. The databases can stay up |
| `debug.sh` ends and the databases are gone | it stopped what it started, which is the default | `scripts/debug.sh --keep` when the same databases are wanted across many restarts |
| the `Containers` panels are blank while running from source | cAdvisor reports per container, and a process run from source is not one | nothing to fix. The `resources.json` dashboard answers the same questions host wide |
| `debug.sh` says it stopped `ottodot-api` again right after starting Prometheus | podman-compose accepts `--no-deps` and starts the dependencies anyway, and Prometheus declares `depends_on` the api and the worker | nothing to fix, that line is the correction working. Without it the container would hold 9000 and the process from source could not bind |
| `/version` answers `"commit": "unknown"` from source | `go run` records the revision only when asked | `go run -buildvcs=true ./cmd/api`, which is what `scripts/debug.sh` already passes |

<br>

## What Is Not Here Yet

Nothing in this stack. Every phase on the backend is finished, including the two
test scripts and the end to end failure run, which is
`../scripts/smoke_failure.sh` at the root because it drives the api over http
rather than through Go.

What remains for the repository as a whole is the five sections of
`../AI_USAGE.md` that are a first-hand account, and the `Time Spent` section of
the root `../README.md`.

Progress is tracked in `phase-track.md`.

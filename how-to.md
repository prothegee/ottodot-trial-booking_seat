# How To Run This Repository

Everything that spans both stacks. Each stack also has its own `how-to.md` with
the detail that belongs to it alone:

| document | scope |
| :- | :- |
| `backend/how-to.md` | Go, the database, migrations, seeding, the test tiers |
| `frontend/how-to.md` | npm, the dev server, the build, the container |

<br>

## What Is Needed

| tool | why | note |
| :- | :- | :- |
| Docker or Podman with compose | the containers | either works, nothing here is runtime specific |
| Go 1.26 or newer | the backend | only for the backend stack |
| Node 24 or newer | the frontend | only for the frontend stack |
| bash | the scripts | every script is bash, none of them are interactive beyond a y/N prompt |

No Postgres client is needed on the host. Every database statement goes through
psql inside the primary container, deliberately, so there is no second path that
can drift.

<br>

## The One Environment Variable

```sh
export APP_ENV=development
```

Every script that can destroy something refuses to run without it, and treats
unset as a refusal rather than a default. This is not a formality: `db_reset.sh`
and `clean.sh` remove local state, and an accidental run in the wrong shell is
exactly what the guard exists to prevent.

<br>

## Starting And Stopping

```sh
scripts/stack_up.sh                # both stacks
scripts/stack_up.sh backend        # postgres primary, postgres replica, redis
scripts/stack_up.sh frontend       # the client behind Caddy

scripts/stack_down.sh              # both, local state left alone
scripts/stack_down.sh backend
scripts/stack_down.sh frontend
```

`stack_up.sh` does not return until each service answers: the primary accepts
connections, the replica reports that it is in recovery, and the client answers
on its port. A silent failure that only shows up later is worse than a slow
start.

The two stacks share nothing. They have separate compose files, separate
container networks, and no link between them. That is on purpose: the client
must send its own origin to the api, because the api checks it on mutations.

<br>

## Ports

| port | what | reachable at |
| :- | :- | :- |
| 5432 | postgres primary | 127.0.0.1 |
| 5433 | postgres replica | 127.0.0.1 |
| 6379 | redis | 127.0.0.1 |
| 9000 | backend api | 127.0.0.1 |
| 9001 | frontend | 127.0.0.1 |
| 9002 | worker metrics | running |
| 9003 | prometheus | running |
| 9004 | grafana | running |
| 9005 | node_exporter | running |
| 9006 | cadvisor | running, and the one service allowed to fail |

Containers keep their own default ports, and a second instance takes the next
number up. That is why the replica is on 5433.

Nothing binds to a public address. Set `FRONTEND_HOST=0.0.0.0` to reach the
client from another machine.

<br>

## First Run, In Order

```sh
export APP_ENV=development

scripts/stack_up.sh backend
backend/scripts/migrate.sh
backend/scripts/seed.sh

cd backend && go test ./...
```

Then the client, in a second shell:

```sh
cd frontend && npm install
npm test
```

<br>

## Local State

Container state lives in bind mounts under `backend/.data/`, one directory per
container, and the whole tree is gitignored. The frontend holds no state, so it
has none.

Each container mounts the parent of its data directory and creates the real one
inside as its own user. That is the only shape that works unchanged under both
rootful Docker and rootless Podman, where a container user maps to a different
host user in each case.

A wipe is therefore one visible path rather than a volume name to look up.

<br>

## Scripts That Can Destroy Something

| script | removes | guards |
| :- | :- | :- |
| `backend/scripts/db_reset.sh` | the whole `public` schema, then migrates and seeds again | `APP_ENV=development`, a named manifest, `y/N` defaulting to No |
| `frontend/scripts/clean.sh` | build output and local caches | the same three |

All of them behave the same way, because they all source
`scripts/lib/confirm.sh`:

| behaviour | detail |
| :- | :- |
| `APP_ENV` not `development` | refuses, exit 2. Unset counts as a refusal |
| every target is named | a script that cannot name what it will remove refuses to run |
| `--dry-run` | prints the manifest and stops, touching nothing |
| not a terminal | refuses without `--yes`, so a pipe cannot answer for you |
| the prompt | the manifest first, then `y/N`, defaulting to No |

Exit codes: 0 done, 1 declined, 2 refused by a guard.

<br>

## When Something Refuses To Start

| symptom | cause | fix |
| :- | :- | :- |
| a script exits 2 saying `APP_ENV` | the guard, working | `export APP_ENV=development` |
| the primary never accepts connections | a port already taken, or a half-written data directory | check 5432, then `scripts/stack_down.sh backend` and start again |
| the replica never leaves recovery | it was started before the primary finished its first boot | `scripts/stack_down.sh backend`, then up again. The replica clones on first start |
| `permission denied` under `backend/.data` | the mount shape was changed by hand | remove the affected directory and start the stack again, it is recreated by the container |
| the frontend answers on 9001 but not on 127.0.0.1 | the default host resolved to IPv6 only | the config binds 127.0.0.1 explicitly, so this means an override is set. Check `FRONTEND_HOST` |
| `go test -tags=containers` cannot reach the primary | the backend stack is not running | `scripts/stack_up.sh backend` |

<br>

## Monitoring

`scripts/stack_up.sh backend` starts the monitoring alongside the service, because
every scrape target is a backend surface.

| open | what |
| :- | :- |
| `http://127.0.0.1:9004` | Grafana, anonymous viewer access, three provisioned dashboards |
| `http://127.0.0.1:9003` | Prometheus, its targets and its rules |

Nothing has to be configured first. The data source and the dashboards are files
in `backend/containers/grafana/`, so they load on every start and a panel change
is a diff rather than a click somebody made once.

One service is allowed to fail. cAdvisor reads a container runtime socket and is
written against Docker, so under rootless Podman it needs `podman system service`
running with its socket mounted where cAdvisor expects one.
`backend/how-to.md` carries the exact command. If it will not start, the per
container rows go blank and the host wide dashboard answers the same questions.

<br>

## What Is Not Here Yet

The continuous integration workflows and the end to end smoke script are the last
phase. Everything else is there now. The api serves on 9000 and the worker has no public
surface at all: its listener on 9002 carries liveness and its metrics, and the
api is what fills the queue, so a worker on a stack nobody has booked on finds an
empty queue and says so.

Signing in from a browser works today. The client on 9001 and the api on 9000 are
different origins, so `FRONTEND_ORIGIN` on the api has to name exactly what the
client is served from, or every write is refused as `invalid_request`. That is
the csrf check doing its job rather than a misconfiguration to work around.

Progress per stack is in `backend/phase-track.md` and
`frontend/phase-track.md`.

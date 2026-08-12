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
| curl | the demonstration scripts | already present on most machines. Nothing here needs a json parser |

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
scripts/stack_up.sh backend        # the databases, redis, the api, the worker, the monitoring
scripts/stack_up.sh frontend       # the client behind nginx

scripts/stack_down.sh              # both, local state left alone
scripts/stack_down.sh backend
scripts/stack_down.sh frontend

scripts/stack_restart.sh           # both, a stop and a start in one command
scripts/stack_restart.sh backend
scripts/stack_restart.sh frontend

scripts/stack_status.sh            # what is up, and what the test runs still need
scripts/stack_status.sh backend
scripts/stack_status.sh frontend
```

`stack_status.sh` only reads, so it is safe at any moment, including while
something is still starting. It exists because every container can be up while
the tests still cannot run, so it reports three more things underneath: whether
the schema is applied, whether the seed is there, and whether the api carries
the fault surface test 16 needs.

```
backend
  ottodot-postgres-primary   up       healthy
  ...
  ottodot-cadvisor           absent   optional, never required
  8 of 9 up

what the test runs need
  the schema               applied
  the seed                 4 parent(s)
  the fault surface        off, test 16 needs it on
```

It exits 0 when every required container is up and 1 when one is not, so it can
gate a command as well as answer a question. cAdvisor never counts.

`stack_restart.sh` is for a change a running container cannot pick up: an edited
`compose.yml`, a rebuilt image, or an environment variable compose only reads
when a container starts, `FAULT_INJECTION_ENABLED` among them. It calls the two
scripts above and does nothing else, so it cannot drift from either, and it
touches no data directory.

`stack_up.sh` does not return until each service answers: the primary accepts
connections, the replica reports that it is in recovery, and the client answers
on its port. A silent failure that only shows up later is worse than a slow
start.

The two stacks share nothing. They have separate compose files, separate
container networks, and no link between them. That is on purpose: the client
must send its own origin to the api, because the api checks it on mutations.

<br>

## Running Something By Hand

Everything above runs built images. To run a process from the source instead,
each stack has a debug script:

```sh
backend/scripts/debug.sh            # the api on 9000, from source
backend/scripts/debug.sh worker     # the worker on 9002, second terminal
frontend/scripts/debug.sh           # the dev server on 9001
```

The backend one starts postgres primary, postgres replica, and redis when they
are not already up, migrates, seeds an empty database, then runs the process in
the foreground. On ctrl-c it stops only what it started, so a stack somebody
else is using survives. `--keep` leaves even those running.

The frontend one starts nothing. This stack owns one container, the served
bundle on 9001, and the dev server is what replaces it, so there is nothing here
to start. It checks, installs `node_modules` when they are missing, and hands
over to `frontend/scripts/dev.sh`.

Neither of them will start the container it replaces, because that container
holds the port. Both refuse with the command to stop it.

<br>

## Ports

| port | what | stack |
| :- | :- | :- |
| 5432 | postgres primary | backend |
| 5433 | postgres replica | backend |
| 6379 | redis | backend |
| 9000 | the api | backend |
| 9001 | the client | frontend |
| 9002 | worker metrics | backend |
| 9003 | prometheus | backend |
| 9004 | grafana | backend |
| 9005 | node_exporter | backend |
| 9006 | cadvisor | backend, and the one service allowed to fail |

Every one of them is on 127.0.0.1. Containers keep their own default ports, and a
second instance takes the next number up. That is why the replica is on 5433.

Nothing binds to a public address. Set `FRONTEND_HOST=0.0.0.0` to reach the
client from another machine.

<br>

## Where The Urls Are

Three lists, each kept next to the thing it describes rather than copied here.

| what | listed in |
| :- | :- |
| every api route on 9000, and what guards each one | `backend/how-to.md`, Every Route |
| the same routes by group, for a first read | `backend/README.md`, Routes |
| every screen the client serves on 9001 | `frontend/README.md`, Screens |
| the same screens with their purpose and state | `frontend/HLD.md`, Routes |
| the monitoring pages on 9003 and 9004 | Monitoring, below |

In code, the api routes are constants in `backend/internal/httpx/paths.go`,
`backend/internal/auth/handler.go`, and
`backend/internal/operations/health.go`. The client names the same paths in
`frontend/src/lib/api/client.ts`, and its own screens are the directories under
`frontend/src/routes/`.

<br>

## Settings

Each stack reads one settings file, and neither file is committed:

| stack | file | made from |
| :- | :- | :- |
| backend | `backend/config.json` | `backend/config.json.template` |
| frontend | `frontend/.env` | `frontend/.env.template` |

Nothing has to be created by hand. `backend/scripts/debug.sh`,
`frontend/scripts/debug.sh`, and `frontend/scripts/build.sh` each check for the
file they need, say plainly that it is missing, copy the template into place, and
carry on.

An existing file is never overwritten, not even to add a setting the template
grew later, because nothing can tell a stale file from a deliberately edited one.
A missing file with no template beside it is a broken clone rather than a missing
setting, so the script refuses and says both paths.

Two rules are worth knowing before changing anything:

**The file wins.** A value stated in `config.json` or `.env` beats an environment
variable of the same name. What is read in the file is what the process is
running with. To let the environment decide a setting, delete its line.

**The frontend file is read at build time.** Vite bakes those values into the
bundle, so changing one needs a rebuild rather than a restart.

The settings worth naming:

| setting | where | what |
| :- | :- | :- |
| `ALLOWED_ORIGINS` | backend | every origin the client may be served from. A list, because a browser treats `127.0.0.1` and `localhost` as different origins |
| `JWT_SECRET` | backend | the signing key. Throwaway in development, refused outside it below 32 characters |
| `WORKER_COUNT` | backend | how many jobs one worker process runs at once. `0` means one per processor |
| `API_BASE_URL` | frontend | where the client sends every call. Has to be spelled the same way the api lists it |

The containers do not read `config.json`. They are configured by
`backend/compose.yml`, because a container reaches postgres and redis by service
name on its own network while the file names addresses on this machine. The
settings that are the same either way, the signing key, the origin list, and the
worker count, are named in both places.

So `scripts/stack_up.sh` needs neither file and never makes one. Every container
setting has a development default written into the compose file, which is why a
fresh clone can bring the whole stack up before reading any of this.

<br>

## Signing In

There are four seeded accounts, and all four share one password:

| email | role | children |
| :- | :- | :- |
| `alice.tan@example.test` | parent | Mira Tan, Nico Tan, Hadi Tan |
| `budi.santoso@example.test` | parent | Dewi Santoso, Fajar Santoso |
| `chandra.wijaya@example.test` | parent | Eka Wijaya, Gita Wijaya |
| `ops.admin@example.test` | admin | none |

The password is `otto123`.

It is stored as an argon2id hash and never as text, so the database holds no
password even in development. What makes this a development posture is the
sharing and the writing down, not the storage.

Treat any database holding these rows as one that anybody who has read this
repository can sign in to. `backend/scripts/seed.sh` asks before creating them
for exactly that reason, and `--generate-demo-users` is how a script answers.

The header carries the title and, once somebody is signed in, their name and a
`Sign out` control. That control tells the api to revoke the refresh token and
expire both cookies, then empties this client and returns to the form. A parent
who is signed out by the api rather than by choice is sent to the same form,
which is why it needs no link. `/status` is an operations screen reached by
typing its address. To open either one directly:
`http://127.0.0.1:9001/sign-in`, `http://127.0.0.1:9001/status`.

`http://localhost:9001` works the same way. Both names are listed by the api, and
the client points itself at whichever one the page was opened on, because a
browser treats them as different sites and would otherwise throw the session
cookies away the moment sign in succeeded.

<br>

## First Run, In Order

```sh
export APP_ENV=development

scripts/stack_up.sh backend
backend/scripts/migrate.sh
backend/scripts/seed.sh

cd backend && go test ./...
```

`seed.sh` names the four demo accounts and waits for a `y` before creating them.

Then the client, in a second shell:

```sh
cd frontend && npm install
npm test
```

<br>

## Step By Step

Three runs to work through by hand, in this order. Each is a numbered checklist
rather than a description, and each ends with what proves it worked, because a
command that printed something is not the same as a command that did something.

### 1. Debug The Project

```sh
# 1) the guard every script here checks first
export APP_ENV=development

# 2) the databases, redis, the api, the worker, the monitoring
scripts/stack_up.sh backend

# 3) the schema, then the four demo accounts. seed.sh names them and waits
backend/scripts/migrate.sh
backend/scripts/seed.sh

# 4) ask the api about itself. /version is the build, there is no /status here
curl -s 127.0.0.1:9000/healthz
curl -s 127.0.0.1:9000/readyz
curl -s 127.0.0.1:9000/version

# 5) the client, in a second shell
cd frontend && npm install && npm run dev
```

Working when `/readyz` answers `{"status":"ready"}` with `ok` beside
`postgres_primary`, `postgres_replica`, and `redis`, and `http://127.0.0.1:9001`
lists the classes.

One thing is normal here rather than a fault: `seed.sh` refuses once the accounts
exist, and says so.

A stack started like this has no fault surface, which is the right default and is
worth knowing before the next run. Test 16 breaks the confirm transaction on
purpose, and it can only do that against an api that was started with the surface
switched on. Nothing here needs it, so it is off. Run Every Test below says what
to do about a stack already up without it.

To run a process from source instead of from an image, `backend/scripts/debug.sh`
is steps 1 to 3 with the api in the foreground, and `frontend/scripts/debug.sh`
is step 5 with its checks first. When a step refuses, When Something Refuses To
Start below names the cause.

### 2. Run Every Test

One command, from the repository root:

```sh
APP_ENV=development scripts/test_all.sh
```

That is every test in the repository: the backend, the frontend, the guards on
the scripts up here, and then everything that needs a real stack, which is the
proof tier, test 6, and test 16. It starts the containers if they are down and
stops them again if it was the one that started them, which is why it asks for
`APP_ENV=development`. About a minute and a half from cold.

Nothing is left out of it. That is the whole point of the name.

**One state to know about, because it is the common one.** A stack that is
already up and was started without `FAULT_INJECTION_ENABLED=true` cannot run
test 16, and the last step refuses about a minute in and says so:

```
--> test 16
refused: the fault surface is off. The api reads this from its container, so it
must be exported before the stack starts:
```

Nothing is broken when that happens. `test_integration.sh` will not restart
containers it did not start, because doing that to somebody's running demo would
be worse than refusing. Either of these clears it:

```sh
# let the run start its own stack, which switches the surface on for you
scripts/stack_down.sh backend
APP_ENV=development scripts/test_all.sh

# or keep the stack and give it the surface
export APP_ENV=development
export FAULT_INJECTION_ENABLED=true
scripts/stack_restart.sh backend
scripts/test_all.sh
```

Started from nothing, the question never arises, because the run starts the
stack itself with the surface on and takes it down afterwards.

**`export`, not just `=`.** This is the one that wastes an afternoon. A variable
assigned without `export` belongs to the shell that typed it and to nothing else,
so `echo $FAULT_INJECTION_ENABLED` prints `true` while every script, compose, and
the api itself sees nothing at all. The value looks set and is not being passed
anywhere.

```sh
FAULT_INJECTION_ENABLED=true             # echo shows it, no script ever sees it
export FAULT_INJECTION_ENABLED=true      # the api gets it
```

`scripts/stack_status.sh backend` reads the setting out of the running container,
so it answers this in one line rather than by argument.

Pass `--run-integration` when there is no terminal to answer for it, which is
what continuous integration does:

```sh
APP_ENV=development scripts/test_all.sh --run-integration
```

Each stack has a command for itself alone, and neither leaves anything out
either:

```sh
APP_ENV=development backend/scripts/test_all.sh    # starts a stack for the proof tier
frontend/scripts/test_all.sh                       # needs nothing running, ever
```

The backend one starts the stack when it is down, applies the schema, seeds an
empty database, runs the proof tier, and takes the stack down again only if it
was the one that started it. A stack already up is used and left alone. That is
why it wants `APP_ENV=development`, and `--run-integration` when there is no
terminal to answer for it.

For the fast loop while writing code, `backend/scripts/test.sh` is the four fake
tiers on their own and starts nothing at all. About five seconds.

The same set, one piece at a time:

```sh
# 1) the backend, with nothing running
cd backend
scripts/test.sh                                 # build, vet, the four fast tiers
scripts/format.sh --check                       # gofmt, then four space indentation

# 2) the backend proofs, which need the stack from run 1
APP_ENV=development scripts/test_proof.sh

# 3) the frontend, against a fake transport
cd ../frontend
npm test                                        # the four tiers
npm run check                                   # types
npm run build                                   # the static bundle

# 4) the guards on the scripts themselves
cd ..
APP_ENV=development backend/scripts/debug_test.sh
backend/scripts/lib/database_test.sh
frontend/scripts/debug_test.sh
frontend/scripts/clean_test.sh
scripts/cleanup_dev_test.sh
scripts/race_last_seat_test.sh
scripts/smoke_refund_test.sh
scripts/stack_up_test.sh
scripts/stack_restart_test.sh
scripts/stack_status_test.sh
scripts/lib/confirm_test.sh
scripts/lib/settings_test.sh
scripts/lib/stack_test.sh
```

Every file in point 4 reads its subject or stops inside a guard, so none of them
starts or removes anything, and whatever they write goes to a throwaway directory
that is deleted on the way out. The one exception runs its subject all the way
through, and only because that subject is `stack_status.sh`, which itself only
reads. They take a second or two together.

Green when no line in any of them reads `FAIL`, every backend package prints
`ok`, and the frontend reports every test file passed. The proof tier is the one
that needs a database, so it is the one that fails first when the stack is down.

For one test rather than all of them, each stack has a `tests-and-diagram.md`:
`backend/tests-and-diagram.md` and `frontend/tests-and-diagram.md`. Every test file is
listed there with what it proves, a diagram for each test, and the command
that runs that file on its own.

Or all of it that needs a real stack, in one command:

```sh
APP_ENV=development scripts/test_integration.sh
```

That starts the containers, applies the schema, runs the proof tier, runs
test 6, runs test 16, and stops the containers again. A stack already up is used
and left up. It is the last step of `scripts/test_all.sh` above, and the only
part of it that touches containers.

Test 6 is given `--fresh-class` here, so it races a throwaway class it makes and
drops rather than the seeded seat. That is what lets this command run twice in a
row.

One case in that reuse fails, and says why. The containers it starts itself get
`FAULT_INJECTION_ENABLED`, but an api already running without it has no fault
surface, so test 16 refuses and the run ends there. Either take the stack down
and let this command start it, or give the running one the surface with
`APP_ENV=development FAULT_INJECTION_ENABLED=true scripts/stack_restart.sh backend`.
Run Every Test above shows both, and it is the same case either way.

### 3. Break It On Purpose And Watch It

```sh
# 1) the fault surface exists only when both of these are set
export APP_ENV=development
export FAULT_INJECTION_ENABLED=true
scripts/stack_up.sh backend

# 2) break one confirm transaction and follow it to the alert
scripts/smoke_failure.sh

# 3) read the same counter by hand
curl -s 127.0.0.1:9000/metrics | grep 'confirm_transaction_total'
```

Step 2 prints eight numbered checks. Step 3 is there because the point of the
whole exercise is that the number is real, so it is worth seeing outside the
script that asserts it.

Compose reads `FAULT_INJECTION_ENABLED` when a container starts, so a stack that
was already up without it has an api without the surface, and step 2 says so
rather than guessing. `scripts/stack_down.sh backend` and step 1 again is the
fix.

Then watch it arrive, in a browser:

| open | what to look for |
| :- | :- |
| `http://127.0.0.1:9003` | query `confirm_transaction_total{outcome="error"}`, then `ALERTS` naming `TransactionErrorSpike` |
| `http://127.0.0.1:9004` | the backend dashboard, opening on refunds owed and failed jobs, with the fault injection banner armed below them |

Proven when the parent is answered 500 `internal_error` with a request id, the
booking is still `pending_payment` with no seat, and the counter has moved by
exactly one. On a local run Prometheus held the series within seconds and the
alert reached pending shortly after, so a wait of under a minute is expected and
a wait of several is a problem.

**Never on a deployed instance.** The two variables in step 1 are what make a
real transaction breakable on request. Set together outside development the api
refuses to start rather than starting with the surface on, and with
`FAULT_INJECTION_ENABLED` unset the routes are never registered at all.
`backend/how-to.md`, Breaking It On Purpose, states both guards and how to see
them refuse.

<br>

## Showing It Working

Two scripts drive a running system over http, cookies and all, and assert what
came back. Neither reaches past a guard, so what they print is what a parent
would get. A third, `smoke_refund.sh` below, moves the refund backlog in the
database instead, because that number is counted from rows rather than reached
by a request.

```sh
scripts/race_last_seat.sh                       # the brief's scenario, test 6
scripts/race_last_seat.sh --fresh-class         # the same race, on a class of its own
APP_ENV=development scripts/smoke_failure.sh    # break it on purpose, test 16
```

`race_last_seat.sh` is the last-seat race. Two parents hold the same single
seat, the second one pays first and is confirmed, the first one pays and is
refused with `seat_lost`, and four tables are then read back to check they
agree: one seat handed out, two charges settled, an audit line for each parent,
and a refund queued for the one who lost.

The seeded seat can only be raced once, because it is gone the moment somebody
has it. `--fresh-class` is the way to go again: it makes a throwaway class with
one seat, races that instead, and deletes it and every row the race wrote when
it is done, so the seeded data is left exactly as it was. Without the flag and
with the seat already gone, it offers the same thing rather than doing it:

```
the seeded race class already holds 1 confirmed booking(s), so its
seat is gone and there is nothing left to race for.

race a throwaway class instead? [y/N]
```

Answering no leaves everything alone, and `scripts/seed_reset.sh` is the other
way back to a free seeded seat. With no terminal to ask, it refuses and names
the flag.

`smoke_failure.sh` breaks the confirm transaction on purpose and follows the
failure to the alert. It needs the api started with fault injection on:

```sh
export APP_ENV=development
export FAULT_INJECTION_ENABLED=true

scripts/stack_up.sh backend
```

It is classed destructive although it deletes nothing, because it breaks a
running system deliberately, so it prompts like every other destructive script.

`smoke_refund.sh` does the same job for the one alert that costs somebody money.
`refund_pending_bookings` is a gauge the api counts from the database every five
seconds, so the only way to move the `Refunds owed` panel is to move rows:

```sh
APP_ENV=development scripts/smoke_refund.sh --increase          # one more
APP_ENV=development scripts/smoke_refund.sh --increase 5        # five more
APP_ENV=development scripts/smoke_refund.sh --decrease 3        # three fewer
APP_ENV=development scripts/smoke_refund.sh --increase 2 --dry-run
```

It asks which parent first, and only the demo accounts on `example.test` are in
the list to choose from:

```
which demo parent is this refund for?

  1) Alice Tan        alice.tan@example.test         owed 0
  2) Budi Santoso     budi.santoso@example.test      owed 0
  3) Chandra Wijaya   chandra.wijaya@example.test    owed 0

choose [1]:
```

`--increase` writes bookings that say the money moved and the seat did not, each
with the charge that settled behind it and an audit line saying why it is owed.
`--decrease` closes that many again, newest first, so an increase can be undone
exactly and a refund left by a real lost race is last in line. It cannot go
below zero: with nothing owed to that parent it says so and stops.

Then it follows the new number to the api's own gauge and to Prometheus, and
names the panel. `RefundBacklog` fires on anything above zero held for five
minutes, so an increase left standing is how that alert is watched arriving.

Everything that needs a real stack runs in one command:

```sh
APP_ENV=development scripts/test_integration.sh
```

That starts the containers, applies the schema, runs the proof tier against real
Postgres, runs test 16, and stops the containers again. A stack that was
already up is used and left up. It never prompts, and it refuses to run without
`--yes` when there is no terminal, which is how continuous integration calls it.

<br>

## Local State

Container state lives in `backend/containers/<service>/.data`, next to that
service's own configuration, and every one is gitignored. The frontend holds no
state, so it has none.

One volume per service, independent of the others: removing one service's state
cannot take another's with it. The name starts with a dot because the tree sits
inside the Go module, and `go build ./...` walks every directory without one.

Each container mounts the parent of its data directory and creates the real one
inside as its own user, which is the only shape that works unchanged under both
rootful Docker and rootless Podman. See ADR-015 and ADR-057.

<br>

## Scripts That Can Destroy Something

| script | removes | guards |
| :- | :- | :- |
| `scripts/seed_reset.sh` | every data row, then inserts the seed rows again. The schema is left alone | `APP_ENV=development`, a named manifest, `y/N` defaulting to No |
| `scripts/cleanup_dev.sh` | this project's containers, the images it built, both networks, and each service's `.data/` directory | the same three, and every target has to carry the project prefix |
| `scripts/smoke_failure.sh` | nothing, but it breaks a running api on purpose | the same three |
| `scripts/smoke_refund.sh` | nothing on `--increase`. On `--decrease` it closes a booking that is owed a refund, with no money sent back | the same three, and only demo accounts on `example.test` are offered to write against |
| `backend/scripts/db_reset.sh` | the whole `public` schema, then migrates and seeds again | the same three |
| `frontend/scripts/clean.sh` | build output and local caches | the same three |

`cleanup_dev.sh` refuses to delete any state while a container it was asked to
remove is still there, and says which one. Deleting a database out from under a
running container leaves it answering `pg_isready` and failing every query.

It is the widest of these scripts, so it is worth saying what it will not do. It
removes nothing it did not build: Postgres, Redis, Prometheus, Grafana,
node-exporter, and cAdvisor are pulled from a registry and shared with whatever
else on the machine uses them. There is no wildcard, no prune, and no `-a`
anywhere in it, and every name is written out literally. `scripts/cleanup_dev_test.sh`
checks all of that without letting it destroy anything.

That test takes no flags and sets `APP_ENV` itself for every case, one case per
value, so its report is the same whichever shell it was run from. A flag typed at
it refuses with exit 2 rather than running and printing a report that looks like
the flag did something.

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

One script prompts without destroying anything. `backend/scripts/seed.sh` can
only insert into an empty database, and it still asks, because what it creates is
four accounts sharing a password that is written down in this file. It names all
four and waits. Its flag is `--generate-demo-users` rather than `--yes`, because
the flag names what happens rather than what is being waived, and it uses the
same exit codes as the table above.

<br>

## When Something Refuses To Start

`scripts/stack_status.sh` first. It names which container is not up and which of
the three test preconditions is missing, which is most of this table answered in
one line without reading any of it.

| symptom | cause | fix |
| :- | :- | :- |
| a script exits 2 saying `APP_ENV` | the guard, working | `export APP_ENV=development` |
| the primary never accepts connections | a port already taken, or a half-written data directory | check 5432, then `scripts/stack_down.sh backend` and start again |
| the replica never leaves recovery | it was started before the primary finished its first boot | `scripts/stack_down.sh backend`, then up again. The replica clones on first start |
| `permission denied` under a service `.data/` | the mount shape was changed by hand | remove the affected directory and start the stack again, it is recreated by the container |
| the frontend answers on 9001 but not on 127.0.0.1 | the default host resolved to IPv6 only | the config binds 127.0.0.1 explicitly, so this means an override is set. Check `FRONTEND_HOST` |
| `go test -tags=containers` cannot reach the primary | the backend stack is not running | `scripts/stack_up.sh backend` |
| the api restarts over and over saying redis is not reachable | `protected-mode` was turned back on in `backend/containers/redis/redis.conf` without a password | with it on and no password, Redis answers every non-loopback client `DENIED`, and in containers that is every client. The file explains what is holding instead |
| `smoke_failure.sh` says the fault surface is off | the api was started without `FAULT_INJECTION_ENABLED=true`, and most often it was assigned without `export`, so `echo` shows it while no script ever received it | `export FAULT_INJECTION_ENABLED=true`, then `scripts/stack_restart.sh backend`. Compose reads it only when a container starts. `scripts/stack_status.sh backend` says what the api actually got |
| every screen in the browser fails, but curl works | the page was opened on an address the api does not list | use `http://127.0.0.1:9001`, or add the address to `ALLOWED_ORIGINS` in `backend/config.json` and restart the api. `localhost` and `127.0.0.1` are different origins to a browser |
| a setting changed in the shell has no effect | the settings file states it, and the file wins | change it in `backend/config.json` or `frontend/.env`, or delete the line there to let the environment decide |
| a frontend setting changed but the page is unchanged | `.env` is read when the bundle is built, not when it is served | rebuild, or restart the dev server |
| sign in is refused for an address that exists | the password is wrong, or the seed predates the password column | the password is `otto123`. If the seed is older, `scripts/seed_reset.sh` |
| a booking fails in the browser with a cors message naming a header, but the same call works with curl | the api read a request header its preflight did not permit | fixed for `Idempotency-Key`. A new one means naming it in `allowedRequestHeaders` in `backend/internal/httpx/cors.go`, see ADR-055 |
| `127.0.0.1:9000/status` answers 404 | there is no such route on the api | `/version` is the api's answer. The status screen is the client's, at `http://127.0.0.1:9001/status` |
| the status screen says the backend version is unknown | the api was started from an image built before this checkout | `scripts/stack_up.sh backend`, which rebuilds and stamps the images with this commit |

<br>

## Monitoring

`scripts/stack_up.sh backend` starts the monitoring alongside the service, because
every scrape target is a backend surface.

`backend/scripts/debug.sh` starts it too, so a process running from source is as
monitored as a containerised one. Prometheus carries two targets for the api and
two for the worker: one at the compose service name, one at
`host.containers.internal`, both labelled with the same component. Only one of
each pair can answer, because `debug.sh` refuses to start while the container
holds the port, so nothing is counted twice and no panel goes blank when the
process moves to this machine.

The one row that does stay blank is the per container one. cAdvisor reports on
containers and a process running from source is not one, which is why
`debug.sh` says so on the way past. The host wide `resources.json` dashboard
answers the same questions.

`frontend/scripts/debug.sh` starts no monitoring and needs none. Every series on
the frontend dashboard is published by the api: the browser posts what it did,
the api counts it, and Prometheus scrapes the api. A dev server with the backend
stack up is a fully monitored frontend, and that script reports whether Grafana
is answering rather than reaching across into the other stack to start it.

| open | what |
| :- | :- |
| `http://127.0.0.1:9004` | Grafana, three provisioned dashboards, sign in as `admin` with `admin` |
| `http://127.0.0.1:9003` | Prometheus, its targets and its rules |

Nothing has to be configured first. The data source and the dashboards are files
in `backend/containers/grafana/`, so they load on every start and a panel change
is a diff rather than a click somebody made once.

One service is allowed to fail. cAdvisor reads the container runtime socket, so a
machine where no socket answers loses the `Containers` rows and nothing else,
because the host wide dashboard asks the same questions a different way. Per
Container Panels in `backend/how-to.md` names what those rows need.

<br>

## Continuous Integration

Three workflows in `.github/workflows/`, and every step in them is a script from
this repository rather than a command typed into a yaml file. That is why those
scripts exist: what runs in a pull request is the same file a developer runs
locally, so a green run there means the same thing it means here.

| workflow | when | jobs |
| :- | :- | :- |
| `pull-request-backend.yml` | a pull request to `main` | formatting and the four fake tiers, the alert rules through `promtool`, then the proof tier and test 16 against real containers |
| `pull-request-frontend.yml` | the same | types, the four tiers, the static build, and the container image |
| `deploy-simulation.yml` | a push to `main-stable` | builds both stacks from a clean checkout, starts them, waits for each to report ready, and stops them |

`deploy-simulation.yml` deploys nowhere, and the name says so. There is no
environment behind this repository, so what it proves is that the images build,
the compose files describe something that starts, the schema applies, and both
stacks answer on their ports.

Each stack is filtered to its own paths, so a client change never waits on Go
and a backend change never waits on npm. The filter is a job rather than a
trigger, deliberately: a workflow filtered at the trigger does not run at all
for an unrelated pull request, its check never appears, and a check that never
appears blocks a merge forever once branch protection marks it required. A job
skipped by an `if` reports as passed, which is what branch protection needs.

<br>

## What Is Not Here Yet

Nothing in the code. Two things nobody but the developer can write: the five
sections of `AI_USAGE.md` that are a first-hand account, and the `Time Spent`
section of the root `README.md`.

Everything else is there now. The api serves on 9000 and the worker has no public
surface at all: its listener on 9002 carries liveness and its metrics, and the
api is what fills the queue, so a worker on a stack nobody has booked on finds an
empty queue and says so.

Signing in from a browser works today. The client on 9001 and the api on 9000 are
different origins, which costs two things rather than one:

The api sends the cors headers a browser needs before it will hand an answer to
the page. Without them every call fails at the browser, whatever the api replied,
and curl never notices because curl is not a browser.

The origin on a write has to be one of the entries in `ALLOWED_ORIGINS`, spelled
exactly, or it is refused as `invalid_request`. That is the csrf check doing its
job rather than a misconfiguration to work around. `127.0.0.1:9001` and
`localhost:9001` are different origins to a browser, so both are listed by
default and neither is a substitute for the other.

Progress per stack is in `backend/phase-track.md` and
`frontend/phase-track.md`.

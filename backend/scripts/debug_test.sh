#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Checks the guards of debug.sh, and reads its source for the promises its
# header makes.
#
# Nothing here starts a container or runs a process. Every case either stops at
# a guard that fires before any of that, or reads the file. The part that does
# start things is covered by running it, which is what the manual path is for.
#
# Usage:
#   backend/scripts/debug_test.sh
#
# Return:
# - 0: every case passed
# - 1: at least one case failed
# - 2: a flag was passed, and this file takes none
# ---------------------------------------------------------------------------- #

set -uo pipefail

# Every case here sets its own conditions, so a flag typed at this file would run
# everything and print the same report, which reads as though the flag did
# something.
if [ "$#" -gt 0 ]; then
    printf "refused: unknown flag '%s', this file takes no flags\\n" "$1" >&2

    exit 2
fi

backend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

subject="$backend_root/scripts/debug.sh"
output_file="$(mktemp)"

passed=0
failed=0

cleanup() {
    rm -f "$output_file"
}

trap cleanup EXIT

record_pass() {
    printf '  pass: %s\n' "$1"
    passed=$((passed + 1))
}

record_fail() {
    printf '  FAIL: %s: %s\n' "$1" "$2" >&2
    failed=$((failed + 1))
}

run_script() {
    "$subject" "$@" >"$output_file" 2>&1 </dev/null

    printf '%s\n' "$?"
}

expect_refusal() {
    local name="$1"
    local expected_text="$2"
    local actual_code="$3"

    if [ "$actual_code" != "2" ]; then
        record_fail "$name" "expected exit 2, got $actual_code"

        return 1
    fi

    if ! grep --quiet --fixed-strings "$expected_text" "$output_file"; then
        record_fail "$name" "the message did not mention '$expected_text': $(tr '\n' ' ' <"$output_file")"

        return 1
    fi

    record_pass "$name"
}

# Reads the source with comment lines dropped, so a word this script promises
# never to use cannot pass just because the header explains why it is absent.
matches_in_code() {
    local pattern="$1"

    grep --line-number --extended-regexp "$pattern" "$subject" |
        grep --invert-match --extended-regexp '^[0-9]+:[[:space:]]*#'
}

expect_absent_from_code() {
    local name="$1"
    local pattern="$2"
    local found

    found="$(matches_in_code "$pattern")"

    if [ -n "$found" ]; then
        record_fail "$name" "found: $(printf '%s' "$found" | tr '\n' ' ')"

        return 1
    fi

    record_pass "$name"
}

expect_present_in_code() {
    local name="$1"
    local pattern="$2"

    if [ -z "$(matches_in_code "$pattern")" ]; then
        record_fail "$name" "no line matched '$pattern'"

        return 1
    fi

    record_pass "$name"
}

# Finds where main calls something, reading only main's own body.
#
# The whole file is the wrong place to look. One function can call another at
# the same one indent, so a plain search returns two lines for one name, and an
# ordering question then compares a number against a pair.
call_line_of() {
    awk -v name="$1" '
        /^main\(\) \{/ { inside = 1; next }
        inside && $0 == "    " name { print NR; exit }
    ' "$subject"
}

expect_call_order() {
    local name="$1"
    local first
    local second

    first="$(call_line_of "$2")"
    second="$(call_line_of "$3")"

    if [ -z "$first" ] || [ -z "$second" ]; then
        record_fail "$name" "one of the calls is missing, found '$first' and '$second'"

        return 1
    fi

    if [ "$first" -ge "$second" ]; then
        record_fail "$name" "$2 is on line $first, $3 on line $second"

        return 1
    fi

    record_pass "$name"
}

expect_code_line_count() {
    local name="$1"
    local pattern="$2"
    local expected="$3"
    local actual

    actual="$(matches_in_code "$pattern" | wc --lines)"

    if [ "$actual" != "$expected" ]; then
        record_fail "$name" "expected $expected line(s) matching '$pattern', found $actual"

        return 1
    fi

    record_pass "$name"
}

printf 'debug.sh guards\n'

expect_refusal "an unknown argument is refused" \
    "unknown argument '--force'" \
    "$(run_script --force)"

expect_refusal "a misspelled process name is refused" \
    "unknown argument 'apii'" \
    "$(run_script apii)"

expect_refusal "any environment but development is refused" \
    "APP_ENV is 'production'" \
    "$(APP_ENV=production run_script)"

# The refusal below has to be the APP_ENV one, not an argument one. That is what
# proves both accepted arguments parsed before the environment guard fired, and
# it costs nothing, because neither case reaches a container.
expect_refusal "the worker argument is accepted" \
    "APP_ENV is 'production'" \
    "$(APP_ENV=production run_script worker)"

expect_refusal "the keep flag is accepted" \
    "APP_ENV is 'production'" \
    "$(APP_ENV=production run_script --keep)"

printf 'debug.sh source\n'

expect_absent_from_code "it never removes a container or an image" \
    '(^|[^[:alnum:]_-])(rm|rmi)([^[:alnum:]_-]|$)'

expect_absent_from_code "it never takes the stack down" \
    'compose[^|]*down|stack_down'

expect_absent_from_code "it never prunes and never acts on all" \
    'prune|--all([^[:alnum:]]|$)'

# inspect, exec, and stop are the whole of what this script asks a runtime for.
# A start or a run here would mean it can bring up something it never named.
expect_absent_from_code "the runtime is never asked to start or create anything" \
    '\$container_runtime" (start|run|create)'

expect_present_in_code "the data services it may start are a literal list" \
    'DEBUG_DATA_SERVICES=\("postgres-primary" "postgres-replica" "redis"\)'

expect_present_in_code "the monitoring services it may start are a literal list" \
    'DEBUG_MONITORING_SERVICES=\("node-exporter" "prometheus" "grafana"\)'

expect_present_in_code "the compose call starts the data list" \
    'stack_compose backend up --detach "\$\{DEBUG_DATA_SERVICES\[@\]\}"'

# --no-deps is the whole reason starting Prometheus here is safe. Prometheus
# declares depends_on api and worker, so without it compose would start the
# container this script just refused to run beside, and take the port twice.
expect_present_in_code "the monitoring call refuses to pull in dependencies" \
    'stack_compose backend up --detach --no-deps "\$\{DEBUG_MONITORING_SERVICES\[@\]\}"'

# One call per literal list, plus the one that starts the data list again when a
# container answered and then went away. A fourth would be a way to start
# something these two lists do not name.
expect_code_line_count "there are exactly three compose calls, over two lists" \
    'stack_compose' 3

# cAdvisor is the one service allowed to fail, so starting it here could end the
# session before the process ever ran.
expect_absent_from_code "it never starts cadvisor" \
    'cadvisor'

# --no-deps is asked for and is not enough. podman-compose 1.6.0 accepts it and
# starts the dependencies anyway, which brings back the container this script
# stopped to take the port. Asking the flag to be present proves the request, and
# only the correction below proves the outcome.
expect_call_order "the correction runs after the monitoring start" \
    start_missing_monitoring_services correct_what_compose_raised

expect_present_in_code "the correction stops the twin compose raised" \
    '"\$container_runtime" stop "\$container"'

# It reads what the runtime reports rather than what compose promised, which is
# the only reading that survives a flag being ignored.
expect_present_in_code "the correction asks the runtime, not the flag" \
    'if ! container_is_running "\$container" \|\| was_running_before "\$container"; then'

# The postgres image builds a new database with a temporary server of its own,
# and that one listens on the unix socket alone. A socket probe cannot tell it
# from the real server, so the gate would open while the database was still being
# created and the migration would land in a server about to be replaced.
expect_present_in_code "the primary is proven by a query, over tcp" \
    "command 'select 1'"

# scripts/cleanup_dev.sh can leave a container it failed to remove, running on a
# data directory that was deleted underneath it. Ending it here is what lets the
# next run start it again on a directory that exists.
expect_call_order "a database that cannot answer is dealt with before the wait" \
    start_missing_data_services wait_for_data_services

expect_present_in_code "the database that cannot answer is ended, not removed" \
    '"\$container_runtime" stop ottodot-postgres-primary'

# pg_isready answers yes for a server that is being replaced and for one whose
# files were deleted underneath it. Neither can answer a query.
expect_absent_from_code "nothing here settles for pg_isready" \
    'pg_isready'

# A previous run of this script can still be stopping its containers while this
# one starts, so the stop lands after the gate opened. Reading the containers
# once more is what turns that into a restart rather than a runtime error about
# a container the migration cannot reach.
expect_call_order "the containers are read again after they answered" \
    wait_for_data_services settle_the_data_services

expect_call_order "nothing is asked of the database until that reading is done" \
    settle_the_data_services prepare_the_database

# Refusing after the databases are already up would leave a developer with
# containers they did not ask for and no process to use them.
expect_call_order "the port refusal comes before anything is started" \
    require_the_port_is_free start_missing_data_services

expect_call_order "the environment refusal comes before any container is asked about" \
    require_development require_the_port_is_free

expect_present_in_code "it stops only the names this run collected" \
    'for container in "\$\{started_containers\[@\]\}"'

# stop and not rm, so a ctrl-c ends the containers without destroying them. The
# database, the queue, and the Prometheus history are all bind mounts on a
# container that still exists afterwards.
expect_present_in_code "it ends a container with stop, which keeps it" \
    '"\$container_runtime" stop "\$container"'

expect_absent_from_code "no runtime verb here destroys anything" \
    '\$container_runtime" (rm|rmi|kill|prune|system)'

# The call below ends in the guard at the top of this file, before a single case
# runs, so it costs one process and cannot recurse.
"${BASH_SOURCE[0]}" --dry-run >/dev/null 2>&1 </dev/null
self_code="$?"

if [ "$self_code" = "2" ]; then
    record_pass "a flag passed to this file refuses"
else
    record_fail "a flag passed to this file refuses" "expected exit 2, got $self_code"
fi

printf '\n%s passed, %s failed\n' "$passed" "$failed"

if [ "$failed" -gt 0 ]; then
    exit 1
fi

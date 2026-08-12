#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: library
#
# One way to talk to the running api from a script. Sourced, never executed.
#
# The two demonstration scripts both sign a parent in, hold a seat, and pay for
# it. That is the whole of what lives here: the shape of a request this api
# accepts, in one place, so a change to the origin rule or the cookie names is a
# change to one file rather than a hunt through two.
#
# It talks over http with cookies, exactly as the browser does. No step reaches
# past a guard, because a demonstration that skips the origin check or the
# ownership check is not a demonstration of this service.
#
# Nothing here parses json, and that is deliberate rather than lazy. curl is the
# only tool it needs, so a reviewer runs these scripts with what they already
# have. What a script needs from a response is either the status code, which
# curl reports directly, or a value it can read back from the primary, which is
# the more honest place to read it from anyway.
#
# Usage:
#   source "<repository root>/scripts/lib/api.sh"
#
#   api_require_tools
#   api_require_running
#
#   jar="$(mktemp)"
#   api_sign_in "$jar" "alice.tan@example.test"
#   api_request POST "$jar" /api/v1/bookings \
#       '{"student_id":"...","class_id":"..."}' "Idempotency-Key: demo-1"
#
#   printf 'the api answered %s\n' "$api_status"
#
# Note:
# - API_BASE and API_ORIGIN override where it calls and what origin it claims.
#   The defaults are the two ports this repository runs on
# - the origin has to be one of the entries in ALLOWED_ORIGINS on the api, spelled
#   exactly, or every write is refused. That is the csrf check working, not a
#   misconfiguration
# ---------------------------------------------------------------------------- #

API_BASE="${API_BASE:-http://127.0.0.1:9000}"
API_ORIGIN="${API_ORIGIN:-http://127.0.0.1:9001}"

# The password every seeded account shares. Development only, and stated in
# how-to.md, which is why a script may carry it in plain sight.
API_SEED_PASSWORD="${API_SEED_PASSWORD:-otto123}"

# The http status of the last call, as a string.
api_status=""

# Where the last response body was written. One file, reused, because only the
# last answer is ever of interest.
API_BODY_FILE="${API_BODY_FILE:-}"

# Refuses early when a tool is missing, with the name of the thing to install.
api_require_tools() {
    if ! command -v curl >/dev/null 2>&1; then
        printf 'this script needs curl, install it and run again\n' >&2

        return 1
    fi

    if [ -z "$API_BODY_FILE" ]; then
        API_BODY_FILE="$(mktemp)"
    fi
}

# Removes the response file. Call it from the caller's EXIT trap.
api_cleanup() {
    if [ -n "$API_BODY_FILE" ] && [ -f "$API_BODY_FILE" ]; then
        rm -f "$API_BODY_FILE"
    fi
}

# Refuses when the api is not answering, with the command that starts it.
api_require_running() {
    if curl --silent --fail --output /dev/null "$API_BASE/healthz" 2>/dev/null; then
        return 0
    fi

    printf 'the api is not answering at %s, run scripts/stack_up.sh backend first\n' "$API_BASE" >&2

    return 1
}

# Makes one call and remembers the status and the body.
#
# Param:
# method - string (GET, POST, DELETE)
# jar - string (path to the cookie file this caller's session lives in)
# path - string (the path, starting with a slash)
# body - string (json, or empty for none)
# ... - any extra headers, one string each, for example "Idempotency-Key: demo-1"
#
# Return:
# - the status in api_status, the body readable with api_body
api_request() {
    local method="$1"
    local jar="$2"
    local path="$3"
    local body="$4"

    shift 4

    local arguments=(
        --silent
        --show-error
        --request "$method"
        --cookie "$jar"
        --cookie-jar "$jar"
        --header "Origin: $API_ORIGIN"
        --output "$API_BODY_FILE"
        --write-out '%{http_code}'
    )
    local header

    for header in "$@"; do
        arguments+=(--header "$header")
    done

    if [ -n "$body" ]; then
        arguments+=(--header 'Content-Type: application/json' --data "$body")
    fi

    api_status="$(curl "${arguments[@]}" "$API_BASE$path")"
}

# The whole last response, for a failure message.
api_body() {
    cat "$API_BODY_FILE" 2>/dev/null
}

# Whether the last response contains a literal string.
#
# It is a fixed-string search, never a pattern. The only thing worth asking a
# response body here is whether an exact code this service defines came back,
# and that question needs no parser.
#
# Param:
# literal - string (for example "\"code\":\"seat_lost\"")
api_body_contains() {
    grep --quiet --fixed-strings "$1" "$API_BODY_FILE" 2>/dev/null
}

# Signs a seeded parent in and leaves both cookies in the jar.
#
# The password defaults to the one every seeded account shares, so a script that
# only wants a session says who and nothing else. Pass a third argument to try a
# different one, which is what a case about a refusal needs.
#
# Param:
# jar - string (path to the cookie file, created if absent)
# email - string (one of the seeded addresses)
# password - string (optional, defaults to the seeded password)
#
# Return:
# - 0 when the session is live
# - 1 with the status and the body when it is not
api_sign_in() {
    local jar="$1"
    local email="$2"
    local password="${3:-$API_SEED_PASSWORD}"

    api_request POST "$jar" /api/v1/auth/login \
        "$(printf '{"email":"%s","password":"%s"}' "$email" "$password")"

    if [ "$api_status" != "204" ]; then
        printf 'sign in for %s answered %s: %s\n' "$email" "$api_status" "$(api_body)" >&2

        return 1
    fi
}

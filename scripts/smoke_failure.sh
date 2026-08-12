#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: destructive
#
# Simulation 16: break the confirm transaction on purpose and follow the failure
# all the way to the alert.
#
# A metric nobody has ever seen move is a decoration, and an alert nobody has
# ever seen fire is worse, because it gets mistaken for coverage. This is the
# script that settles both. It arms one fault, drives one real payment through
# the api, and then asserts the number arrives in four places: the api's own
# exposition, Prometheus, the alert, and the dashboard panel bound to the same
# series.
#
# It is classed destructive although it deletes nothing. It deliberately breaks
# a running system, which earns the same manifest, the same y/N defaulting to
# No, the same development guard, and the same --dry-run as a script that
# removes a directory.
#
# What it leaves behind: one booking still in pending_payment, one settled mock
# charge, and a counter one higher than it was. The fault disarms itself after a
# single trigger, and this script disarms it again on the way out whatever
# happened, because a stack left broken by a script that died is the worst
# outcome available here.
#
# Usage:
#   scripts/stack_up.sh backend
#   backend/scripts/migrate.sh
#   backend/scripts/seed.sh
#   APP_ENV=development scripts/smoke_failure.sh
#   APP_ENV=development scripts/smoke_failure.sh --dry-run
#
# Note:
# - the api has to be running with FAULT_INJECTION_ENABLED=true. Without it the
#   routes are not on the mux at all and this script says so rather than
#   guessing why a call came back 404
#
# Exit codes:
# - 0: the failure was visible end to end
# - 1: declined by the operator, or an assertion failed
# - 2: refused by a guard, or the stack is not ready
# ---------------------------------------------------------------------------- #

set -uo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
backend_root="$repository_root/backend"

source "$repository_root/scripts/lib/confirm.sh"
source "$repository_root/scripts/lib/api.sh"
source "$backend_root/scripts/lib/database.sh"

# The point that answers the question this project is about. It fails inside the
# confirm transaction, after the seat has been written and before the commit,
# which is the database dying mid-transaction.
FAULT_POINT="confirm.before_commit"

# The seeded open class, and a child whose parent has no booking on it. Four
# seats and none taken, so nothing here competes with the last-seat race script.
SMOKE_CLASS_ID="0192a000-0000-7000-8000-000000000021"
SMOKE_PARENT_EMAIL="chandra.wijaya@example.test"
SMOKE_STUDENT_ID="0192a000-0000-7000-8000-000000000014"

# Arming is an admin route, guarded by the role check and the write rate limit
# exactly like every other mutation.
ADMIN_EMAIL="ops.admin@example.test"

TRIAL_PRICE_CENTS=4500
TRIAL_CURRENCY="SGD"

PROMETHEUS_BASE="${PROMETHEUS_BASE:-http://127.0.0.1:9003}"
GRAFANA_PANEL_URL="${GRAFANA_PANEL_URL:-http://127.0.0.1:9004/d/ottodot-backend?viewPanel=21}"

# The series the alert fires on, the panel draws, and this script counts.
ERROR_SERIES='confirm_transaction_total{outcome="error"}'
ALERT_NAME="TransactionErrorSpike"

# How long to wait for Prometheus. Two scrape intervals would be enough for the
# series and the alert needs one evaluation more, so this is generous on
# purpose: a slow machine should report late, not report wrongly.
WAIT_SECONDS="${SMOKE_WAIT_SECONDS:-60}"

jar_admin=""
jar_parent=""
armed="no"
booking_id=""
failures=0

cleanup() {
    if [ "$armed" = "yes" ] && [ -n "$jar_admin" ]; then
        api_request DELETE "$jar_admin" /dev/faults ""
        printf '\nfaults disarmed (%s)\n' "$api_status"
    fi

    api_cleanup

    [ -n "$jar_admin" ] && rm -f "$jar_admin"
    [ -n "$jar_parent" ] && rm -f "$jar_parent"

    return 0
}

trap cleanup EXIT

refuse() {
    printf 'refused: %s\n' "$1" >&2

    exit 2
}

expect() {
    local name="$1"
    local expected="$2"
    local actual="$3"

    if [ "$actual" = "$expected" ]; then
        printf '  ok    %-46s %s\n' "$name" "$actual"

        return 0
    fi

    printf '  FAIL  %-46s expected %s, got %s\n' "$name" "$expected" "$actual" >&2
    failures=$((failures + 1))
}

report() {
    local name="$1"
    local detail="$2"

    if [ "$3" = "0" ]; then
        printf '  ok    %-46s %s\n' "$name" "$detail"

        return 0
    fi

    printf '  FAIL  %-46s %s\n' "$name" "$detail" >&2
    failures=$((failures + 1))
}

# Reads one counter out of the api's own exposition.
#
# The line is matched whole rather than searched for, so a metric whose name
# merely starts the same way can never be read as this one.
#
# Return:
# - the value with any decimal part cut, or 0 when the series is absent
metric_value() {
    curl --silent "$API_BASE/metrics" |
        awk -v series="$ERROR_SERIES" '$1 == series { split($2, parts, "."); print parts[1]; found = 1 }
             END { if (!found) print 0 }'
}

# Asks Prometheus a question whose answer is whether anything comes back.
#
# Comparisons filter in Prometheus, so `series > 3` returns nothing at all when
# the value is 3. That turns every check here into empty or not empty, which is
# a question that needs no json parser and cannot be misread.
#
# Param:
# query - string (a Prometheus expression)
#
# Return:
# - 0 when the query answered with at least one series
# - 1 when it answered with none, or did not answer
prometheus_has_series() {
    local answer

    answer="$(curl --silent --get --data-urlencode "query=$1" "$PROMETHEUS_BASE/api/v1/query" 2>/dev/null)"

    case "$answer" in
        *'"status":"success"'*) ;;
        *) return 1 ;;
    esac

    case "$answer" in
        *'"result":[]'*) return 1 ;;
    esac

    return 0
}

# Polls a query until it answers, or until the wait is spent.
wait_for_series() {
    local query="$1"
    local attempt=1

    while [ "$attempt" -le "$WAIT_SECONDS" ]; do
        if prometheus_has_series "$query"; then
            printf '%s\n' "$attempt"

            return 0
        fi

        sleep 1
        attempt=$((attempt + 1))
    done

    printf '%s\n' "$attempt"

    return 1
}

require_fault_surface() {
    api_sign_in "$jar_admin" "$ADMIN_EMAIL" || refuse "the seeded admin could not sign in"

    api_request GET "$jar_admin" /dev/faults ""

    if [ "$api_status" = "404" ]; then
        refuse "the fault surface is off. Start the api with FAULT_INJECTION_ENABLED=true and APP_ENV=development, then run this again"
    fi

    if [ "$api_status" != "200" ]; then
        refuse "the fault surface answered $api_status: $(api_body)"
    fi
}

arm_the_fault() {
    api_request POST "$jar_admin" /dev/faults \
        "$(printf '{"point":"%s","count":1,"ttl_seconds":120}' "$FAULT_POINT")"

    if [ "$api_status" != "200" ]; then
        refuse "arming $FAULT_POINT answered $api_status: $(api_body)"
    fi

    armed="yes"
}

# Holds a seat, reusing the booking from an earlier run rather than refusing.
#
# The booking this leaves behind stays in pending_payment, so a second run finds
# it and is answered already_booked. That is correct behaviour by the api and no
# reason for this script to stop.
hold_a_seat() {
    api_request POST "$jar_parent" /api/v1/bookings \
        "$(printf '{"student_id":"%s","class_id":"%s"}' "$SMOKE_STUDENT_ID" "$SMOKE_CLASS_ID")" \
        "Idempotency-Key: smoke-hold"

    case "$api_status" in
        201) ;;
        409)
            if ! api_body_contains '"code":"already_booked"'; then
                refuse "holding a seat answered 409: $(api_body)"
            fi

            printf '   reusing the booking an earlier run left in pending_payment\n'
            ;;
        *)
            refuse "holding a seat answered $api_status: $(api_body)"
            ;;
    esac

    booking_id="$(database_scalar "
        select id from bookings
        where student_id = '$SMOKE_STUDENT_ID' and class_id = '$SMOKE_CLASS_ID'
        order by created_at desc
        limit 1
    ")"

    if [ -z "$booking_id" ]; then
        refuse "no booking row exists for that child and class"
    fi
}

# Pays with a key no earlier run has used.
#
# A replayed key is answered with the stored result rather than charged again,
# which is the api being correct and this script being useless. The second is
# what the timestamp avoids.
pay_and_break_it() {
    api_request POST "$jar_parent" "/api/v1/bookings/$booking_id/payments" \
        "$(printf '{"amount_cents":%s,"currency":"%s"}' "$TRIAL_PRICE_CENTS" "$TRIAL_CURRENCY")" \
        "Idempotency-Key: smoke-pay-$(date +%s)"
}

run_the_smoke() {
    local baseline
    local after
    local waited

    baseline="$(metric_value)"
    printf '\nbaseline: %s is %s\n' "$ERROR_SERIES" "$baseline"

    printf '\n1. arming %s for one trigger\n' "$FAULT_POINT"
    arm_the_fault

    printf '\n2. holding a seat and paying for it, which is what breaks\n'
    hold_a_seat
    pay_and_break_it
    printf '   http %s\n' "$api_status"

    expect "the parent is told the service broke" "500" "$api_status"

    if api_body_contains '"code":"internal_error"'; then
        report "the refusal is internal_error with a request id" "internal_error" 0
    else
        report "the refusal is internal_error with a request id" "$(api_body)" 1
    fi

    printf '\n3. nothing was left half done\n'
    expect "the booking is still pending_payment" "pending_payment" \
        "$(database_scalar "select status from bookings where id = '$booking_id'")"
    expect "no seat was consumed" "0" \
        "$(database_scalar "select count(*) from bookings where id = '$booking_id' and seat_no is not null")"

    printf '\n4. the api counted it\n'
    after="$(metric_value)"
    expect "the error counter moved by exactly one" "$((baseline + 1))" "$after"

    printf '\n5. Prometheus holds the same series\n'
    waited="$(wait_for_series "$ERROR_SERIES > $baseline")"
    report "the series answers a query" "after ${waited}s" "$?"

    printf '\n6. the alert reaches at least pending\n'
    waited="$(wait_for_series "ALERTS{alertname=\"$ALERT_NAME\"}")"
    report "$ALERT_NAME is pending or firing" "after ${waited}s" "$?"

    printf '\n7. the panel is bound to the same series\n'
    if grep --quiet --fixed-strings 'confirm_transaction_total{outcome=\"error\"}' \
        "$backend_root/containers/grafana/dashboards/backend.json"; then
        report "backend.json queries this exact metric" "panel 21" 0
    else
        report "backend.json queries this exact metric" "the panel query has drifted" 1
    fi

    printf '\n8. the one-shot disarmed itself\n'
    api_request GET "$jar_admin" /dev/faults ""

    if api_body_contains '"armed":[]'; then
        report "nothing is armed any more" "empty" 0
    else
        report "nothing is armed any more" "$(api_body)" 1
    fi
}

main() {
    confirm_parse_flags "$@"
    confirm_require_environment

    api_require_tools || exit 2
    api_require_running || exit 2
    database_require_running || exit 2

    confirm_target_name "ottodot-api" "a fault armed in"
    confirm_target_name "ottodot-postgres-primary" "a booking and a mock charge written to"
    confirm_proceed "break the confirm transaction on purpose, then watch the error reach Prometheus and the alert"

    jar_admin="$(mktemp)"
    jar_parent="$(mktemp)"

    require_fault_surface
    api_sign_in "$jar_parent" "$SMOKE_PARENT_EMAIL" || exit 2

    run_the_smoke

    if [ "$failures" -ne 0 ]; then
        printf '\n%s assertion(s) failed\n' "$failures" >&2

        return 1
    fi

    printf '\nthe failure was visible end to end. The panel is at:\n  %s\n' "$GRAFANA_PANEL_URL"
}

main "$@"

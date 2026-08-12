#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Test 6, the scenario the brief asks for, against a live system.
#
#   1. Parent A takes the last seat of a class and goes to pay
#   2. Parent B takes the same seat and goes to pay
#   3. Parent B pays first and is confirmed
#   4. Parent A pays, and must not also be confirmed
#
# Both parents are allowed to hold, which is the part worth watching. A hold is
# not a seat. The class carries an allowance above its capacity precisely so two
# parents can be on the payment screen at once, and the seat is decided in the
# confirm transaction rather than at the moment somebody clicked.
#
# Everything here goes over http with cookies, exactly as the browser does. No
# step reaches past a guard, so what this prints is what a parent would get.
#
# It asserts against the database afterwards rather than against the http
# answers alone. Four tables have to agree: one seat handed out, two payments
# taken, an audit line for each parent, and a refund job queued for the one who
# lost. An api that answered correctly while leaving those disagreeing would be
# the more interesting failure.
#
# Usage:
#   scripts/stack_up.sh backend
#   backend/scripts/migrate.sh
#   backend/scripts/seed.sh
#   scripts/race_last_seat.sh
#
# Note:
# - it is not repeatable on its own. The seat it races for is gone once
#   somebody has it, so a second run needs scripts/seed_reset.sh first. It says
#   so rather than failing halfway
#
# Return:
# - 0: at most one parent was confirmed and every table agrees
# - 1: an assertion failed, each one printed
# - 2: the stack is not ready, or the seat has already been taken
# ---------------------------------------------------------------------------- #

set -uo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
backend_root="$repository_root/backend"

source "$repository_root/scripts/lib/api.sh"
source "$backend_root/scripts/lib/database.sh"

# The seeded race class: one seat, no confirmed booking, and an allowance that
# admits both parents as holders. It exists in the seed for this script.
RACE_CLASS_ID="0192a000-0000-7000-8000-000000000025"

# Parent A, who reaches the payment screen first and pays second.
PARENT_A_EMAIL="alice.tan@example.test"
PARENT_A_STUDENT_ID="0192a000-0000-7000-8000-000000000012"

# Parent B, who reaches it second and pays first.
PARENT_B_EMAIL="budi.santoso@example.test"
PARENT_B_STUDENT_ID="0192a000-0000-7000-8000-000000000013"

# What a trial class costs. The api refuses any other amount, so this is not a
# number the caller gets to choose.
TRIAL_PRICE_CENTS=4500
TRIAL_CURRENCY="SGD"

jar_a=""
jar_b=""
booking_a=""
booking_b=""
failures=0

cleanup() {
    api_cleanup

    [ -n "$jar_a" ] && rm -f "$jar_a"
    [ -n "$jar_b" ] && rm -f "$jar_b"

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
        printf '  ok    %-44s %s\n' "$name" "$actual"

        return 0
    fi

    printf '  FAIL  %-44s expected %s, got %s\n' "$name" "$expected" "$actual" >&2
    failures=$((failures + 1))
}

# Refuses before the first request when the seat this script races for is
# already gone. Failing here is a message and a command. Failing halfway is a
# half-finished demonstration nobody can read.
require_free_seat() {
    local capacity
    local confirmed

    capacity="$(database_scalar "select capacity from trial_classes where id = '$RACE_CLASS_ID'")"

    if [ -z "$capacity" ]; then
        refuse "the seeded race class is missing, run backend/scripts/seed.sh"
    fi

    confirmed="$(database_scalar "select count(*) from bookings where class_id = '$RACE_CLASS_ID' and status = 'confirmed'")"

    if [ "$confirmed" != "0" ]; then
        refuse "the race class already has $confirmed confirmed booking(s), run scripts/seed_reset.sh to start again"
    fi
}

# Holds a seat and leaves the new booking id in hold_booking_id.
#
# The identifier comes back in a variable rather than on standard output, so a
# refusal inside here can end the whole script. A failure that only ended a
# command substitution would carry on with an empty booking id and report
# something far stranger than what went wrong.
#
# It is read back from the primary rather than picked out of the response body.
# These scripts carry no json parser on purpose, and the row is the more honest
# place to read it from: a booking this script cannot find in the table is not a
# booking, whatever the api answered.
hold_booking_id=""

hold_seat() {
    local jar="$1"
    local student="$2"
    local key="$3"
    local label="$4"

    api_request POST "$jar" /api/v1/bookings \
        "$(printf '{"student_id":"%s","class_id":"%s"}' "$student" "$RACE_CLASS_ID")" \
        "Idempotency-Key: $key"

    if [ "$api_status" != "201" ]; then
        refuse "$label could not hold a seat, the api answered $api_status: $(api_body)"
    fi

    hold_booking_id="$(database_scalar "
        select id from bookings
        where student_id = '$student' and class_id = '$RACE_CLASS_ID'
        order by created_at desc
        limit 1
    ")"

    if [ -z "$hold_booking_id" ]; then
        refuse "$label was answered 201 but no booking row exists, which is a failure worth stopping on"
    fi
}

booking_status() {
    database_scalar "select status from bookings where id = '$1'"
}

pay() {
    local jar="$1"
    local booking="$2"
    local key="$3"

    api_request POST "$jar" "/api/v1/bookings/$booking/payments" \
        "$(printf '{"amount_cents":%s,"currency":"%s"}' "$TRIAL_PRICE_CENTS" "$TRIAL_CURRENCY")" \
        "Idempotency-Key: $key"
}

run_the_race() {
    printf '\n1. parent A takes the last seat and goes to pay\n'
    hold_seat "$jar_a" "$PARENT_A_STUDENT_ID" "race-hold-a" "parent A"
    booking_a="$hold_booking_id"
    printf '   booking %s, status %s\n' "$booking_a" "$(booking_status "$booking_a")"

    printf '\n2. parent B takes the same seat, which the hold allowance permits\n'
    hold_seat "$jar_b" "$PARENT_B_STUDENT_ID" "race-hold-b" "parent B"
    booking_b="$hold_booking_id"
    printf '   booking %s, status %s\n' "$booking_b" "$(booking_status "$booking_b")"

    printf '\n3. parent B pays first\n'
    pay "$jar_b" "$booking_b" "race-pay-b"
    printf '   http %s\n' "$api_status"

    expect "B is answered with a confirmed booking" "200" "$api_status"

    printf '\n4. parent A pays, and the seat is already gone\n'
    pay "$jar_a" "$booking_a" "race-pay-a"
    printf '   http %s\n' "$api_status"

    expect "A is refused with a conflict" "409" "$api_status"

    if api_body_contains '"code":"seat_lost"'; then
        printf '  ok    %-44s %s\n' "A is told the seat was lost" "seat_lost"
    else
        printf '  FAIL  %-44s %s\n' "A is told the seat was lost" "$(api_body)" >&2
        failures=$((failures + 1))
    fi
}

# The four tables the plan names, read straight from the primary. This is what
# a demonstration points at: the answers above are what each parent saw, and these
# are what the system actually did.
#
# Two of the assertions allow for the worker having already run. It polls every
# few seconds, and by the time these queries land it may well have refunded
# parent A and closed the booking. Both states are correct, and an assertion
# that demanded the earlier one would fail on a healthy system for the sole
# reason that it was quick. The audit trail is what pins the outcome, because
# rows are only ever added to it.
assert_the_rows() {
    local status_a

    status_a="$(database_scalar "select status from bookings where id = '$booking_a'")"

    printf '\nbookings\n'
    expect "confirmed bookings on this class" "1" \
        "$(database_scalar "select count(*) from bookings where class_id = '$RACE_CLASS_ID' and status = 'confirmed'")"
    expect "B is confirmed" "confirmed" \
        "$(database_scalar "select status from bookings where id = '$booking_b'")"
    expect "B was given the one seat" "1" \
        "$(database_scalar "select seat_no from bookings where id = '$booking_b'")"
    expect "A is owed a refund, or has had one ($status_a)" "yes" \
        "$(database_scalar "select case when status in ('refund_required', 'cancelled') then 'yes' else 'no' end from bookings where id = '$booking_a'")"
    expect "A holds no seat number" "0" \
        "$(database_scalar "select count(*) from bookings where id = '$booking_a' and seat_no is not null")"

    printf '\npayment_attempts\n'
    expect "both parents were charged" "2" \
        "$(database_scalar "select count(*) from payment_attempts where booking_id in ('$booking_a', '$booking_b')")"
    expect "both charges settled" "2" \
        "$(database_scalar "select count(*) from payment_attempts where booking_id in ('$booking_a', '$booking_b') and status = 'succeeded'")"

    printf '\nbooking_events\n'
    expect "B moved to confirmed" "1" \
        "$(database_scalar "select count(*) from booking_events where booking_id = '$booking_b' and from_status = 'pending_payment' and to_status = 'confirmed'")"
    expect "A moved to refund_required" "1" \
        "$(database_scalar "select count(*) from booking_events where booking_id = '$booking_a' and from_status = 'pending_payment' and to_status = 'refund_required'")"

    printf '\njob_queue\n'
    expect "a refund job names A, or the worker has run it" "yes" \
        "$(database_scalar "
            select case when
                exists (select 1 from job_queue where kind = 'reconcile_refund' and payload->>'booking_id' = '$booking_a')
                or exists (select 1 from booking_events where booking_id = '$booking_a' and from_status = 'refund_required')
            then 'yes' else 'no' end
        ")"
}

main() {
    api_require_tools || exit 2
    api_require_running || exit 2
    database_require_running || exit 2

    require_free_seat

    jar_a="$(mktemp)"
    jar_b="$(mktemp)"

    api_sign_in "$jar_a" "$PARENT_A_EMAIL" || exit 2
    api_sign_in "$jar_b" "$PARENT_B_EMAIL" || exit 2

    run_the_race
    assert_the_rows

    if [ "$failures" -ne 0 ]; then
        printf '\n%s assertion(s) failed\n' "$failures" >&2

        return 1
    fi

    printf '\nexactly one parent was confirmed for the last seat, and the money that\n'
    printf 'moved for the other one is on its way back.\n'
}

main "$@"

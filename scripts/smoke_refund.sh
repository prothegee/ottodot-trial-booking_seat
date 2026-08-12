#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: destructive
#
# Moves the number the `Refunds owed` panel draws, on purpose, so the one alert
# in this project that costs somebody real money can be watched arriving and
# clearing.
#
# `refund_pending_bookings` is a gauge. The api counts it every five seconds by
# asking how many bookings sit in `refund_required` right now, so the only way to
# move the panel is to move rows. `--increase` writes bookings that say a parent
# paid and lost the seat. `--decrease` closes that many again, newest first, so
# an increase can be undone exactly.
#
# It asks which parent before it writes anything, because a refund is owed to
# somebody and a demonstration reads better when the operator surface names a
# person rather than whoever happened to be first in the seed. Only the demo
# accounts are offered, and a real address is never in the list to choose from.
#
# It is classed destructive although `--increase` only inserts. `--decrease`
# closes a booking without any money being sent back, which on a real system
# would erase the record that a parent is out of pocket. That earns the same
# manifest, the same y/N defaulting to No, the same development guard, and the
# same --dry-run as a script that removes a directory.
#
# What it leaves behind: bookings in `refund_required` with a settled charge and
# an audit line each, or those same bookings closed as `cancelled`. Seat counts
# are untouched, because a booking in either status holds no seat.
#
# Usage:
#   scripts/stack_up.sh backend
#   backend/scripts/migrate.sh
#   backend/scripts/seed.sh
#   APP_ENV=development scripts/smoke_refund.sh --increase
#   APP_ENV=development scripts/smoke_refund.sh --increase 5
#   APP_ENV=development scripts/smoke_refund.sh --decrease 3
#   APP_ENV=development scripts/smoke_refund.sh --increase 2 --dry-run
#
# Note:
# - the number without a flag is 1, and a number after the flag replaces it
# - the choice is limited to accounts on the demo domain, `example.test`, which
#   is reserved for testing and can never belong to a real person
# - decreasing below zero is not possible. With nothing owed to that parent the
#   run says so and stops, and asking for more than they are owed closes what
#   there is
# - `RefundBacklog` fires on anything above zero held for five minutes, so an
#   increase left standing is how that alert is watched
#
# Exit codes:
# - 0: the number moved and the gauge agrees, or there was nothing to decrease
# - 1: declined by the operator, or an assertion failed
# - 2: refused by a guard, or the stack is not ready
# ---------------------------------------------------------------------------- #

set -uo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
backend_root="$repository_root/backend"

source "$repository_root/scripts/lib/confirm.sh"
source "$repository_root/scripts/lib/api.sh"
source "$backend_root/scripts/lib/database.sh"

# The seeded class these bookings are written against. Any class does, because a
# booking in `refund_required` holds no seat and counts against no capacity.
SMOKE_CLASS_ID="0192a000-0000-7000-8000-000000000021"

# The only accounts this script will write a refund against. `.test` is reserved
# for testing and can never be a real address, so a database that somehow held
# one offers nothing here rather than offering a person.
DEMO_EMAIL_DOMAIN="${DEMO_EMAIL_DOMAIN:-example.test}"

# What a trial class costs, so the settled charge on each booking is the amount
# the api would have taken.
TRIAL_PRICE_CENTS=4500
TRIAL_CURRENCY="SGD"

# The audit lines this script writes. They are what an operator reading
# booking_events sees, and they say plainly that a script put the row there.
REASON_OWED="refund owed, written by smoke_refund.sh"
REASON_CLEARED="refund closed by smoke_refund.sh, no money was sent back"

# The gauge, the alert it feeds, and where the panel is.
GAUGE_SERIES="refund_pending_bookings"
ALERT_NAME="RefundBacklog"
PROMETHEUS_BASE="${PROMETHEUS_BASE:-http://127.0.0.1:9003}"
GRAFANA_DASHBOARD_URL="${GRAFANA_DASHBOARD_URL:-http://127.0.0.1:9004/d/ottodot-backend}"

# The api resamples every five seconds and Prometheus scrapes on the same
# period, so a healthy stack agrees within about ten. This is generous on
# purpose: a slow machine should report late, not report wrongly.
WAIT_SECONDS="${SMOKE_WAIT_SECONDS:-30}"

# The most this gauge ever reads. The api caps its own worklist there, so a
# backlog pushed past it stops being visible on the panel.
GAUGE_CEILING=200

action=""
step=1
confirm_flags=()

parent_id=""
parent_name=""
parent_email=""
student_id=""
failures=0

cleanup() {
    api_cleanup

    return 0
}

trap cleanup EXIT

refuse() {
    printf 'refused: %s\n' "$1" >&2

    exit 2
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

# Reads the flags, and the optional number that may follow either action.
#
# --dry-run and --yes are collected rather than handled, because confirm.sh owns
# those two and a second reading of them here would be a copy that drifts.
parse_flags() {
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --increase|--decrease)
                if [ -n "$action" ]; then
                    refuse "pass one of --increase or --decrease, not both"
                fi

                action="${1#--}"

                case "${2:-}" in
                    ''|*[!0-9]*) ;;
                    *)
                        step="$2"
                        shift
                        ;;
                esac
                ;;
            --dry-run|--yes)
                confirm_flags+=("$1")
                ;;
            *)
                refuse "unknown flag '$1', this script takes --increase or --decrease, each with an optional number, and --dry-run or --yes"
                ;;
        esac

        shift
    done

    if [ -z "$action" ]; then
        refuse "nothing to do, pass --increase or --decrease, each with an optional number"
    fi

    if [ "$step" -lt 1 ]; then
        refuse "the number has to be 1 or more, '$step' moves nothing"
    fi
}

# Reads the gauge out of the api's own exposition.
#
# The line is matched whole rather than searched for, so a metric whose name
# merely starts the same way can never be read as this one.
#
# Return:
# - the value with any decimal part cut, or 0 when the series is absent
metric_value() {
    curl --silent "$API_BASE/metrics" |
        awk -v series="$GAUGE_SERIES" '$1 == series { split($2, parts, "."); print parts[1]; found = 1 }
             END { if (!found) print 0 }'
}

# Polls the api until the gauge reads what it should, or the wait is spent.
#
# Param:
# expected - string (the value the gauge should reach)
#
# Return:
# - 0 with the seconds waited printed, when it arrived
# - 1 with the seconds waited printed, when it did not
wait_for_gauge() {
    local expected="$1"
    local attempt=1

    while [ "$attempt" -le "$WAIT_SECONDS" ]; do
        if [ "$(metric_value)" = "$expected" ]; then
            printf '%s\n' "$attempt"

            return 0
        fi

        sleep 1
        attempt=$((attempt + 1))
    done

    printf '%s\n' "$attempt"

    return 1
}

# Asks Prometheus a question whose answer is whether anything comes back.
#
# Comparisons filter in Prometheus, so a query that does not match returns
# nothing at all. That turns the check into empty or not empty, which needs no
# json parser and cannot be misread.
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

backlog_total() {
    database_scalar "select count(*) from bookings where status = 'refund_required'"
}

backlog_for_parent() {
    database_scalar "
        select count(*)
        from bookings
        where status = 'refund_required'
          and student_id in (select id from students where parent_id = '$parent_id')
    "
}

# Asks which parent this run is about, and remembers one of their children.
#
# The list is read from the database rather than written here, so it stays true
# when the seed changes. What each parent is already owed is on the line,
# because that is the number the next decision depends on.
#
# Only the demo accounts are offered, and DEMO_EMAIL_DOMAIN is the whole of that
# rule. A real address could never be chosen here even by typing its number,
# because it is never in the list this reads from.
choose_the_parent() {
    local ids=()
    local names=()
    local emails=()
    local owed=()
    local row_id row_name row_email row_owed
    local choice=""
    local index=1

    while IFS='|' read -r row_id row_name row_email row_owed; do
        if [ -z "$row_id" ]; then
            continue
        fi

        ids+=("$row_id")
        names+=("$row_name")
        emails+=("$row_email")
        owed+=("$row_owed")
    done < <(database_scalar "
        select parents.id, parents.full_name, parents.email, count(bookings.id)
        from parents
        left join students on students.parent_id = parents.id
        left join bookings on bookings.student_id = students.id
                          and bookings.status = 'refund_required'
        where parents.role = 'parent'
          and parents.email like '%@$DEMO_EMAIL_DOMAIN'
        group by parents.id, parents.full_name, parents.email
        order by parents.email
    ")

    if [ "${#ids[@]}" -eq 0 ]; then
        refuse "no demo parent accounts on $DEMO_EMAIL_DOMAIN exist, run backend/scripts/seed.sh"
    fi

    printf '\nwhich demo parent is this refund for?\n\n'

    while [ "$index" -le "${#ids[@]}" ]; do
        printf '  %s) %-16s %-30s owed %s\n' \
            "$index" "${names[index - 1]}" "${emails[index - 1]}" "${owed[index - 1]}"
        index=$((index + 1))
    done

    printf '\n'

    if [ -t 0 ]; then
        printf 'choose [1]: '
        read -r choice || choice=""
    else
        printf 'nothing here can answer, taking the first.\n'
    fi

    if [ -z "$choice" ]; then
        choice=1
    fi

    case "$choice" in
        ''|*[!0-9]*) refuse "'$choice' is not one of the numbers offered" ;;
    esac

    if [ "$choice" -lt 1 ] || [ "$choice" -gt "${#ids[@]}" ]; then
        refuse "'$choice' is not one of the numbers offered"
    fi

    parent_id="${ids[choice - 1]}"
    parent_name="${names[choice - 1]}"
    parent_email="${emails[choice - 1]}"

    student_id="$(database_scalar "select id from students where parent_id = '$parent_id' order by full_name limit 1")"

    if [ -z "$student_id" ]; then
        refuse "$parent_name has no child on the account, so no booking can be written"
    fi

    printf '\nchosen: %s (%s)\n' "$parent_name" "$parent_email"
}

# Writes the bookings that put a parent in the backlog.
#
# Each one is a booking, the charge that settled for it, and the audit line that
# says why it is owed, in one statement. A booking saying money moved with no
# payment row behind it would be a state the service itself never produces.
raise_the_backlog() {
    database_scalar "
        with made as (
            insert into bookings (id, student_id, class_id, status)
            select gen_random_uuid(), '$student_id', '$SMOKE_CLASS_ID', 'refund_required'
            from generate_series(1, $step)
            returning id
        ),
        charged as (
            insert into payment_attempts
                (id, booking_id, idempotency_key, amount_cents, currency, status, provider_ref, settled_at)
            select gen_random_uuid(), made.id, 'smoke-refund-' || made.id,
                   $TRIAL_PRICE_CENTS, '$TRIAL_CURRENCY', 'succeeded', 'mock_smoke_refund', now()
            from made
            returning booking_id
        ),
        recorded as (
            insert into booking_events (id, booking_id, from_status, to_status, actor, reason)
            select gen_random_uuid(), charged.booking_id, 'pending_payment', 'refund_required',
                   'admin', '$REASON_OWED'
            from charged
            returning booking_id
        )
        select count(*) from recorded
    "
}

# Closes that many of the parent's owed bookings, newest first.
#
# Newest first is what makes an increase undoable: the rows this script wrote a
# moment ago go before anything a real lost race left behind.
lower_the_backlog() {
    database_scalar "
        with picked as (
            select id
            from bookings
            where status = 'refund_required'
              and student_id in (select id from students where parent_id = '$parent_id')
            order by created_at desc
            limit $step
        ),
        closed as (
            update bookings
            set status = 'cancelled', updated_at = now()
            where id in (select id from picked)
            returning id
        ),
        recorded as (
            insert into booking_events (id, booking_id, from_status, to_status, actor, reason)
            select gen_random_uuid(), closed.id, 'refund_required', 'cancelled', 'admin', '$REASON_CLEARED'
            from closed
            returning booking_id
        )
        select count(*) from recorded
    "
}

# Names what will happen, then asks. Nothing above this point has written a row.
ask_first() {
    local before="$1"

    confirm_target_name "ottodot-postgres-primary" "the database written to"

    if [ "$action" = "increase" ]; then
        confirm_proceed "write $step booking(s) for $parent_name that say the money moved and the seat did not, taking the backlog from $before to $((before + step))"

        return 0
    fi

    confirm_proceed "close $step of the booking(s) $parent_name is owed a refund on, without any money being sent back"
}

# Follows the new number from the database to the api and then to Prometheus.
#
# The database count is the truth and the gauge is what the panel draws, so both
# are read. A number that moved in one and not the other is the failure this is
# here to catch.
verify_the_number() {
    local expected="$1"
    local waited

    printf '\n3. the api resampled\n'
    waited="$(wait_for_gauge "$expected")"
    report "$GAUGE_SERIES reads $expected" "after ${waited}s" "$?"

    printf '\n4. Prometheus holds the same number\n'
    waited="$(wait_for_series "$GAUGE_SERIES == $expected")"
    report "the series answers a query" "after ${waited}s" "$?"
}

run_the_change() {
    local before
    local owed_here
    local moved
    local expected
    local after

    before="$(backlog_total)"
    owed_here="$(backlog_for_parent)"

    printf '\nnow: %s is %s, %s of them for %s\n' \
        "$GAUGE_SERIES" "$before" "$owed_here" "$parent_name"

    if [ "$action" = "increase" ] && [ "$((before + step))" -gt "$GAUGE_CEILING" ]; then
        refuse "the api caps this gauge at $GAUGE_CEILING, so $before plus $step would stop being visible on the panel"
    fi

    if [ "$action" = "decrease" ] && [ "$owed_here" = "0" ]; then
        printf '\n%s is owed nothing, so there is nothing to close. The number stays at %s.\n' \
            "$parent_name" "$before"

        exit 0
    fi

    ask_first "$before"

    if [ "$action" = "increase" ]; then
        printf '\n1. writing %s booking(s) into refund_required\n' "$step"
        moved="$(raise_the_backlog)"
        expected=$((before + moved))
    else
        printf '\n1. closing %s booking(s) that are owed a refund\n' "$step"
        moved="$(lower_the_backlog)"
        expected=$((before - moved))
    fi

    printf '   %s row(s) changed\n' "$moved"

    if [ "$moved" != "$step" ]; then
        printf '   asked for %s, and %s is what %s had\n' "$step" "$moved" "$parent_name"
    fi

    printf '\n2. the database agrees\n'
    after="$(backlog_total)"

    if [ "$after" = "$expected" ]; then
        report "the backlog is now $expected" "$after" 0
    else
        report "the backlog is now $expected" "found $after" 1
    fi

    verify_the_number "$expected"

    printf '\nthe Refunds owed panel is on the backend dashboard, top left:\n  %s\n' \
        "$GRAFANA_DASHBOARD_URL"

    if [ "$expected" -gt 0 ]; then
        printf '\n%s fires on anything above zero held for five minutes.\n' "$ALERT_NAME"
    fi
}

main() {
    parse_flags "$@"

    if [ "${#confirm_flags[@]}" -gt 0 ]; then
        confirm_parse_flags "${confirm_flags[@]}"
    fi

    confirm_require_environment

    api_require_tools || exit 2
    api_require_running || exit 2
    database_require_running || exit 2

    choose_the_parent
    run_the_change

    if [ "$failures" -ne 0 ]; then
        printf '\n%s assertion(s) failed\n' "$failures" >&2

        return 1
    fi
}

main "$@"

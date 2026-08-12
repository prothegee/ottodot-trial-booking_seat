#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Tests the widest destructive script in this repository without letting it
# destroy anything. Every case here stops inside the guard, so no container, no
# image, and no directory is ever touched by this file.
#
# The case that matters most is the prefix. cleanup_dev.sh is the one script
# that removes containers and images by name, so the property worth pinning is
# that it refuses every target it cannot prove it owns.
#
# Some of the cases read the source rather than run it. A wildcard removal is
# dangerous whether or not the guard is reached, and no exit code can prove its
# absence.
#
# Usage:
#   scripts/cleanup_dev_test.sh
#
# Return:
# - 0: every case passed
# - 1: at least one case failed, each one printed
# - 2: a flag was passed, and this file takes none
# ---------------------------------------------------------------------------- #

set -uo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
subject="$repository_root/scripts/cleanup_dev.sh"
self="${BASH_SOURCE[0]}"

output_file=""
passed=0
failed=0

cleanup() {
    if [ -n "$output_file" ] && [ -f "$output_file" ]; then
        rm -f "$output_file"
    fi
}

trap cleanup EXIT

report_pass() {
    printf '  pass: %s\n' "$1"
    passed=$((passed + 1))
}

report_fail() {
    printf '  FAIL: %s\n' "$1" >&2
    failed=$((failed + 1))
}

# Runs the subject with no terminal attached and reports its exit code.
run_subject() {
    "$subject" "$@" >"$output_file" 2>&1 </dev/null

    printf '%s\n' "$?"
}

check_code() {
    local name="$1"
    local expected="$2"
    local actual="$3"

    if [ "$actual" != "$expected" ]; then
        report_fail "$name: expected exit $expected, got $actual"
        printf '    output: %s\n' "$(tr '\n' ' ' <"$output_file")" >&2

        return 1
    fi

    report_pass "$name"

    return 0
}

run_guard_cases() {
    printf 'guard cases\n'

    check_code "APP_ENV unset refuses" 2 "$(unset APP_ENV; run_subject --dry-run)"
    check_code "APP_ENV production refuses" 2 "$(APP_ENV=production run_subject --dry-run)"
    check_code "unknown flag refuses" 2 "$(APP_ENV=development run_subject --force)"
    check_code "a pipe without --yes refuses" 2 "$(APP_ENV=development run_subject)"
    check_code "--dry-run touches nothing" 0 "$(APP_ENV=development run_subject --dry-run)"
}

# The requirement this file exists for: every name the script would remove has
# to carry the project prefix, and one that does not stops the whole run.
#
# Moving the prefix is how that is proven against the real target list. With it
# set to something else, every literal name in the script becomes a name it
# cannot prove it owns, and the first one refuses.
run_prefix_cases() {
    local code

    printf 'prefix cases\n'

    code="$(APP_ENV=development CONFIRM_PROJECT_PREFIX=notthisproject run_subject --dry-run)"

    check_code "a target lacking the project prefix refuses" 2 "$code"

    if grep --quiet 'does not carry the project prefix' "$output_file"; then
        report_pass "the refusal names the prefix that was missing"
    else
        report_fail "the refusal did not say which prefix was missing"
        printf '    output: %s\n' "$(tr '\n' ' ' <"$output_file")" >&2
    fi
}

# Reads the manifest the operator is shown and checks it against the two rules
# every target obeys: a name carries the prefix, a path sits inside this
# repository.
run_manifest_cases() {
    local line
    local value
    local names=0
    local paths=0
    local strays=0

    printf 'manifest cases\n'

    APP_ENV=development run_subject --dry-run >/dev/null

    while read -r line; do
        case "$line" in
            *'path: '*)
                value="${line#*path: }"
                paths=$((paths + 1))

                case "$value" in
                    "$repository_root"/*) ;;
                    *)
                        strays=$((strays + 1))
                        printf '    outside the repository: %s\n' "$value" >&2
                        ;;
                esac
                ;;
            *'container: '* | *'image: '* | *'network: '*)
                value="${line##*: }"
                names=$((names + 1))

                case "$value" in
                    ottodot*) ;;
                    *)
                        strays=$((strays + 1))
                        printf '    missing the prefix: %s\n' "$value" >&2
                        ;;
                esac
                ;;
        esac
    done <"$output_file"

    if [ "$names" -gt 0 ] && [ "$paths" -gt 0 ]; then
        report_pass "the manifest names $names container(s), image(s), and network(s) and $paths path(s)"
    else
        report_fail "the manifest is missing targets: $names name(s), $paths path(s)"
    fi

    if [ "$strays" -eq 0 ]; then
        report_pass "every target in the manifest is one this project owns"
    else
        report_fail "$strays target(s) in the manifest are not this project's"
    fi
}

# Finds a pattern in the script's code and never in its prose.
#
# The comment lines have to come out first. This file's own subject documents
# the rule it is being checked against, in the words "no wildcard, no prune",
# and a check that reads that sentence as a violation would fail every correctly
# written script.
#
# Param:
# pattern - string (an extended regular expression)
#
# Return:
# - the matching lines with their numbers on standard output
# - 1 when the code does not match, which is the passing case here
matches_in_code() {
    local pattern="$1"

    grep --line-number --extended-regexp "$pattern" "$subject" |
        grep --invert-match --extended-regexp '^[0-9]+:[[:space:]]*#'
}

# A guard cannot prove the absence of a wildcard, because a wildcard that
# expands wrongly does so after every guard has passed. This reads the file
# instead.
run_source_cases() {
    local found

    printf 'source cases\n'

    found="$(matches_in_code '(prune|--all|[[:space:]]-a([[:space:]]|$))')"

    if [ -n "$found" ]; then
        report_fail "the script contains a bulk removal flag"
        printf '%s\n' "$found" >&2
    else
        report_pass "no prune and no bulk removal flag"
    fi

    found="$(matches_in_code 'rm .*\$\(')"

    if [ -n "$found" ]; then
        report_fail "a removal takes its target from a command substitution"
        printf '%s\n' "$found" >&2
    else
        report_pass "no removal takes its target from a command substitution"
    fi

    # A removal that fails and says nothing is what deleted a database out from
    # under a container that was still running. The container then answered
    # pg_isready, failed every query, and the next run reported something
    # impossible about the catalogue instead of the truth.
    if [ -z "$(matches_in_code 'survivors\+=\("\$target"\)')" ]; then
        report_fail "a container that could not be removed is not recorded"
    else
        report_pass "a container that could not be removed is recorded"
    fi

    if [ -z "$(matches_in_code 'could not remove container %s')" ]; then
        report_fail "a failed removal says nothing"
    else
        report_pass "a failed removal is reported rather than swallowed"
    fi

    if [ -z "$(matches_in_code 'refusing to remove any state')" ]; then
        report_fail "state is removed even when a container survived"
    else
        report_pass "no state is removed while a container is still here"
    fi

    # Podman holds a dependency record that outlives the container it points at,
    # and refuses the removal until the record goes with it. Without the retry a
    # cleanup stops on a container nothing depends on any more.
    if [ -z "$(matches_in_code 'rm --force --volumes --depend')" ]; then
        report_fail "a refused removal is not retried with --depend"
    else
        report_pass "a refused removal is retried with --depend"
    fi

    # Docker has no --depend, and the docker command is a shim over podman on
    # some machines. A retry gated on the name would take the flag to a runtime
    # that refuses it, and hide the real complaint behind an unknown flag.
    if [ -z "$(matches_in_code 'runtime_takes_depend')" ]; then
        report_fail "the --depend retry is not gated on the runtime taking it"
    else
        report_pass "the --depend retry is gated on the runtime taking it"
    fi
}

# The tag this script falls back to has to be the tag compose builds.
#
# It did not. compose fell back to 0.1.0 and this script to dev, so a cleanup run
# the documented way named an image that had never existed, reported it removed
# because a missing target is not a failure, and left the built one behind. Both
# fallbacks are read here so they cannot drift apart again.
run_image_tag_cases() {
    local compose_file="$repository_root/backend/compose.yml"
    local compose_tag
    local cleanup_tag

    printf 'image tag cases\n'

    compose_tag="$(sed -n 's/^[[:space:]]*image: ottodot-api:${BUILD_VERSION:-\(.*\)}$/\1/p' "$compose_file")"
    cleanup_tag="$(sed -n 's/^[[:space:]]*"ottodot-api:${BUILD_VERSION:-\(.*\)}"$/\1/p' "$subject")"

    if [ -z "$compose_tag" ] || [ -z "$cleanup_tag" ]; then
        report_fail "the api image tag could not be read from both files"
        printf '    compose: %s, cleanup: %s\n' "${compose_tag:-none}" "${cleanup_tag:-none}" >&2

        return
    fi

    if [ "$compose_tag" = "$cleanup_tag" ]; then
        report_pass "the api image tag falls back to $cleanup_tag in both files"
    else
        report_fail "compose builds ottodot-api:$compose_tag and this removes ottodot-api:$cleanup_tag"
    fi
}

# This file sets APP_ENV per case and takes no flags of its own, so a flag typed
# here used to run everything and print the same report, which reads as though
# the flag did something.
#
# The call below ends in the guard at the top of main, before a single case runs,
# so it costs one process and cannot recurse.
run_self_cases() {
    local code

    printf 'self cases\n'

    "$self" --dry-run >"$output_file" 2>&1 </dev/null
    code="$?"

    check_code "a flag passed to this file refuses" 2 "$code"
}

main() {
    if [ "$#" -gt 0 ]; then
        printf "refused: unknown flag '%s', this file takes no flags\\n" "$1" >&2

        return 2
    fi

    output_file="$(mktemp)"

    run_guard_cases
    run_prefix_cases
    run_manifest_cases
    run_source_cases
    run_image_tag_cases
    run_self_cases

    printf '\n%s passed, %s failed\n' "$passed" "$failed"

    [ "$failed" -eq 0 ]
}

main "$@"

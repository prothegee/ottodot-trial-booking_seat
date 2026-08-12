#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Tests scripts/lib/settings.sh. Nothing here starts a container or reads a
# stack's real settings file: the library is sourced, and every case runs against
# a throwaway file in a temporary directory.
#
# The property worth pinning is that a missing settings file is not an error. It
# is the ordinary state of a fresh clone, and a clone that refused to run until
# somebody hand wrote a file would be a clone nobody could review.
#
# The last case is the other half of that promise: the copy only works while both
# templates are committed, so this checks they still are.
#
# Usage:
#   scripts/lib/settings_test.sh
#
# Return:
# - 0: every case passed
# - 1: at least one case failed, each one printed
# - 2: a flag was passed, and this file takes none
# ---------------------------------------------------------------------------- #

set -uo pipefail

library_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd "$library_directory/../.." && pwd)"

source "$library_directory/settings.sh"

work_directory=""
passed=0
failed=0

cleanup() {
    if [ -n "$work_directory" ] && [ -d "$work_directory" ]; then
        rm -rf "$work_directory"
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

check_value() {
    local name="$1"
    local expected="$2"
    local actual="$3"

    if [ "$actual" != "$expected" ]; then
        report_fail "$name: expected '$expected', got '$actual'"

        return 1
    fi

    report_pass "$name"

    return 0
}

# A template that is nothing like either real one, so a case can only pass by
# copying what it was given rather than by recognising a setting.
#
# Param:
# path - string (where to write it)
write_template() {
    printf 'the committed template\n' >"$1"
}

run_missing_file_cases() {
    local target="$work_directory/missing/config.json"
    local template="$work_directory/missing/config.json.template"
    local output
    local status

    printf 'a settings file that is not there\n'

    mkdir --parents "$work_directory/missing"
    write_template "$template"

    output="$(settings_ensure "$target" "$template" 2>&1)"
    status="$?"

    check_value "it carries on" "0" "$status"
    check_value "the file is made" "yes" "$([ -f "$target" ] && echo yes || echo no)"
    check_value "it holds the template" "the committed template" "$(cat "$target")"
    check_value "it says which file was missing" "said" \
        "$(printf '%s' "$output" | grep --quiet 'no settings file' && echo said || echo silent)"
}

# The one case that protects a value somebody set by hand. A library that
# refreshed an existing file from the template would throw away the local
# database url, or the signing key, of whoever ran it.
run_existing_file_cases() {
    local target="$work_directory/existing/.env"
    local template="$work_directory/existing/.env.template"
    local output
    local status

    printf 'a settings file this machine already edited\n'

    mkdir --parents "$work_directory/existing"
    write_template "$template"
    printf 'a value only this machine knows\n' >"$target"

    output="$(settings_ensure "$target" "$template" 2>&1)"
    status="$?"

    check_value "it carries on" "0" "$status"
    check_value "the edit survived" "a value only this machine knows" "$(cat "$target")"
    check_value "it stays quiet" "quiet" "$([ -z "$output" ] && echo quiet || echo spoke)"
}

# No file and no template is a broken clone, not a missing setting, and the two
# are worth telling apart: one is fixed by running the script, the other is not
# fixed by anything the script can do.
run_broken_clone_cases() {
    local target="$work_directory/broken/config.json"
    local template="$work_directory/broken/config.json.template"
    local output
    local status

    printf 'neither the file nor the template\n'

    mkdir --parents "$work_directory/broken"

    output="$(settings_ensure "$target" "$template" 2>&1)"
    status="$?"

    check_value "it refuses" "1" "$status"
    check_value "nothing was made" "no" "$([ -f "$target" ] && echo yes || echo no)"
    check_value "it names the template it wanted" "named" \
        "$(printf '%s' "$output" | grep --quiet 'no template at' && echo named || echo silent)"
}

# Every case above proves the copy works. This one proves there is something to
# copy, which is the half that lives in the checkout rather than in the library.
run_committed_template_cases() {
    printf 'the templates a fresh clone needs\n'

    check_value "backend/config.json.template is committed" "yes" \
        "$([ -f "$repository_root/backend/config.json.template" ] && echo yes || echo no)"
    check_value "frontend/.env.template is committed" "yes" \
        "$([ -f "$repository_root/frontend/.env.template" ] && echo yes || echo no)"
}

main() {
    if [ "$#" -gt 0 ]; then
        printf "refused: unknown flag '%s', this file takes no flags\\n" "$1" >&2

        return 2
    fi

    work_directory="$(mktemp --directory)"

    run_missing_file_cases
    run_existing_file_cases
    run_broken_clone_cases
    run_committed_template_cases

    printf '\n%s passed, %s failed\n' "$passed" "$failed"

    [ "$failed" -eq 0 ]
}

main "$@"

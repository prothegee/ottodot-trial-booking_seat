#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Tests every guard in confirm.sh. It builds a throwaway destructive script in a
# temporary directory, runs it under each condition, and checks both the exit
# code and whether the target file survived.
#
# The exit code alone is not enough. A guard that returns the right number while
# still deleting the file would pass a weaker test, so every case asserts the
# file too.
#
# Usage:
#   scripts/lib/confirm_test.sh
#
# Return:
# - 0: every case passed
# - 1: at least one case failed, each one printed
# - 2: a flag was passed, and this file takes none
# ---------------------------------------------------------------------------- #

set -uo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

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

# The subject under test: a minimal destructive script that removes one file
# inside the repository, guarded by confirm.sh exactly as a real one would be.
write_subject() {
    cat >"$work_directory/subject.sh" <<SUBJECT
#!/usr/bin/env bash
set -euo pipefail

source "$repository_root/scripts/lib/confirm.sh"

confirm_parse_flags "\$@"
confirm_target_path "$work_directory/target.txt"
confirm_proceed "remove the test target"

rm -f "$work_directory/target.txt"
SUBJECT

    chmod 0755 "$work_directory/subject.sh"
}

reset_target() {
    printf 'still here\n' >"$work_directory/target.txt"
}

# Runs the subject without a terminal and reports the exit code.
run_piped() {
    reset_target
    "$work_directory/subject.sh" "$@" >"$work_directory/output.txt" 2>&1 </dev/null

    printf '%s\n' "$?"
}

# Runs the subject with a real pseudo terminal, feeding the given answer.
run_interactive() {
    local answer="$1"

    shift
    reset_target

    printf '%s\n' "$answer" | script --quiet --return \
        --command "$work_directory/subject.sh $*" /dev/null \
        >"$work_directory/output.txt" 2>&1

    printf '%s\n' "$?"
}

target_exists() {
    [ -f "$work_directory/target.txt" ]
}

check() {
    local name="$1"
    local expected_code="$2"
    local expected_target="$3"
    local actual_code="$4"
    local actual_target="present"

    target_exists || actual_target="removed"

    if [ "$actual_code" != "$expected_code" ]; then
        report_fail "$name: expected exit $expected_code, got $actual_code"
        printf '    output: %s\n' "$(tr '\n' ' ' <"$work_directory/output.txt")" >&2

        return 1
    fi

    if [ "$actual_target" != "$expected_target" ]; then
        report_fail "$name: expected target $expected_target, it was $actual_target"

        return 1
    fi

    report_pass "$name"

    return 0
}

run_guard_cases() {
    printf 'guard cases, no terminal\n'

    check "APP_ENV unset refuses" 2 present "$(unset APP_ENV; run_piped)"
    check "APP_ENV empty refuses" 2 present "$(APP_ENV= run_piped)"
    check "APP_ENV production refuses" 2 present "$(APP_ENV=production run_piped)"
    check "unknown flag refuses" 2 present "$(APP_ENV=development run_piped --force)"
    check "a pipe without --yes refuses" 2 present "$(APP_ENV=development run_piped)"
    check "--dry-run touches nothing" 0 present "$(APP_ENV=development run_piped --dry-run)"
    check "--yes on a pipe proceeds" 0 removed "$(APP_ENV=development run_piped --yes)"
}

run_prompt_cases() {
    if ! command -v script >/dev/null 2>&1; then
        printf 'prompt cases skipped: the "script" command is needed to attach a pseudo terminal\n'

        return 0
    fi

    printf 'prompt cases, real terminal\n'

    check "an empty answer declines" 1 present "$(APP_ENV=development run_interactive '')"
    check "n declines" 1 present "$(APP_ENV=development run_interactive 'n')"
    check "N declines" 1 present "$(APP_ENV=development run_interactive 'N')"
    check "yes declines, only y confirms" 1 present "$(APP_ENV=development run_interactive 'yes')"
    check "y confirms" 0 removed "$(APP_ENV=development run_interactive 'y')"
}

run_target_cases() {
    printf 'target cases\n'

    reset_target

    APP_ENV=development bash -c "
        set -euo pipefail
        source '$repository_root/scripts/lib/confirm.sh'
        confirm_target_path '/etc/passwd'
    " >"$work_directory/output.txt" 2>&1

    check "a path outside the repository refuses" 2 present "$?"

    APP_ENV=development bash -c "
        set -euo pipefail
        source '$repository_root/scripts/lib/confirm.sh'
        confirm_target_path '$repository_root/.data/../../escape'
    " >"$work_directory/output.txt" 2>&1

    check "a path containing .. refuses" 2 present "$?"

    APP_ENV=development bash -c "
        set -euo pipefail
        source '$repository_root/scripts/lib/confirm.sh'
        confirm_target_name 'postgres'
    " >"$work_directory/output.txt" 2>&1

    check "a name without the project prefix refuses" 2 present "$?"

    APP_ENV=development bash -c "
        set -euo pipefail
        source '$repository_root/scripts/lib/confirm.sh'
        confirm_proceed 'destroy nothing in particular'
    " >"$work_directory/output.txt" 2>&1

    check "no declared target refuses" 2 present "$?"
}

run_manifest_case() {
    printf 'manifest case\n'

    APP_ENV=development run_piped --dry-run >/dev/null

    if grep --quiet "$work_directory/target.txt" "$work_directory/output.txt"; then
        report_pass "the manifest names the target before asking"
    else
        report_fail "the manifest did not name the target"
        printf '    output: %s\n' "$(tr '\n' ' ' <"$work_directory/output.txt")" >&2
    fi
}

# This file takes no flags of its own. The call below ends in the guard at the
# top of main, before a single case runs, so it costs one process and cannot
# recurse.
run_self_cases() {
    local code

    printf 'self cases\n'

    "${BASH_SOURCE[0]}" --dry-run >/dev/null 2>&1 </dev/null
    code="$?"

    if [ "$code" = "2" ]; then
        report_pass "a flag passed to this file refuses"
    else
        report_fail "a flag passed to this file refuses: expected exit 2, got $code"
    fi
}

main() {
    # Every case sets its own conditions, so a flag typed at this file would run
    # everything and print the same report, which reads as though the flag did
    # something.
    if [ "$#" -gt 0 ]; then
        printf "refused: unknown flag '%s', this file takes no flags\\n" "$1" >&2

        return 2
    fi

    # The work directory has to sit inside the repository, because the guard
    # under test refuses any path outside it. .tmp is gitignored at the root.
    mkdir -p "$repository_root/.tmp"
    work_directory="$(mktemp --directory "$repository_root/.tmp/confirm-test-XXXXXX")"

    write_subject

    run_guard_cases
    run_prompt_cases
    run_target_cases
    run_manifest_case
    run_self_cases

    printf '\n%s passed, %s failed\n' "$passed" "$failed"

    [ "$failed" -eq 0 ]
}

main "$@"

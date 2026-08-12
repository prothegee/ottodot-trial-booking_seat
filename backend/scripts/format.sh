#!/usr/bin/env bash
#
# Format every Go file in this stack.
#
# Two steps, and the second one is why this script exists at all. gofmt owns the
# layout and writes tabs, with no option to write anything else. This repository
# is written at four spaces everywhere, so each leading tab is expanded to four
# spaces afterwards.
#
# The consequence is stated rather than hidden: `gofmt -l` lists every file here,
# so it is not the formatting check for this stack. This script is. `go build`,
# `go vet`, and `go test` are unaffected, because none of them reads indentation.
#
# Usage:
#   scripts/format.sh              rewrite every file
#   scripts/format.sh --check      report what would change, write nothing
#
set -euo pipefail

stack_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$stack_root"

check_only=0

for argument in "$@"; do
    case "$argument" in
        --check)
            check_only=1
            ;;
        *)
            printf 'unknown option: %s\n' "$argument" >&2
            exit 2
            ;;
    esac
done

# expand -i converts leading whitespace only, so a tab inside a string or at the
# end of a line is left exactly as it was.
format_one() {
    local source="$1"

    gofmt "$source" | expand -i -t 4
}

changed=0

while IFS= read -r -d '' source; do
    formatted="$(format_one "$source")"

    if [ "$formatted" = "$(cat "$source")" ]; then
        continue
    fi

    changed=$((changed + 1))

    if [ "$check_only" -eq 1 ]; then
        printf '%s\n' "$source"

        continue
    fi

    printf '%s\n' "$formatted" > "$source"

# containers/ is pruned rather than filtered out of the results. Filtering still
# walks into it, and each service's data directory in there is written by a
# container user this one cannot read, so the check printed a page of permission
# errors before saying anything. No Go source lives under it either way.
done < <(find . -path ./containers -prune -o -name '*.go' -print0)

if [ "$check_only" -eq 1 ] && [ "$changed" -gt 0 ]; then
    printf '%d file(s) are not formatted, run scripts/format.sh\n' "$changed" >&2
    exit 1
fi

printf 'formatted %d file(s)\n' "$changed"

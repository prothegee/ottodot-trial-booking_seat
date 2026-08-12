#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: library
#
# Makes a stack's settings file from its committed template. Sourced, never
# executed.
#
# Two files are ignored by git and needed to run: backend/config.json and
# frontend/.env. Both hold values that belong to one machine, and one of them
# holds a signing key, so neither is committed. What is committed is the template
# beside it.
#
# A missing settings file is not an error here. It is the ordinary state of a
# fresh clone, and the answer is to say so loudly and copy the template, rather
# than to fail with a message about a value nobody has had a chance to set yet.
#
# Usage:
#   source "<repository root>/scripts/lib/settings.sh"
#   settings_ensure "$backend_root/config.json" "$backend_root/config.json.template"
#
# Note:
# - an existing file is never touched, not even to add a setting the template
#   grew later. Overwriting one would throw away whatever the machine had set,
#   and there is no way to tell a stale file from a deliberately edited one
# ---------------------------------------------------------------------------- #

# settings_ensure copies a template into place when the real file is missing.
#
# Param:
# target - the settings file the stack reads
# template - the committed file to copy when the target is not there
#
# Return:
# - 0 when the target exists, or was made from the template
# - 1 when neither the target nor the template is there, which is a broken clone
#   rather than a missing setting
settings_ensure() {
    local target="$1"
    local template="$2"

    if [ -f "$target" ]; then
        return 0
    fi

    if [ ! -f "$template" ]; then
        printf 'no settings file at %s and no template at %s to make one from.\n' \
            "$target" "$template" >&2

        return 1
    fi

    printf 'no settings file at %s.\n' "$target"
    printf '  copying %s\n' "$template"
    printf '  it holds development values only. Read it before pointing this at anything real.\n'

    cp "$template" "$target"
}

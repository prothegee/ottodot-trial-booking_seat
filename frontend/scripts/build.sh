#!/usr/bin/env bash
# ---------------------------------------------------------------------------- #
# class: safe
#
# Static build with the version and the commit injected.
#
# Neither is defaulted here. When nothing states them, vite.config.ts takes the
# version package.json declares and the commit git recorded, so a local build
# still says which code it came from. A reviewer has to be able to tell which
# build is on screen, and one place deciding that is easier to trust than two.
#
# Where the bundle points is .env, not this script. That file is made from
# .env.template on the first run and belongs to the machine it is on.
#
# Usage:
#   frontend/scripts/build.sh
#   BUILD_VERSION=1.4.0 frontend/scripts/build.sh
# ---------------------------------------------------------------------------- #

set -euo pipefail

frontend_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repository_root="$(cd "$frontend_root/.." && pwd)"

source "$repository_root/scripts/lib/settings.sh"

cd "$frontend_root"

# The build reads .env, so a missing one has to be dealt with here rather than
# by the dev server alone. Vite bakes these values into the bundle, which is why
# this file matters at build time and not at run time.
settings_ensure "$frontend_root/.env" "$frontend_root/.env.template" || exit 2

printf 'building version %s, commit %s\n' \
    "${BUILD_VERSION:-from package.json}" "${BUILD_COMMIT:-from git}"
printf 'the api address comes from %s/.env\n' "$frontend_root"

npm run build

printf 'static bundle is in %s/build\n' "$frontend_root"

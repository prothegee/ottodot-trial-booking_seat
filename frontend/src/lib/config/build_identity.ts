/**
 * Where the build identity comes from when nothing states it.
 *
 * This module runs in Node while vite starts, never in the browser.
 * `vite.config.ts` is the only file that may import it: a component importing it
 * would pull `node:fs` into the bundle.
 *
 * A footer reading "version dev, commit unknown" tells a reviewer nothing about
 * which build is on screen, which is the one job it has. Both facts are already
 * written down: the version in `package.json`, the commit in git. Neither has to
 * be typed into a settings file to be true, so neither is asked for.
 *
 * `BUILD_VERSION` and `BUILD_COMMIT` still take precedence when they are set. A
 * release names itself, and this is only what an unnamed build falls back to.
 */

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join } from "node:path";

/** Shown when the manifest states no version, or cannot be read at all. */
export const untaggedVersion = "dev";

/** Shown when git cannot answer, which includes not being a checkout. */
export const unknownCommit = "unknown";

/**
 * Reads the version this client already declares for itself.
 *
 * Param:
 * projectRoot - string (the directory holding package.json)
 *
 * Return:
 * - the version stated in package.json
 * - "dev", when the file is missing, unparseable, or states no version
 */
export function versionFromManifest(projectRoot: string): string {
    try {
        const manifest: unknown = JSON.parse(
            readFileSync(join(projectRoot, "package.json"), "utf8"),
        );
        const stated = (manifest as { version?: unknown }).version;

        if (typeof stated !== "string" || stated.trim() === "") return untaggedVersion;

        return stated.trim();
    } catch {
        return untaggedVersion;
    }
}

/**
 * Asks git which commit this build was made from.
 *
 * Note:
 * - Seven characters, matching what `frontend/scripts/build.sh` asks for, so a
 *   build made either way prints the same string.
 * - A failure is silent by design. A missing git, a tarball with no history, and
 *   a checkout with no commits are all the same answer to the only question this
 *   asks, and none of them is a reason to stop a build.
 *
 * Param:
 * projectRoot - string (any directory inside the checkout)
 *
 * Return:
 * - the short commit hash
 * - "unknown", when git is absent, fails, or answers with nothing
 */
export function commitFromGit(projectRoot: string): string {
    try {
        const answer = execFileSync("git", ["rev-parse", "--short=7", "HEAD"], {
            cwd: projectRoot,
            encoding: "utf8",
            stdio: ["ignore", "pipe", "ignore"],
        });

        return answer.trim() === "" ? unknownCommit : answer.trim();
    } catch {
        return unknownCommit;
    }
}

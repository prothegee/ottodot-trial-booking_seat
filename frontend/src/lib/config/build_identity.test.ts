import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, describe, expect, test } from "vitest";

import {
    commitFromGit,
    unknownCommit,
    untaggedVersion,
    versionFromManifest,
} from "$lib/config/build_identity";

/** Vitest runs from the frontend root, which is where package.json lives. */
const projectRoot = process.cwd();

/** A directory with no manifest in it and no git history above it. */
const emptyDirectory = mkdtempSync(join(tmpdir(), "ottodot-build-identity-"));

afterAll(() => {
    rmSync(emptyDirectory, { recursive: true, force: true });
});

describe("the build identity fallbacks", () => {
    test("unit: the version is the one package.json already states", () => {
        const manifest = JSON.parse(readFileSync(join(projectRoot, "package.json"), "utf8"));

        expect(versionFromManifest(projectRoot)).toBe(manifest.version);
    });

    test("unit: that version is a real number, not the untagged placeholder", () => {
        // The point of reading the manifest is that a reviewer can tell one build
        // from another. A version that still says "dev" would mean the manifest
        // was never filled in and the footer is back to saying nothing.
        expect(versionFromManifest(projectRoot)).not.toBe(untaggedVersion);
        expect(versionFromManifest(projectRoot)).toMatch(/^\d+\.\d+\.\d+/);
    });

    test("edge: a directory with no manifest falls back to the untagged version", () => {
        expect(versionFromManifest(emptyDirectory)).toBe(untaggedVersion);
    });

    test("unit: the commit is the short hash of this checkout", () => {
        expect(commitFromGit(projectRoot)).toMatch(/^[0-9a-f]{7}$/);
    });

    test("edge: a directory outside any checkout falls back to the unknown commit", () => {
        expect(commitFromGit(emptyDirectory)).toBe(unknownCommit);
    });

    test("behaviour: neither fallback ever answers with an empty string", () => {
        // Both values are printed into a sentence. An empty one would render as
        // "version , commit " and read as a broken page rather than as a build
        // that did not name itself.
        expect(versionFromManifest(emptyDirectory)).not.toBe("");
        expect(commitFromGit(emptyDirectory)).not.toBe("");
    });
});

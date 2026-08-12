import { describe, expect, test } from "vitest";

import { commitFromGit, versionFromManifest } from "$lib/config/build_identity";
import { apiBaseUrl, buildIdentity } from "$lib/config/environment";

describe("the injected environment", () => {
    test("unit: the api base url arrives from the build, not from a component", () => {
        expect(apiBaseUrl).toMatch(/^https?:\/\//);
    });

    test("unit: the build identity carries a version and a commit", () => {
        expect(buildIdentity.version).not.toBe("");
        expect(buildIdentity.commit).not.toBe("");
    });

    test("edge: an unset variable falls back to the local development value", () => {
        // A clone with no environment file still has to run, so the fallbacks
        // point at the local api and let the build name itself from what the
        // repository already records. The expectations mirror vite.config.ts,
        // which is what makes this test fail if a fallback there is ever quietly
        // changed.
        const projectRoot = process.cwd();

        expect(apiBaseUrl).toBe(process.env.API_BASE_URL ?? "http://127.0.0.1:9000");
        expect(buildIdentity.version).toBe(
            process.env.BUILD_VERSION ?? versionFromManifest(projectRoot),
        );
        expect(buildIdentity.commit).toBe(process.env.BUILD_COMMIT ?? commitFromGit(projectRoot));
    });

    test("behaviour: an unnamed build still says which code it is", () => {
        // The failure this guards is a footer reading "version dev, commit
        // unknown" on a build made from a checkout that knows both.
        expect(buildIdentity.version).not.toBe("dev");
        expect(buildIdentity.commit).not.toBe("unknown");
    });
});

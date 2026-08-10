import { describe, expect, test } from "vitest";

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
        // point at the local api and say plainly that the build is untagged.
        // The expectations mirror vite.config.ts, which is what makes this test
        // fail if a fallback there is ever quietly changed.
        expect(apiBaseUrl).toBe(process.env.API_BASE_URL ?? "http://127.0.0.1:9000");
        expect(buildIdentity.version).toBe(process.env.BUILD_VERSION ?? "dev");
        expect(buildIdentity.commit).toBe(process.env.BUILD_COMMIT ?? "unknown");
    });
});

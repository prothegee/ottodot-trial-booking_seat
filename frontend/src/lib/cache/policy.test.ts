import { describe, expect, test } from "vitest";

import { freshWindowMilliseconds, staleWindowMilliseconds, tierForAge } from "$lib/cache/policy";

describe("the cache freshness policy", () => {
    test("unit: under five seconds is fresh, so nothing is sent", () => {
        expect(tierForAge(0)).toBe("fresh");
        expect(tierForAge(1)).toBe("fresh");
        expect(tierForAge(4_999)).toBe("fresh");
    });

    test("unit: five to thirty seconds is stale, so the body still renders", () => {
        expect(tierForAge(5_001)).toBe("stale");
        expect(tierForAge(10_000)).toBe("stale");
        expect(tierForAge(29_999)).toBe("stale");
    });

    test("unit: past thirty seconds is cold, so nothing renders before the answer", () => {
        expect(tierForAge(30_001)).toBe("cold");
        expect(tierForAge(60_000)).toBe("cold");
    });

    test("edge: exactly five seconds counts as stale", () => {
        // Both boundaries are exclusive at the low end. Sitting on one costs a
        // conditional request, which is the cheap side of the mistake.
        expect(tierForAge(freshWindowMilliseconds)).toBe("stale");
    });

    test("edge: exactly thirty seconds counts as cold", () => {
        expect(tierForAge(staleWindowMilliseconds)).toBe("cold");
    });

    test("edge: an entry from the future is cold rather than fresh", () => {
        // A negative age means the system clock moved backwards after the
        // entry was written, so its timestamp says nothing about its age.
        expect(tierForAge(-1)).toBe("cold");
        expect(tierForAge(-60_000)).toBe("cold");
    });
});

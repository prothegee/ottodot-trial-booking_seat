import { describe, expect, test } from "vitest";

import type { TelemetryRoute } from "$lib/telemetry/event";
import { measurePageLoad } from "$lib/telemetry/page_load";

/** A clock a case moves by hand, so a duration is exact rather than roughly right. */
function stubClock(start = 1000) {
    let now = start;

    return {
        now: () => now,
        advance(byMs: number) {
            now += byMs;
        },
    };
}

/** Collects what was reported, so a case asserts on values rather than on a post. */
function collector() {
    const reported: Array<{ route: TelemetryRoute; seconds: number }> = [];

    return {
        reported,
        report(route: TelemetryRoute, seconds: number) {
            reported.push({ route, seconds });
        },
    };
}

describe("measuring how long a screen took to become usable", () => {
    test("integration: the reading is the gap between the start and the call", () => {
        const clock = stubClock();
        const sink = collector();

        const usable = measurePageLoad("/", { now: clock.now, report: sink.report });

        clock.advance(1500);

        usable();

        expect(sink.reported).toEqual([{ route: "/", seconds: 1.5 }]);
    });

    test("edge: a second call reports nothing", () => {
        // A screen whose data arrives in two parts would otherwise report the
        // second part as a second page load, and the histogram would fill with
        // numbers nobody meant.
        const clock = stubClock();
        const sink = collector();

        const usable = measurePageLoad("/status", { now: clock.now, report: sink.report });

        clock.advance(200);
        usable();

        clock.advance(200);
        usable();

        expect(sink.reported).toHaveLength(1);
    });

    test("edge: the route is a pattern rather than a path", () => {
        // A path carries a booking identifier, and a label carrying one is a
        // series per booking. The type is what makes the wrong value impossible
        // to pass, and this is the case that says why.
        const clock = stubClock();
        const sink = collector();

        measurePageLoad("/booking/[bookingId]", { now: clock.now, report: sink.report })();

        expect(sink.reported[0].route).toBe("/booking/[bookingId]");
        expect(sink.reported[0].route).not.toContain("0192");
    });

    test("edge: a reading of zero is still reported", () => {
        // A screen that was usable immediately is a real measurement and the
        // best one there is. Dropping it would bias every quantile upwards.
        const clock = stubClock();
        const sink = collector();

        measurePageLoad("/sign-in", { now: clock.now, report: sink.report })();

        expect(sink.reported).toEqual([{ route: "/sign-in", seconds: 0 }]);
    });

    test("unit: the reading is in seconds, because that is what the histogram holds", () => {
        const clock = stubClock();
        const sink = collector();

        const usable = measurePageLoad("/", { now: clock.now, report: sink.report });

        clock.advance(250);
        usable();

        expect(sink.reported[0].seconds).toBeCloseTo(0.25);
    });
});

import { describe, expect, test } from "vitest";

import { remainingFor } from "$lib/booking/countdown";

/** One fixed instant, so nothing here depends on when it runs. */
const now = Date.parse("2026-08-11T09:00:00.000Z");

/** A deadline a given number of milliseconds from that instant. */
function deadlineIn(milliseconds: number): string {
    return new Date(now + milliseconds).toISOString();
}

describe("what is left of a hold", () => {
    test("unit: a deadline in the future renders as minutes and seconds", () => {
        const left = remainingFor(deadlineIn(9 * 60 * 1000 + 5000), now);

        expect(left.expired).toBe(false);
        expect(left.label).toBe("09:05");
        expect(left.milliseconds).toBe(9 * 60 * 1000 + 5000);
    });

    test("unit: both parts are padded, so the label never changes width", () => {
        expect(remainingFor(deadlineIn(65 * 1000), now).label).toBe("01:05");
        expect(remainingFor(deadlineIn(9 * 1000), now).label).toBe("00:09");
    });

    test("unit: a hold longer than ten minutes still reads as minutes and seconds", () => {
        expect(remainingFor(deadlineIn(15 * 60 * 1000), now).label).toBe("15:00");
    });

    test("edge: a deadline exactly now is already expired", () => {
        // Inclusive, matching the backend, where a hold ending on this instant
        // is one the worker may already have swept.
        const left = remainingFor(deadlineIn(0), now);

        expect(left.expired).toBe(true);
        expect(left.label).toBe("00:00");
    });

    test("edge: a deadline in the past reads as zero, never as a negative", () => {
        // A parent seeing "-01:12 left" learns nothing except that this was not
        // thought about.
        const left = remainingFor(deadlineIn(-72 * 1000), now);

        expect(left.expired).toBe(true);
        expect(left.milliseconds).toBe(0);
        expect(left.label).toBe("00:00");
    });

    test("edge: one millisecond left still counts as live and reads as one second", () => {
        // Rounding down here would show "00:00" beside a control that still
        // works, which is the one way this display can contradict itself.
        const left = remainingFor(deadlineIn(1), now);

        expect(left.expired).toBe(false);
        expect(left.label).toBe("00:01");
    });

    test("edge: a booking with no deadline reads as expired rather than throwing", () => {
        // The api sends null for a booking that is not holding, and a screen
        // that threw on it would show a blank page instead of a status.
        const left = remainingFor(null, now);

        expect(left.expired).toBe(true);
        expect(left.label).toBe("00:00");
    });

    test("edge: a deadline the browser cannot parse reads as expired rather than NaN", () => {
        const left = remainingFor("not a date", now);

        expect(left.expired).toBe(true);
        expect(left.label).toBe("00:00");
        expect(Number.isNaN(left.milliseconds)).toBe(false);
    });

    test("edge: an empty deadline is treated the same as an absent one", () => {
        expect(remainingFor("", now).expired).toBe(true);
    });

    test("unit: the countdown decreases as the instant moves forward", () => {
        const deadline = deadlineIn(60 * 1000);

        expect(remainingFor(deadline, now).label).toBe("01:00");
        expect(remainingFor(deadline, now + 30 * 1000).label).toBe("00:30");
        expect(remainingFor(deadline, now + 59 * 1000).label).toBe("00:01");
        expect(remainingFor(deadline, now + 60 * 1000).expired).toBe(true);
    });
});

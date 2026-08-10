import { render, screen } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import HoldCountdown from "./HoldCountdown.svelte";

/** One fixed instant, so nothing here depends on when it runs. */
const now = Date.parse("2026-08-11T09:00:00.000Z");

/** A deadline a given number of milliseconds from that instant. */
function deadlineIn(milliseconds: number): string {
    return new Date(now + milliseconds).toISOString();
}

describe("the hold countdown", () => {
    beforeEach(() => {
        vi.useFakeTimers();
        vi.setSystemTime(now);
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    test("integration: a live hold shows what is left to pay", () => {
        render(HoldCountdown, { props: { deadline: deadlineIn(10 * 60 * 1000) } });

        expect(screen.getByTestId("hold-countdown-label")).toHaveTextContent("10:00 left to pay");
    });

    test("integration: the label moves as the clock does", async () => {
        render(HoldCountdown, { props: { deadline: deadlineIn(10 * 60 * 1000) } });

        await vi.advanceTimersByTimeAsync(60 * 1000);

        expect(screen.getByTestId("hold-countdown-label")).toHaveTextContent("09:00 left to pay");
    });

    test("edge: a deadline already past renders as ended rather than as a negative", () => {
        render(HoldCountdown, { props: { deadline: deadlineIn(-30 * 1000) } });

        expect(screen.getByTestId("hold-countdown-label")).toHaveTextContent("Your hold has ended");
    });

    test("edge: a booking with no deadline renders as ended rather than blank", () => {
        render(HoldCountdown, { props: { deadline: null } });

        expect(screen.getByTestId("hold-countdown-label")).toHaveTextContent("Your hold has ended");
    });

    test("behaviour: reaching zero reports it once, not on every tick after", async () => {
        // What this reports does is ask the api what really happened. Asking
        // every second would be a poll nobody asked for.
        let reported = 0;

        render(HoldCountdown, {
            props: { deadline: deadlineIn(2000), onExpire: () => (reported += 1) },
        });

        await vi.advanceTimersByTimeAsync(10 * 1000);

        expect(reported).toBe(1);
    });

    test("edge: a hold that had already ended on arrival still reports once", async () => {
        // A parent opening the link an hour later has to be told, and the
        // screen has to ask the api, exactly as if they had watched it run out.
        let reported = 0;

        render(HoldCountdown, {
            props: { deadline: deadlineIn(-60 * 1000), onExpire: () => (reported += 1) },
        });

        await vi.advanceTimersByTimeAsync(1000);

        expect(reported).toBe(1);
    });

    test("edge: a tab that was suspended shows the right number on its first frame back", async () => {
        // The remainder is worked out from the deadline and the clock on every
        // render, so nothing has to catch up one second at a time.
        render(HoldCountdown, { props: { deadline: deadlineIn(10 * 60 * 1000) } });

        vi.setSystemTime(now + 5 * 60 * 1000);

        await vi.advanceTimersByTimeAsync(1000);

        expect(screen.getByTestId("hold-countdown-label")).toHaveTextContent("04:59 left to pay");
    });
});

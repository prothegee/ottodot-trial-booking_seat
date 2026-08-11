import { render, screen, waitFor } from "@testing-library/svelte";
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";

import type { Booking, PayRequest } from "$lib/api/types";
import { mockCaptchaToken } from "$lib/booking/bot_signals";
import { api } from "$lib/session/client";
import { classMutator } from "$lib/session/cached_api";
import { booking } from "$lib/stores/booking";
import PayPage from "../routes/pay/[bookingId]/+page.svelte";

/**
 * Simulation F7: the honeypot and the fill timer.
 *
 *     form mounts, timer starts
 *     fields filled
 *     submit
 *     payload carries the honeypot value and the elapsed milliseconds
 *     backend decides, the frontend does not block
 *
 * Asserts: the honeypot input is present, hidden, empty by default, excluded
 * from the accessibility tree and from the keyboard order, and the elapsed time
 * is a real measurement rather than a constant.
 *
 * The last assertion is the one that matters. A hard coded number would pass the
 * backend's check while proving nothing about who filled the form in, which is
 * the same as not sending it at all. So the case holds the form open for a known
 * stretch of time and reads the number back.
 */

const classId = "0192a000-0000-7000-8000-000000000021";
const studentId = "0192a000-0000-7000-8000-000000000011";
const bookingId = "0192a000-0000-7000-8000-000000000031";

/** The instant this runs at, so the countdown is not a moving target. */
const now = Date.parse("2026-08-11T09:00:00.000Z");

vi.mock("$app/navigation", () => ({
    goto: vi.fn(() => Promise.resolve()),
}));

vi.mock("$app/state", () => ({
    page: { params: { bookingId: "0192a000-0000-7000-8000-000000000031" } },
}));

vi.mock("$lib/session/cached_api", () => ({
    classReader: { read: vi.fn() },
    classMutator: { send: vi.fn() },
}));

vi.mock("$lib/session/client", () => ({
    api: { request: vi.fn() },
}));

const heldBooking: Booking = {
    id: bookingId,
    student_id: studentId,
    class_id: classId,
    status: "pending_payment",
    seat_no: null,
    hold_expires_at: new Date(now + 10 * 60 * 1000).toISOString(),
};

const confirmedBooking: Booking = {
    ...heldBooking,
    status: "confirmed",
    seat_no: 2,
    hold_expires_at: null,
};

/** The body the payment call was sent with. */
function sentPayment(): PayRequest {
    const call = vi.mocked(classMutator.send).mock.calls.at(-1);

    if (call === undefined) {
        throw new Error("no payment was sent");
    }

    return call[0].body as PayRequest;
}

/**
 * Mounts the payment screen, waits a known stretch, submits, and reports the
 * elapsed time the payload carried.
 */
async function measureOneSubmission(holdOpenMs: number): Promise<number> {
    booking.reset();
    vi.mocked(classMutator.send).mockClear();

    const screenUnderTest = render(PayPage);

    await waitFor(() => {
        expect(screen.getByTestId("payment-submit")).not.toBeDisabled();
    });

    await vi.advanceTimersByTimeAsync(holdOpenMs);

    screen.getByTestId("payment-submit").click();

    await waitFor(() => {
        expect(vi.mocked(classMutator.send)).toHaveBeenCalled();
    });

    const measured = sentPayment().filled_in_ms;

    screenUnderTest.unmount();

    return measured;
}

describe("simulation F7: the honeypot and the fill timer", () => {
    beforeEach(() => {
        sessionStorage.clear();

        vi.useFakeTimers({ shouldAdvanceTime: true });
        vi.setSystemTime(now);

        booking.reset();

        vi.mocked(api.request).mockReset().mockResolvedValue(heldBooking);
        vi.mocked(classMutator.send).mockReset().mockResolvedValue(confirmedBooking);
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    test("unit: the honeypot is present, empty, and out of reach of a person", async () => {
        const { container } = render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("payment-form")).toBeInTheDocument();
        });

        const trap = container.querySelector<HTMLInputElement>('input[name="website"]');

        if (trap === null) {
            throw new Error("the honeypot is missing, so this layer does nothing at all");
        }

        // Empty by default. A field with a value would be filled in by every
        // parent and would refuse every one of them.
        expect(trap.value).toBe("");

        // Out of the keyboard order, so nobody tabs into it by accident.
        expect(trap).toHaveAttribute("tabindex", "-1");

        // Out of the browser's own reach, so a password manager or an autofill
        // does not put something there on the parent's behalf.
        expect(trap).toHaveAttribute("autocomplete", "off");

        // Out of the accessibility tree, so it is never read out.
        expect(trap.closest("[aria-hidden='true']")).not.toBeNull();
    });

    test("edge: the honeypot is a real field rather than a hidden input", async () => {
        // `type="hidden"` is the first thing a script looks for, so the field is
        // laid out and moved off screen instead. It is still invisible to a
        // person and still invisible to assistive technology.
        const { container } = render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("payment-form")).toBeInTheDocument();
        });

        const trap = container.querySelector<HTMLInputElement>('input[name="website"]');

        expect(trap?.getAttribute("type")).toBe("text");
    });

    test("integration: the payload carries the honeypot and the elapsed time", async () => {
        render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("payment-submit")).not.toBeDisabled();
        });

        // The challenge widget answers after a moment, the way a real one does.
        await vi.advanceTimersByTimeAsync(500);

        screen.getByTestId("payment-submit").click();

        await waitFor(() => {
            expect(vi.mocked(classMutator.send)).toHaveBeenCalled();
        });

        const body = sentPayment();

        expect(body.website).toBe("");
        expect(body.captcha_token).toBe(mockCaptchaToken);
        expect(typeof body.filled_in_ms).toBe("number");
    });

    test("behaviour: the elapsed time is a measurement rather than a constant", async () => {
        // Two screens, each held open for a different stretch, must not report
        // the same number. A constant would pass the backend and prove nothing
        // about who filled the form in.
        //
        // They are two mounts rather than two clicks on one, because a settled
        // payment closes the control, which is the correct behaviour and would
        // make a second click on the same screen do nothing.
        const quick = await measureOneSubmission(2000);
        const slow = await measureOneSubmission(8000);

        expect(slow).toBeGreaterThan(quick);
    });

    test("behaviour: a filled honeypot is still sent, because this side blocks nothing", async () => {
        // The backend owns the decision. A client that refused its own
        // submission would teach a script exactly which value to change, and
        // would refuse a slow person on a bad connection for no reason.
        const { container } = render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("payment-submit")).not.toBeDisabled();
        });

        const trap = container.querySelector<HTMLInputElement>('input[name="website"]');

        if (trap === null) {
            throw new Error("the honeypot is missing");
        }

        trap.value = "http://cheap-pills.example";
        trap.dispatchEvent(new Event("input", { bubbles: true }));

        screen.getByTestId("payment-submit").click();

        await waitFor(() => {
            expect(vi.mocked(classMutator.send)).toHaveBeenCalled();
        });

        expect(sentPayment().website).toBe("http://cheap-pills.example");
    });

    test("edge: none of the three signals is anything about the parent", async () => {
        // The whole payload is inspected rather than the three fields, because
        // the risk is a field somebody adds later, not the ones here now.
        render(PayPage);

        await waitFor(() => {
            expect(screen.getByTestId("payment-submit")).not.toBeDisabled();
        });

        await vi.advanceTimersByTimeAsync(500);

        screen.getByTestId("payment-submit").click();

        await waitFor(() => {
            expect(vi.mocked(classMutator.send)).toHaveBeenCalled();
        });

        const encoded = JSON.stringify(sentPayment());

        for (const leak of ["@", "email", "full_name", "cookie", "jwt", "parent_id", studentId]) {
            expect(encoded.toLowerCase()).not.toContain(leak.toLowerCase());
        }
    });
});

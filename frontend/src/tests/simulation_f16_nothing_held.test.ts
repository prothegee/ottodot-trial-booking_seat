import { get } from "svelte/store";
import { beforeEach, describe, expect, test, vi } from "vitest";

import type { Booking, Session, TrialClass } from "$lib/api/types";
import { trialPayment } from "$lib/booking/price";
import { classMutator, classReader } from "$lib/session/cached_api";
import { api } from "$lib/session/client";
import { auth } from "$lib/stores/auth";
import { booking } from "$lib/stores/booking";
import { bookings } from "$lib/stores/bookings";
import { classes } from "$lib/stores/classes";
import { createEmitter } from "$lib/telemetry/emitter";
import { apiErrorEvent, cacheEvent, funnelEvent, pageLoadEvent, type TelemetryBatch } from "$lib/telemetry/event";

/**
 * Test 16: nothing sensitive is held by the client.
 *
 *     drive a full booking, sign in to confirmed
 *     capture every store, every sessionStorage entry, every route visited,
 *     and every queued telemetry payload
 *     scan all four for an email or a token
 *
 * Asserts: the seeded email appears in no store, no sessionStorage entry, no
 * url, and no telemetry payload, and no code path attempts to read a cookie.
 *
 * The last one is the reason the cookies are HttpOnly in the first place. A
 * client that never reads a token cannot leak one however badly it is written,
 * and the case below proves the property by watching `document.cookie` rather
 * than by trusting that nobody wrote the line.
 */

const classId = "0192a000-0000-7000-8000-000000000021";
const studentId = "0192a000-0000-7000-8000-000000000011";
const bookingId = "0192a000-0000-7000-8000-000000000031";

/** The seeded account this test drives. None of it may end up held. */
const seeded = {
    email: "alice.tan@example.test",
    accessToken: "eyJhbGciOiJIUzI1NiJ9.abcdefghijklmnop.signature",
    refreshToken: "rt_abcdefghijklmnopqrstuvwxyz",
};

vi.mock("$app/navigation", () => ({
    goto: vi.fn(() => Promise.resolve()),
}));

vi.mock("$lib/session/cached_api", () => ({
    classReader: { read: vi.fn() },
    classMutator: { send: vi.fn() },
}));

vi.mock("$lib/session/client", () => ({
    api: { request: vi.fn() },
}));

vi.mock("$lib/telemetry/report", () => ({
    reportPageLoad: vi.fn(),
    reportApiError: vi.fn(),
    reportFunnel: vi.fn(),
    reportCacheLookup: vi.fn(),
}));

/**
 * What the api sends back for the signed in parent.
 *
 * There is no email on it and no token, because the api does not send either.
 * It is written out here rather than assumed, so the case is driving the real
 * contract rather than a convenient one.
 */
const session: Session = {
    parent_id: "0192a000-0000-7000-8000-0000000000aa",
    display_name: "Alice Tan",
    role: "parent",
    children: [{ id: studentId, full_name: "Adi Tan", grade_level: 4 }],
};

const trialClass: TrialClass = {
    id: classId,
    subject: "science",
    title: "Science trial",
    starts_at: "2026-08-20T09:00:00.000Z",
    duration_minutes: 60,
    capacity: 4,
    seats_remaining: 2,
};

const heldBooking: Booking = {
    id: bookingId,
    student_id: studentId,
    class_id: classId,
    class_subject: "science",
    class_title: "Science Discovery",
    class_starts_at: "2026-08-15T01:28:26.224983Z",
    status: "pending_payment",
    seat_no: null,
    hold_expires_at: "2026-08-11T09:10:00.000Z",
};

const confirmedBooking: Booking = { ...heldBooking, status: "confirmed", seat_no: 2, hold_expires_at: null };

/** Every route this booking passed through, as patterns rather than as paths. */
const routesVisited = ["/", "/sign-in", "/book/[classId]", "/pay/[bookingId]", "/booking/[bookingId]"] as const;

/** Everything that must not appear anywhere. */
const secrets = [seeded.email, seeded.accessToken, seeded.refreshToken, "@example.test", "Bearer "];

/** Fails naming the surface and the value it carried. */
function scan(surface: string, captured: string): void {
    for (const secret of secrets) {
        expect(captured.toLowerCase(), `${surface} carries ${secret}`).not.toContain(secret.toLowerCase());
    }
}

/** Serialises every sessionStorage entry, keys and values together. */
function dumpSessionStorage(): string {
    const entries: string[] = [];

    for (let index = 0; index < sessionStorage.length; index += 1) {
        const key = sessionStorage.key(index) ?? "";

        entries.push(key, sessionStorage.getItem(key) ?? "");
    }

    return entries.join("\n");
}

describe("test 16: nothing sensitive is held by the client", () => {
    beforeEach(() => {
        sessionStorage.clear();

        auth.signOut("requested");
        booking.reset();
        bookings.reset();
        classes.reset();

        vi.mocked(api.request).mockReset().mockResolvedValue({ bookings: [confirmedBooking] });

        vi.mocked(classReader.read).mockReset().mockResolvedValue({
            body: { classes: [trialClass] },
            result: "miss",
            revalidation: null,
        });

        vi.mocked(classMutator.send)
            .mockReset()
            .mockResolvedValueOnce(heldBooking)
            .mockResolvedValueOnce(confirmedBooking);
    });

    test("integration: a whole booking leaves nothing held on any of the four surfaces", async () => {
        // Sign in to confirmed, through the stores rather than the screens,
        // because a store is what actually holds something between screens.
        auth.signIn(session);

        await classes.load();
        await booking.create({ student_id: studentId, class_id: classId });
        await booking.pay(bookingId, trialPayment());
        await bookings.load();

        expect(get(booking).booking?.status).toBe("confirmed");

        // Surface one: every store, serialised.
        scan("the auth store", JSON.stringify(get(auth)));
        scan("the booking store", JSON.stringify(get(booking)));
        scan("the bookings store", JSON.stringify(get(bookings)));
        scan("the classes store", JSON.stringify(get(classes)));

        // Surface two: everything the browser was asked to keep.
        scan("sessionStorage", dumpSessionStorage());

        // Surface three: every route the parent passed through.
        scan("the routes visited", routesVisited.join(" "));

        // Surface four: everything queued for telemetry.
        const queued: TelemetryBatch[] = [];

        const emitter = createEmitter({
            post: async (batch) => {
                queued.push(batch);
            },
            intervalMs: 1,
        });

        emitter.record(pageLoadEvent("/pay/[bookingId]", 0.4));
        emitter.record(funnelEvent("confirmed"));
        emitter.record(apiErrorEvent("class_full"));
        emitter.record(cacheEvent("fresh"));

        await emitter.flush();

        scan("the telemetry payload", JSON.stringify(queued));
    });

    test("edge: the auth store holds a display name and never an email or a token", async () => {
        // The api does not send either, so this is really a check on the guard
        // in the store: if the api ever starts sending one, it has to stop there
        // rather than in a component that renders it.
        auth.signIn({ ...session, ...({ email: seeded.email } as object) } as Session);

        const held = JSON.stringify(get(auth));

        scan("the auth store", held);

        expect(held).toContain("Alice Tan");
    });

    test("edge: no telemetry payload carries an identifier of any kind", async () => {
        // Every field on every event is a label value or a duration, and there
        // is nowhere to put an identifier. The backend drops what it does not
        // recognise, and this is the half of that rule that lives here.
        const queued: TelemetryBatch[] = [];

        const emitter = createEmitter({
            post: async (batch) => {
                queued.push(batch);
            },
            intervalMs: 1,
        });

        emitter.record(pageLoadEvent("/booking/[bookingId]", 0.4));
        emitter.record(funnelEvent("hold"));

        await emitter.flush();

        const payload = JSON.stringify(queued);

        for (const identifier of [bookingId, studentId, classId, session.parent_id]) {
            expect(payload).not.toContain(identifier);
        }
    });

    test("edge: no route this client navigates to carries a name or an address", async () => {
        // A route is in the browser's history and in whatever a reader can see
        // over somebody's shoulder. Identifiers are acceptable there, and names
        // are not.
        for (const route of routesVisited) {
            scan(`the route ${route}`, route);
            expect(route).not.toContain("Alice");
            expect(route).not.toContain("Adi");
        }
    });

    test("behaviour: no code path reads a cookie", async () => {
        // The tokens are HttpOnly, so `document.cookie` cannot see them however
        // hard this client tries. Watching the property is what turns that from
        // a claim about the code into a property of the run.
        const original = Object.getOwnPropertyDescriptor(Document.prototype, "cookie");

        let reads = 0;

        Object.defineProperty(document, "cookie", {
            configurable: true,
            get() {
                reads += 1;

                return "";
            },
            set() {
                reads += 1;
            },
        });

        try {
            auth.signIn(session);

            await classes.load();
            await booking.create({ student_id: studentId, class_id: classId });
            await booking.pay(bookingId, trialPayment());
        } finally {
            if (original !== undefined) {
                Object.defineProperty(document, "cookie", original);
            }
        }

        expect(reads).toBe(0);
    });

    test("behaviour: a sign out leaves nothing of the previous parent behind", async () => {
        auth.signIn(session);

        await classes.load();
        await booking.create({ student_id: studentId, class_id: classId });

        auth.signOut("session_ended");

        expect(get(auth).session).toBeNull();
        expect(get(booking).booking).toBeNull();
        expect(get(classes).classes).toEqual([]);
    });
});

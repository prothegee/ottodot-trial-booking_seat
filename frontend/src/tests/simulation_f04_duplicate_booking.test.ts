import { beforeEach, describe, expect, test } from "vitest";
import { get } from "svelte/store";

import { createApiClient } from "$lib/api/client";
import { createFakeTransport } from "$lib/api/transport_fake";
import { createCacheAwareMutator } from "$lib/cache/mutation";
import { createCachedReader } from "$lib/cache/read_through";
import { createCacheStore } from "$lib/cache/store";
import { bookingsPath, createBookingStore } from "$lib/stores/booking";
import { classListPath, createClassesStore } from "$lib/stores/classes";

/**
 * Test 4: the same child is booked into the same class twice.
 *
 *     parent -> client: book the same child into the same class
 *     client -> api: create the booking
 *     api -> client: 409 already_booked, with the existing booking id
 *     client -> parent: already booked, and a link to that booking
 *
 * Asserts: no second booking is attempted, and the link resolves to the booking
 * the child already has.
 *
 * The rule itself belongs to the database, on a partial unique index over the
 * live statuses. Nothing here checks first and then books, because a check
 * followed by a write is the bug this whole exercise is about. The client sends
 * the request and reads the answer.
 */

const classId = "0192a000-0000-7000-8000-000000000021";
const studentId = "0192a000-0000-7000-8000-000000000011";
const existingBookingId = "0192a000-0000-7000-8000-000000000099";

const classList = {
    classes: [
        {
            id: classId,
            subject: "science",
            title: "Science Discovery",
            starts_at: "2026-08-14T09:00:00Z",
            duration_minutes: 60,
            capacity: 4,
            seats_remaining: 2,
        },
    ],
};

/** The failure the api sends, carrying the booking the child already has. */
const duplicateBody = {
    error: {
        code: "already_booked",
        message: "this child already has a live booking for this class",
        booking_id: existingBookingId,
    },
};

function newStage() {
    const transport = createFakeTransport((request) => {
        if (request.method === "GET" && request.path === classListPath) {
            return { status: 200, body: classList, headers: { etag: '"v1"' } };
        }

        if (request.method === "POST" && request.path === bookingsPath) {
            return { status: 409, body: duplicateBody };
        }

        return { status: 404, body: { error: { code: "invalid_request", message: "no such route" } } };
    });

    const client = createApiClient({ transport, onSignOut: () => {} });
    const store = createCacheStore();

    return {
        transport,
        classes: createClassesStore({ reader: createCachedReader({ client, store }) }),
        booking: createBookingStore({ mutator: createCacheAwareMutator({ client, store }), cache: store }),
    };
}

describe("test 4: this child already has a booking for this class", () => {
    beforeEach(() => {
        sessionStorage.clear();
    });

    test("behaviour: the parent is told, and given the booking that already exists", async () => {
        const stage = newStage();

        const refused = await stage.booking.create({ student_id: studentId, class_id: classId });

        expect(refused).toBeNull();

        const failure = get(stage.booking).failure;

        expect(failure?.kind).toBe("AlreadyBooked");
        expect(failure?.message).toBe("this child already has a booking for this class");
        expect(failure?.bookingId).toBe(existingBookingId);
    });

    test("behaviour: no second booking is attempted", async () => {
        // A client that retried on a 409 would be arguing with the one place
        // that knows, and on a different day it would produce the duplicate the
        // index exists to stop.
        const stage = newStage();

        await stage.booking.create({ student_id: studentId, class_id: classId });

        expect(stage.transport.callsTo("POST", bookingsPath)).toHaveLength(1);
    });

    test("edge: nothing is checked before the write, so there is no window to lose", async () => {
        // Reading first and then writing is the shape this whole exercise is
        // about. The only call made here is the write.
        const stage = newStage();

        await stage.booking.create({ student_id: studentId, class_id: classId });

        expect(stage.transport.calls).toHaveLength(1);
        expect(stage.transport.calls[0].method).toBe("POST");
    });

    test("edge: a duplicate says nothing about seat counts, so the cached list is left alone", async () => {
        // The class did not fill and no seat moved. Dropping the entry would
        // cost a request to learn a number that has not changed.
        const stage = newStage();

        await stage.classes.load();
        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.classes.load();

        expect(get(stage.classes).lastResult).toBe("fresh");
        expect(stage.transport.callsTo("GET", classListPath)).toHaveLength(1);
    });
});

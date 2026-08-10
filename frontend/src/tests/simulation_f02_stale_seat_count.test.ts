import { beforeEach, describe, expect, test } from "vitest";
import { get } from "svelte/store";

import { createApiClient } from "$lib/api/client";
import { createFakeTransport, errorBody } from "$lib/api/transport_fake";
import { createCacheAwareMutator } from "$lib/cache/mutation";
import { createCachedReader } from "$lib/cache/read_through";
import { createCacheStore } from "$lib/cache/store";
import { bookingsPath, createBookingStore } from "$lib/stores/booking";
import { classListPath, createClassesStore } from "$lib/stores/classes";

/**
 * Simulation F2: the seat count was stale, and the class was full by the time
 * the parent submitted.
 *
 *     note over client: the cached list says one seat left
 *     parent -> client: submit the booking
 *     client -> api: create the booking
 *     api -> client: 409 class_full
 *     client -> api: read the class list again
 *     api -> client: no seats left
 *     client -> parent: the class filled while you were choosing
 *
 * Asserts: the client never insists its own count was right, the entry is
 * invalidated, the list is read again, and the message names the real cause.
 *
 * This is the case the whole caching design is built to survive. A cached count
 * is a hint. The api is the only thing that decides, and every screen is
 * written to be told no.
 */

const classId = "0192a000-0000-7000-8000-000000000021";
const studentId = "0192a000-0000-7000-8000-000000000011";

/** One class, at whatever count the api is currently reporting. */
function listWith(seatsRemaining: number) {
    return {
        classes: [
            {
                id: classId,
                subject: "math",
                title: "Math Challenge",
                starts_at: "2026-08-15T09:00:00Z",
                duration_minutes: 60,
                capacity: 4,
                seats_remaining: seatsRemaining,
            },
        ],
    };
}

/** The client, with the api filling the class between the two reads. */
function newStage() {
    let seatsRemaining = 1;
    let version = 1;

    const transport = createFakeTransport((request) => {
        if (request.method === "GET" && request.path === classListPath) {
            return { status: 200, body: listWith(seatsRemaining), headers: { etag: `"v${version}"` } };
        }

        if (request.method === "POST" && request.path === bookingsPath) {
            // Somebody else took the last seat while this parent was choosing,
            // which is the only reason this whole case exists.
            seatsRemaining = 0;
            version += 1;

            return { status: 409, body: errorBody("class_full") };
        }

        return { status: 404, body: errorBody("invalid_request") };
    });

    const client = createApiClient({ transport, onSignOut: () => {} });
    const store = createCacheStore();

    return {
        transport,
        classes: createClassesStore({ reader: createCachedReader({ client, store }) }),
        booking: createBookingStore({ mutator: createCacheAwareMutator({ client, store }), cache: store }),
    };
}

describe("simulation F2: the class filled while the parent was choosing", () => {
    beforeEach(() => {
        sessionStorage.clear();
    });

    test("behaviour: the refusal is reported in this client's words, not the server's", async () => {
        const stage = newStage();

        await stage.classes.load();

        expect(get(stage.classes).classes[0].seats_remaining).toBe(1);

        const refused = await stage.booking.create({ student_id: studentId, class_id: classId });

        expect(refused).toBeNull();
        expect(get(stage.booking).failure?.kind).toBe("ClassFull");
        expect(get(stage.booking).failure?.message).toBe("this class filled while you were choosing");
    });

    test("behaviour: the client accepts the refusal instead of trusting its own count", async () => {
        // The count on screen said one seat. Nothing here argues with the api
        // about that, and no retry is attempted on the strength of it.
        const stage = newStage();

        await stage.classes.load();
        await stage.booking.create({ student_id: studentId, class_id: classId });

        expect(stage.transport.callsTo("POST", bookingsPath)).toHaveLength(1);
        expect(get(stage.booking).booking).toBeNull();
    });

    test("behaviour: the entry is dropped and the list is read again, showing the real count", async () => {
        // Without the invalidation this is the bug: the entry is still fresh,
        // the second read is served from memory, and the parent goes back to a
        // list that still offers them the seat they were just refused.
        const stage = newStage();

        await stage.classes.load();

        expect(stage.transport.callsTo("GET", classListPath)).toHaveLength(1);

        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.classes.load();

        expect(stage.transport.callsTo("GET", classListPath)).toHaveLength(2);
        expect(get(stage.classes).classes[0].seats_remaining).toBe(0);
    });

    test("edge: the tag survives the invalidation, so an unaffected list costs a 304", async () => {
        // Invalidating ages the entry rather than deleting it. The body is
        // unusable either way, and keeping the tag is what turns the next read
        // into a conditional request rather than a full body.
        const stage = newStage();

        await stage.classes.load();
        await stage.booking.create({ student_id: studentId, class_id: classId });
        await stage.classes.load();

        const reads = stage.transport.callsTo("GET", classListPath);

        expect(reads[1].headers?.["if-none-match"]).toBe('"v1"');
    });
});

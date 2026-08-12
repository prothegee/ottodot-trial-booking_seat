import { get } from "svelte/store";
import { describe, expect, test } from "vitest";

import { ApiError } from "$lib/api/errors";
import type { Booking, BookingList } from "$lib/api/types";
import { createBookingsStore, myBookingsPath } from "$lib/stores/bookings";

const waiting: Booking = {
    id: "0192a000-0000-7000-8000-000000000031",
    student_id: "0192a000-0000-7000-8000-000000000011",
    class_id: "0192a000-0000-7000-8000-000000000021",
    class_subject: "science",
    class_title: "Science Discovery",
    class_starts_at: "2026-08-15T01:28:26.224983Z",
    status: "pending_payment",
    seat_no: null,
    hold_expires_at: "2026-08-11T09:10:00Z",
};

const settled: Booking = {
    id: "0192a000-0000-7000-8000-000000000032",
    student_id: "0192a000-0000-7000-8000-000000000011",
    class_id: "0192a000-0000-7000-8000-000000000022",
    class_subject: "science",
    class_title: "Science Discovery",
    class_starts_at: "2026-08-15T01:28:26.224983Z",
    status: "confirmed",
    seat_no: 2,
    hold_expires_at: null,
};

/** A client that answers with one list, or refuses. */
function stubClient(answer: BookingList | Error) {
    const calls: string[] = [];

    return {
        calls,
        client: {
            async request<T>(request: { method: string; path: string }): Promise<T> {
                calls.push(request.path);

                if (answer instanceof Error) {
                    throw answer;
                }

                return answer as T;
            },
        },
    };
}

describe("the bookings store", () => {
    test("integration: it reads this parent's bookings and holds them", async () => {
        const stub = stubClient({ bookings: [waiting, settled] });
        const store = createBookingsStore({ client: stub.client });

        await store.load();

        expect(stub.calls).toEqual([myBookingsPath]);
        expect(get(store).bookings).toHaveLength(2);
        expect(get(store).loaded).toBe(true);
        expect(get(store).failure).toBe("");
    });

    test("behaviour: the order the api sent is the order kept", async () => {
        // Newest first is the api's decision, and re-sorting here would be a
        // second opinion that can disagree with it.
        const stub = stubClient({ bookings: [waiting, settled] });
        const store = createBookingsStore({ client: stub.client });

        await store.load();

        expect(get(store).bookings.map((one) => one.id)).toEqual([waiting.id, settled.id]);
    });

    test("behaviour: an empty answer is told apart from one never asked for", async () => {
        // Both hold no bookings. Only one of them means "you have booked
        // nothing", and the screen shows that sentence on it.
        const store = createBookingsStore({ client: stubClient({ bookings: [] }).client });

        expect(get(store).loaded).toBe(false);

        await store.load();

        expect(get(store).loaded).toBe(true);
        expect(get(store).bookings).toHaveLength(0);
    });

    test("edge: a failed read leaves the list that was already there", async () => {
        // The parent was looking at something. Emptying the screen on a dropped
        // connection would read as "your bookings are gone", which is the one
        // thing this screen exists to stop them thinking.
        const store = createBookingsStore({ client: stubClient({ bookings: [waiting] }).client });

        await store.load();

        const refused = createBookingsStore({
            client: stubClient(new ApiError("Unavailable", "dependency_unavailable", 503)).client,
        });

        await refused.load();

        expect(get(store).bookings).toHaveLength(1);
        expect(get(refused).failure).not.toBe("");
    });

    test("edge: a failure that is not from the api still leaves a sentence", async () => {
        const store = createBookingsStore({
            client: {
                async request<T>(): Promise<T> {
                    throw new TypeError("the network went away");
                },
            },
        });

        await store.load();

        expect(get(store).failure).not.toBe("");
    });

    test("unit: resetting drops every booking the screen was holding", async () => {
        const store = createBookingsStore({ client: stubClient({ bookings: [waiting] }).client });

        await store.load();
        store.reset();

        expect(get(store)).toEqual({ bookings: [], loading: false, loaded: false, failure: "" });
    });

    test("unit: the path carries no identifier, because the token decides whose list it is", () => {
        expect(myBookingsPath).toBe("/api/v1/bookings");
    });
});

/**
 * The parent's own bookings, read straight through the api client.
 *
 * It is a separate store from `booking.ts` because the two answer different
 * questions. That one owns the booking in flight: the attempt key, the hold
 * counting down, the retry rules. This one owns a list somebody is looking at,
 * which has no attempt, no key, and nothing to retry.
 *
 * It deliberately does not go through the cache. Every cached read in this
 * client is advisory by construction, and a booking status is the opposite: it
 * is what decides whether the money landed. A stored copy of it is how a parent
 * is shown a payment as still pending two minutes after it cleared.
 */
import { writable } from "svelte/store";

import { ApiError } from "$lib/api/errors";
import type { Booking, BookingList } from "$lib/api/types";
import { api } from "$lib/session/client";
import { auth } from "$lib/stores/auth";

/** Where the signed-in parent's own bookings are read from. */
export const myBookingsPath = "/api/v1/bookings";

/** Everything the store holds. */
export interface BookingsState {
    /**
     * What the api last reported, newest first.
     *
     * It starts empty rather than null, so a screen renders the empty state
     * without a second check. `loaded` is what tells "none yet" from "not asked
     * yet" apart.
     */
    bookings: Booking[];

    /** True only while a read the parent is waiting on is in flight. */
    loading: boolean;

    /** True once a read has come back, whether it found anything or not. */
    loaded: boolean;

    /** What to show when the list could not be read. Empty when it could. */
    failure: string;
}

const emptyState: BookingsState = { bookings: [], loading: false, loaded: false, failure: "" };

/** What the store is built with. */
export interface BookingsStoreOptions {
    client?: { request<T>(request: { method: string; path: string }): Promise<T> };
}

/**
 * Builds the store.
 *
 * Note:
 * - a failed read leaves the previous list alone. The parent was looking at
 *   something, and emptying the screen on a dropped connection would read as
 *   "your bookings are gone".
 *
 * Param:
 * options - BookingsStoreOptions (the client, injected only by a test)
 *
 * Return:
 * - a store with one way to fill it
 */
export function createBookingsStore(options: BookingsStoreOptions = {}) {
    const client = options.client ?? api;

    const { subscribe, set, update } = writable<BookingsState>(emptyState);

    return {
        subscribe,

        /** Reads this parent's own bookings. */
        async load(): Promise<void> {
            update((state) => ({ ...state, loading: true, failure: "" }));

            try {
                const listed = await client.request<BookingList>({
                    method: "GET",
                    path: myBookingsPath,
                });

                set({ bookings: listed.bookings, loading: false, loaded: true, failure: "" });
            } catch (error) {
                const message =
                    error instanceof ApiError ? error.message : "your bookings could not be loaded";

                update((state) => ({ ...state, loading: false, failure: message }));
            }
        },

        /** Empties the store. */
        reset(): void {
            set(emptyState);
        },
    };
}

/** The one bookings store. */
export const bookings = createBookingsStore();

// A sign out has to leave nothing of the previous parent in memory. This list is
// every booking they have, so it is the last thing that should outlive them.
//
// Only the wired singleton does this. A store a test builds keeps whatever the
// test put in it.
auth.subscribe((state) => {
    if (state.session === null) {
        bookings.reset();
    }
});

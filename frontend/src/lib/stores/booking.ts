/**
 * The booking in flight: the one this parent is part way through.
 *
 * It holds the booking, the deadline the countdown runs from, and the
 * idempotency key covering the current attempt. One key spans the whole attempt
 * so that a retry of either call produces the first answer rather than a second
 * charge, and a new attempt mints a new one.
 *
 * Every write goes through `classMutator`, never through the api client, so a
 * successful booking cannot leave a seat count on screen that it just made
 * untrue. That invalidation happens before the promise here resolves, which is
 * what lets a screen navigate straight back to the list.
 */
import { writable } from "svelte/store";

import { createAttemptKey } from "$lib/api/attempt";
import { ApiError } from "$lib/api/errors";
import { newIdempotencyKey, withIdempotencyKey } from "$lib/api/idempotency";
import type { Booking, CreateBookingRequest, PayRequest } from "$lib/api/types";
import type { TransportRequest } from "$lib/api/transport";
import { api } from "$lib/session/client";
import { classCache } from "$lib/session/cache";
import { classMutator } from "$lib/session/cached_api";
import { auth } from "$lib/stores/auth";
import { reportApiError, reportFunnel } from "$lib/telemetry/report";

/** Where bookings are created. */
export const bookingsPath = "/api/v1/bookings";

/** The path one booking is read from. Never cached: it is what decides. */
export function bookingPathFor(bookingId: string): string {
    return `${bookingsPath}/${bookingId}`;
}

/** The path a payment for one booking is sent to. */
export function paymentPathFor(bookingId: string): string {
    return `${bookingsPath}/${bookingId}/payments`;
}

/** What a failed call left the screen with. */
export interface BookingFailure {
    /** The wording this client owns. Never the server's prose. */
    message: string;

    /** The kind, so a screen can branch without matching on wording. */
    kind: string;

    /**
     * The booking an `already_booked` failure points at, or an empty string.
     * It is what turns a duplicate notice into a link.
     */
    bookingId: string;

    /**
     * The request id an `internal_error` carries, or an empty string.
     *
     * It is the only thing that failure gives anybody. The message cannot say
     * what happened, so what is put on screen instead is the one string that
     * lets somebody find out.
     */
    requestId: string;
}

/** Everything the store holds. */
export interface BookingState {
    booking: Booking | null;

    /**
     * The key covering the attempt in progress. Empty until one starts.
     *
     * It is held rather than minted per call, because a create and the payment
     * that follows it are one attempt by the parent and must carry one key.
     */
    attemptKey: string;

    /** True only while a call the parent is waiting on is in flight. */
    submitting: boolean;

    failure: BookingFailure | null;
}

const emptyState: BookingState = { booking: null, attemptKey: "", submitting: false, failure: null };

/** What the store is built with. All of them have production defaults. */
export interface BookingStoreOptions {
    mutator?: { send<T>(request: TransportRequest): Promise<T> };

    /**
     * How a booking is read back.
     *
     * It is the plain api client rather than the cached reader, because a
     * booking status is the thing that decides. Serving one from a cache is
     * how a parent is shown a hold that expired two minutes ago.
     */
    reader?: { request<T>(request: TransportRequest): Promise<T> };

    /** The entries a refusal can prove wrong. Injected only by a test. */
    cache?: { invalidateAll(): void };

    /** Handed in only so a test can pin the key instead of matching a uuid. */
    newKey?: () => string;
}

/**
 * The failures that prove a cached seat count wrong.
 *
 * Most failures say nothing about seats and invalidate nothing, which is the
 * rule the mutation layer already follows. These two are the exceptions, and
 * they are exceptions for a reason a reader can check: `ClassFull` is the api
 * saying the class has no room, and `SeatLost` is it saying the last seat went
 * to somebody else. Both are news about the count on screen, arriving on the
 * failure path.
 *
 * Without this, a parent refused for a full class would go back to a list still
 * showing them a seat, because the entry is fresh and nothing asked again.
 */
const kindsThatMoveASeatCount: ReadonlySet<string> = new Set(["ClassFull", "SeatLost"]);

/**
 * Turns any thrown value into something a screen can render.
 *
 * A failure that is not an ApiError came from somewhere other than the api, and
 * the parent still needs a sentence rather than a blank panel.
 */
function failureFrom(error: unknown): BookingFailure {
    if (error instanceof ApiError) {
        return {
            message: error.message,
            kind: error.kind,
            bookingId: error.bookingId,
            requestId: error.requestId,
        };
    }

    return {
        message: "something went wrong, your booking was not changed",
        kind: "Unavailable",
        bookingId: "",
        requestId: "",
    };
}

/**
 * Builds the store.
 *
 * Note:
 * - `create` mints the key and `pay` reuses it. That is the whole attempt
 *   rule, in two lines, and it is why the key lives in the store rather than
 *   in either call.
 * - a failure does not clear the booking. A declined payment leaves the parent
 *   holding a booking they can pay for again, and throwing it away would send
 *   them back to the class list to start over while their hold is still
 *   standing.
 * - a refusal that names a seat count drops the cached list. See
 *   `kindsThatMoveASeatCount` for which two, and why the other failures do
 *   not.
 *
 * Param:
 * options - BookingStoreOptions (the mutator and the key source, both injected
 * only by a test)
 *
 * Return:
 * - a store that owns one booking at a time
 */
export function createBookingStore(options: BookingStoreOptions = {}) {
    const mutator = options.mutator ?? classMutator;
    const reader = options.reader ?? api;
    const cache = options.cache ?? classCache;
    const newKey = options.newKey ?? newIdempotencyKey;

    const { subscribe, set, update } = writable<BookingState>(emptyState);

    // The key lifecycle lives in the api layer, not here. This store decides
    // when an attempt starts, and `attempt.ts` decides when one is spent, which
    // keeps the rule readable without a store around it.
    const attempt = createAttemptKey(newKey);

    /** Runs one call, recording that it is in flight and what it produced. */
    async function submit(request: (key: string) => TransportRequest, key: string): Promise<Booking | null> {
        update((state) => ({ ...state, attemptKey: key, submitting: true, failure: null }));

        try {
            const booked = await mutator.send<Booking>(request(key));

            update((state) => ({ ...state, booking: booked, submitting: false, failure: null }));

            return booked;
        } catch (error) {
            const refusal = failureFrom(error);

            // Recorded where the parent is about to be told, rather than where
            // the failure was caught. "the api refused something" and "somebody
            // was told no" are different numbers, and the second is the one
            // worth a panel.
            if (error instanceof ApiError) {
                reportApiError(error.code);
            }

            if (kindsThatMoveASeatCount.has(refusal.kind)) {
                cache.invalidateAll();
            }

            // The key rule is applied here rather than left to whichever screen
            // offers the retry. A screen that forgot to mint a fresh key after
            // a decline would replay the decline, and a screen that minted one
            // after an `internal_error` would risk a second charge. Neither is
            // a mistake a component should be able to make.
            attempt.settle(refusal.kind);

            update((state) => ({
                ...state,
                attemptKey: attempt.current(),
                submitting: false,
                failure: refusal,
            }));

            return null;
        }
    }

    return {
        subscribe,

        /**
         * Asks for a hold on one class for one child, starting a new attempt.
         *
         * Return:
         * - the booking in pending_payment, carrying the deadline the parent has
         * - null when it was refused, with the reason in the store
         */
        async create(request: CreateBookingRequest): Promise<Booking | null> {
            const granted = await submit(
                (key) => ({
                    method: "POST",
                    path: bookingsPath,
                    body: request,
                    headers: withIdempotencyKey(key),
                }),
                attempt.restart(),
            );

            if (granted !== null) {
                reportFunnel("hold");
            }

            return granted;
        },

        /**
         * Reads one booking back from the api.
         *
         * It goes straight through the api client rather than through the
         * cached reader. A booking status is what decides: whether the hold is
         * still standing, whether the seat was won, whether a refund is on the
         * way. A cached answer to any of those is a screen telling a parent
         * something that stopped being true two minutes ago.
         *
         * Note:
         * - it never touches the attempt key. Reading is not attempting, and a
         *   parent refreshing the payment screen must not be handed a fresh key
         *   for a charge that may already be in flight.
         *
         * Return:
         * - the booking as the api currently reports it
         * - null when it could not be read, with the reason in the store
         */
        async load(bookingId: string): Promise<Booking | null> {
            update((state) => ({ ...state, submitting: true, failure: null }));

            try {
                const held = await reader.request<Booking>({
                    method: "GET",
                    path: bookingPathFor(bookingId),
                });

                update((state) => ({ ...state, booking: held, submitting: false, failure: null }));

                return held;
            } catch (error) {
                update((state) => ({ ...state, submitting: false, failure: failureFrom(error) }));

                return null;
            }
        },

        /**
         * Settles the booking in flight, under the key the attempt already has.
         *
         * The payment screen is what calls this. It is here rather than there
         * because the key belongs to the attempt, and the attempt is what this
         * store holds.
         *
         * Return:
         * - the booking, confirmed with its seat number when the seat was won
         * - null when it was refused, with the reason in the store
         */
        async pay(bookingId: string, amount: PayRequest): Promise<Booking | null> {
            reportFunnel("pay");

            // The key in force, which the keeper mints if this is somehow the
            // first call of the attempt. Sending an empty header would be
            // refused by the api, and sending a fresh key here would turn a
            // retry into a second charge.
            const settled = await submit(
                (key) => ({
                    method: "POST",
                    path: paymentPathFor(bookingId),
                    body: amount,
                    headers: withIdempotencyKey(key),
                }),
                attempt.current(),
            );

            // Only a confirmed seat closes the funnel. A settled payment that
            // lost the race is a parent owed a refund, and counting it as
            // reaching the end would make the one failure the whole design is
            // about invisible on the panel.
            if (settled !== null && settled.status === "confirmed") {
                reportFunnel("confirmed");
            }

            return settled;
        },

        /**
         * Clears the failure on screen without touching the attempt.
         *
         * A screen calls it when the parent starts typing again, so the
         * previous refusal stops shouting at them. It deliberately does not
         * mint a key: whether the next call is a new attempt or a retry of the
         * same one was already decided the moment the failure arrived, by
         * `kindsThatEndTheAttempt`.
         */
        dismissFailure(): void {
            update((state) => ({ ...state, failure: null }));
        },

        /** Empties the store, which a sign out does before the screen changes. */
        reset(): void {
            attempt.clear();

            set(emptyState);
        },
    };
}

/** The one booking store. */
export const booking = createBookingStore();

// A sign out has to leave nothing of the previous parent in memory.
//
// The store cannot be reset from `session/sign_out.ts`, because reaching this
// file from there would close a cycle: sign out imports the store, the store
// imports the mutator, the mutator imports the api client, and the api client
// reports a hard sign out. Watching the auth store instead is the same effect
// with the arrows pointing one way.
//
// Only the wired singleton does this. A store a test builds keeps whatever the
// test put in it.
auth.subscribe((state) => {
    if (state.session === null) {
        booking.reset();
    }
});

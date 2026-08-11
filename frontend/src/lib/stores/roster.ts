/**
 * The roster for one class, read straight through the api client.
 *
 * It deliberately does not go through the cache. Every other read in this client
 * that can be cached is advisory, and this one is the exception on both counts:
 * it carries a child's name next to a seat, and a stored copy of it would be the
 * only place in the browser where another family's name outlives the screen that
 * showed it.
 *
 * The route is admin only on the backend. Nothing here enforces that and nothing
 * here could: a client that hid the link would still be a client anybody can
 * open the developer tools on. Hiding the link is a courtesy, the api refusing
 * the request is the rule.
 */
import { writable } from "svelte/store";

import { ApiError } from "$lib/api/errors";
import type { Roster } from "$lib/api/types";
import { api } from "$lib/session/client";

/** Where one class's roster is read from. */
export function rosterPath(classId: string): string {
    return `/api/v1/classes/${classId}/roster`;
}

/** Everything the store holds. */
export interface RosterState {
    roster: Roster | null;

    /** True only while a read the teacher is waiting on is in flight. */
    loading: boolean;

    /** What to show when the roster could not be read. Empty when it could. */
    failure: string;

    /**
     * True when the api refused this account rather than this class.
     *
     * It is separate from the message because the two want different screens: a
     * parent who reached this route by typing it should be told plainly that it
     * is not for them, and a teacher whose class does not exist should be told
     * that instead.
     */
    forbidden: boolean;
}

const emptyState: RosterState = { roster: null, loading: false, failure: "", forbidden: false };

/** What the store is built with. */
export interface RosterStoreOptions {
    client?: { request<T>(request: { method: string; path: string }): Promise<T> };
}

/**
 * Builds the store.
 *
 * Param:
 * options - RosterStoreOptions (the client, injected only by a test)
 *
 * Return:
 * - a store with one way to fill it
 */
export function createRosterStore(options: RosterStoreOptions = {}) {
    const client = options.client ?? api;

    const { subscribe, set, update } = writable<RosterState>(emptyState);

    return {
        subscribe,

        /** Reads one class's roster. */
        async load(classId: string): Promise<void> {
            update((state) => ({ ...state, loading: true, failure: "", forbidden: false }));

            try {
                const roster = await client.request<Roster>({ method: "GET", path: rosterPath(classId) });

                set({ roster, loading: false, failure: "", forbidden: false });
            } catch (error) {
                const refused = error instanceof ApiError && error.kind === "Forbidden";
                const message = error instanceof ApiError ? error.message : "the roster could not be loaded";

                set({ roster: null, loading: false, failure: message, forbidden: refused });
            }
        },

        /** Empties the store, which the screen does on the way out. */
        reset(): void {
            set(emptyState);
        },
    };
}

/** The one roster store. */
export const roster = createRosterStore();

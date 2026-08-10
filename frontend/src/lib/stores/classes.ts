/**
 * The class list, read through the cache.
 *
 * Every read goes through `classReader`, never through the api client, which is
 * what makes a repeat view of the list cost nothing and a view after a booking
 * cost one conditional request. A store reaching past the reader is the one way
 * that cache can go wrong, so this file is the only one that reads classes.
 *
 * Nothing here decides anything. `seats_remaining` is a hint that saves a
 * parent a wasted click, and by the time they click it may already be wrong.
 * The screen is written to expect exactly that, and the api is what says no.
 */
import { writable } from "svelte/store";

import { ApiError } from "$lib/api/errors";
import type { ClassList, TrialClass } from "$lib/api/types";
import type { CacheLookupResult } from "$lib/cache/read_through";
import { classReader } from "$lib/session/cached_api";
import { auth } from "$lib/stores/auth";

/** The path the list is read from. */
export const classListPath = "/api/v1/classes";

/** Everything the store holds. */
export interface ClassesState {
    classes: TrialClass[];

    /** True only while a read the parent is waiting on is in flight. */
    loading: boolean;

    /** What to show when the list could not be read. Empty when it could. */
    failure: string;

    /**
     * Which cache tier served the last read.
     *
     * It is kept because it is the difference between a list that is a moment
     * old and one that was just confirmed, and phase 7 turns it into
     * `frontend_cache_lookup_total{result}`.
     */
    lastResult: CacheLookupResult | null;
}

const emptyState: ClassesState = { classes: [], loading: false, failure: "", lastResult: null };

/** What the store is built with. Both have production defaults. */
export interface ClassesStoreOptions {
    reader?: { read<T>(path: string): Promise<{ body: T; result: CacheLookupResult; revalidation: Promise<void> | null }> };
}

/**
 * Builds the store.
 *
 * Note:
 * - a stale read shows the stored list at once and confirms it behind the
 *   parent's back. The returned promise resolves as soon as there is something
 *   to render, not when the confirmation lands, because making a parent wait
 *   for a list they can already see is the cost this cache exists to avoid.
 * - a failed read leaves the previous list on screen alongside the message.
 *   Blanking the page would take away the one thing the parent could still act
 *   on, and the list they have is no more wrong than it was a second ago.
 *
 * Param:
 * options - ClassesStoreOptions (the reader, injected only by a test)
 *
 * Return:
 * - a store with one way to fill it
 */
export function createClassesStore(options: ClassesStoreOptions = {}) {
    const reader = options.reader ?? classReader;

    const { subscribe, set, update } = writable<ClassesState>(emptyState);

    return {
        subscribe,

        /**
         * Reads the list.
         *
         * Return:
         * - the background revalidation when the read was stale, so a test can
         *   await it. Null on every other tier, and a screen ignores it either
         *   way
         */
        async load(): Promise<Promise<void> | null> {
            update((state) => ({ ...state, loading: true, failure: "" }));

            try {
                const answer = await reader.read<ClassList>(classListPath);

                set({
                    classes: answer.body.classes ?? [],
                    loading: false,
                    failure: "",
                    lastResult: answer.result,
                });

                return answer.revalidation;
            } catch (error) {
                const message = error instanceof ApiError ? error.message : "the class list could not be loaded";

                update((state) => ({ ...state, loading: false, failure: message }));

                return null;
            }
        },

        /** Empties the store, which a sign out does before the screen changes. */
        reset(): void {
            set(emptyState);
        },
    };
}

/** The one class list store. */
export const classes = createClassesStore();

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
        classes.reset();
    }
});

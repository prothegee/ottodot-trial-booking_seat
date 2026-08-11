/**
 * What the backend says about itself, while the status screen is open.
 *
 * It is the only thing in this client that polls on a timer, and the only thing
 * that reads the two unauthenticated operations routes. Both of those are worth
 * being explicit about: a poll that outlives the screen it belongs to is a
 * request every fifteen seconds for a page nobody is looking at, and a client
 * that polled readiness everywhere would turn a readiness probe into traffic.
 *
 * Neither route is cached. A cached answer to "is the database up" is an answer
 * about a moment that has passed, which is the one question a cache cannot help
 * with.
 */
import { writable } from "svelte/store";

import { ApiError } from "$lib/api/errors";
import type { Readiness, VersionIdentity } from "$lib/api/types";
import { api } from "$lib/session/client";

/** Where the two operations routes live. They carry no version prefix. */
export const versionPath = "/version";
export const readinessPath = "/readyz";

/** How often the screen asks again while it is open. */
export const pollIntervalMs = 15_000;

/** Everything the store holds. */
export interface StatusState {
    version: VersionIdentity | null;
    readiness: Readiness | null;

    /** True only while the first read is in flight, so the screen can say so. */
    loading: boolean;

    /**
     * What to show when the backend could not be reached at all.
     *
     * It is separate from an unready readiness body, because the two mean
     * different things: an unready answer is a service that is talking and
     * telling the truth about being broken, and this is a service that is not
     * talking.
     */
    failure: string;
}

const emptyState: StatusState = { version: null, readiness: null, loading: false, failure: "" };

/** What the store is built with. Both have production defaults. */
export interface StatusStoreOptions {
    client?: { request<T>(request: { method: string; path: string }): Promise<T> };

    /** Overrides the poll period, so a test does not wait fifteen seconds. */
    intervalMs?: number;
}

/**
 * Builds the store.
 *
 * Note:
 * - a 503 from readiness is a successful read of a real answer, not a failure.
 *   The api answers unready with the body describing which dependency is down,
 *   and treating that status as an error would throw away the only information
 *   the screen exists to show.
 *
 * Param:
 * options - StatusStoreOptions (the client and the interval, injected by a test)
 *
 * Return:
 * - a store that can be opened and closed
 */
export function createStatusStore(options: StatusStoreOptions = {}) {
    const client = options.client ?? api;
    const intervalMs = options.intervalMs ?? pollIntervalMs;

    const { subscribe, set, update } = writable<StatusState>(emptyState);

    let timer: ReturnType<typeof setInterval> | undefined;

    /** Reads readiness, treating an unready answer as an answer. */
    async function readReadiness(): Promise<Readiness | null> {
        try {
            return await client.request<Readiness>({ method: "GET", path: readinessPath });
        } catch (error) {
            if (error instanceof ApiError && error.body !== undefined) {
                const body = error.body as Partial<Readiness>;

                if (typeof body.status === "string") {
                    return body as Readiness;
                }
            }

            return null;
        }
    }

    /** One round of both reads. */
    async function refresh(): Promise<void> {
        try {
            const [identity, readiness] = await Promise.all([
                client.request<VersionIdentity>({ method: "GET", path: versionPath }),
                readReadiness(),
            ]);

            set({
                version: identity,
                readiness,
                loading: false,
                failure: readiness === null ? "the backend answered, but not about its readiness" : "",
            });
        } catch (error) {
            const message = error instanceof ApiError ? error.message : "the backend could not be reached";

            update((state) => ({ ...state, loading: false, failure: message }));
        }
    }

    return {
        subscribe,

        /**
         * Reads once and then keeps reading until close.
         *
         * Return:
         * - the first read, so a screen can await it before rendering and a test
         *   does not have to guess when it landed
         */
        async open(): Promise<void> {
            update((state) => ({ ...state, loading: true, failure: "" }));

            await refresh();

            // The timer is started after the first read rather than before it,
            // so a slow first answer cannot be overlapped by the second.
            if (timer === undefined) {
                timer = setInterval(() => {
                    void refresh();
                }, intervalMs);
            }
        },

        /**
         * Stops polling.
         *
         * A screen calls this when it is destroyed. A timer that outlives its
         * screen is a request every fifteen seconds for a page nobody is looking
         * at, and in a test it is a warning after the case has finished.
         */
        close(): void {
            if (timer !== undefined) {
                clearInterval(timer);
                timer = undefined;
            }
        },

        /** Empties the store, which the screen does on the way out. */
        reset(): void {
            set(emptyState);
        },
    };
}

/** The one status store. */
export const status = createStatusStore();

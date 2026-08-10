/**
 * The read path: memory first, the api only when it has to be asked.
 *
 * This is the file that puts `policy.ts`, `key.ts`, and `store.ts` together
 * with the api client. A fresh entry is answered with no request at all. A
 * stale one is answered at once and confirmed behind the parent's back. A cold
 * one is confirmed before anything is shown.
 *
 * None of this would be safe if a cached count decided anything. It does not:
 * a seat count is a hint, and the only decision in the system is a transaction
 * on the backend. See ADR-F003.
 */
import { ApiError } from "$lib/api/errors";
import type { ApiClient } from "$lib/api/client";
import { cacheKeyFor } from "$lib/cache/key";
import { tierForAge } from "$lib/cache/policy";
import type { CacheEntry, CacheStore } from "$lib/cache/store";

/**
 * What the cache did for one read.
 *
 * Phase 7 turns this into `frontend_cache_lookup_total{result}`. It is a plain
 * returned value here so that nothing in this phase has to know a telemetry
 * emitter exists.
 */
export type CacheLookupResult = "fresh" | "stale" | "revalidated" | "miss";

/** One answer from the cache, and what it cost. */
export interface CachedRead<T> {
    /** What to render. Stored or fetched, the caller does not have to care. */
    body: T;

    /** Which of the four outcomes this read was. */
    result: CacheLookupResult;

    /**
     * The background revalidation, present only on a stale read.
     *
     * A screen ignores it. A test awaits it, which is how the revalidation can
     * be observed without a timer.
     */
    revalidation: Promise<void> | null;
}

/** Reads class data through the cache. */
export interface CachedReader {
    read<T>(path: string): Promise<CachedRead<T>>;
}

/** What the reader is built with. */
export interface CachedReaderOptions {
    client: ApiClient;
    store: CacheStore;

    /** Handed in only so a test can place an entry at a chosen age. */
    now?: () => number;
}

/**
 * Builds the reader.
 *
 * Note:
 * - a background revalidation never surfaces its failure. The parent already
 *   has a body on screen, and a stale list is not worth an error banner. A 401
 *   still reaches the sign-out path, because that happens inside the api
 *   client rather than here.
 * - two stale reads of the same key share one revalidation, for the same
 *   reason the refresh is single flight: the second call buys nothing.
 * - a path this cache may not hold is fetched and returned untouched, so a
 *   caller cannot accidentally cache a roster by routing it through here.
 *
 * Param:
 * options - CachedReaderOptions (the api client, the store, and a clock)
 *
 * Return:
 * - a reader whose every answer says which tier served it
 */
export function createCachedReader(options: CachedReaderOptions): CachedReader {
    const { client, store } = options;
    const now = options.now ?? (() => Date.now());

    const revalidating = new Map<string, Promise<void>>();

    /**
     * Asks the api whether a stored entry still stands, and records the answer.
     *
     * A 304 refreshes the age and leaves body and tag alone, so subscribers
     * are not woken for a body that did not change. A 200 replaces both and
     * wakes them.
     */
    async function confirm(key: string, path: string, etag: string): Promise<void> {
        const answer = await client.conditionalGet<unknown>(path, etag);

        if (answer.notModified) {
            store.touch(key);

            return;
        }

        store.save(key, answer.body, answer.etag);
    }

    /** Runs one confirmation per key at a time, and swallows its failure. */
    function confirmInBackground(key: string, path: string, etag: string): Promise<void> {
        const running = revalidating.get(key);

        if (running !== undefined) {
            return running;
        }

        const attempt = confirm(key, path, etag)
            .catch(() => {})
            .finally(() => {
                revalidating.delete(key);
            });

        revalidating.set(key, attempt);

        return attempt;
    }

    /** The blocking read, used when nothing may be rendered before the answer. */
    async function readThrough<T>(key: string | null, path: string, held: CacheEntry | null): Promise<CachedRead<T>> {
        const answer = await client.conditionalGet<T>(path, held?.etag ?? "");

        if (answer.notModified) {
            if (held === null) {
                // The api said nothing changed for a question that was never
                // asked. Nothing here can turn that into a body, and guessing
                // one would be worse than saying so.
                throw new ApiError("Unavailable", "not_modified_without_copy", 304);
            }

            if (key !== null) {
                store.touch(key);
            }

            return { body: held.body as T, result: "revalidated", revalidation: null };
        }

        if (key !== null) {
            store.save(key, answer.body, answer.etag);
        }

        return { body: answer.body as T, result: "miss", revalidation: null };
    }

    return {
        async read<T>(path: string): Promise<CachedRead<T>> {
            const key = cacheKeyFor("GET", path);

            if (key === null) {
                return readThrough<T>(null, path, null);
            }

            const held = store.read(key);

            if (held === null) {
                return readThrough<T>(key, path, null);
            }

            const tier = tierForAge(now() - held.storedAt);

            if (tier === "fresh") {
                return { body: held.body as T, result: "fresh", revalidation: null };
            }

            if (tier === "stale") {
                return {
                    body: held.body as T,
                    result: "stale",
                    revalidation: confirmInBackground(key, path, held.etag),
                };
            }

            return readThrough<T>(key, path, held);
        },
    };
}

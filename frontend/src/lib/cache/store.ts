/**
 * Where cached bodies and their tags live.
 *
 * An in-memory map is the real store. The session mirror behind it exists so a
 * page reload keeps the entry and, more importantly, keeps its ETag, because a
 * tag that survives is what turns the first request after a reload into a 304
 * rather than a full body.
 *
 * The store holds and forgets. It never decides whether an entry is old enough
 * to use, and it never sends anything: that is `policy.ts` and `read_through.ts`.
 */
import { staleWindowMilliseconds } from "$lib/cache/policy";
import { createSessionMirror, type SessionMirror } from "$lib/cache/session_mirror";

/** One cached answer, with the tag the api gave it. */
export interface CacheEntry {
    /** The parsed response body, exactly as the api sent it. */
    body: unknown;

    /**
     * The validator, stored verbatim and sent back verbatim. This client never
     * parses one, compares two, or reasons about what is inside a tag. Only the
     * api knows what its own tags mean.
     */
    etag: string;

    /** When this entry last came from, or was confirmed by, the api. */
    storedAt: number;
}

/** Told when an entry's body changed, and never told when it did not. */
export type CacheListener = (key: string) => void;

/** Holds entries, and tells watchers when a body was replaced. */
export interface CacheStore {
    /** The entry under this key, from memory or from the mirror, or null. */
    read(key: string): CacheEntry | null;

    /** Replaces body and tag together, stamps the time, and notifies. */
    save(key: string, body: unknown, etag: string): void;

    /** Stamps the time and keeps the body and tag as they are. No notify. */
    touch(key: string): void;

    /** Ages one entry past its stale window, keeping its body and its tag. */
    invalidate(key: string): void;

    /** The same, for every entry held, mirrored ones included. */
    invalidateAll(): void;

    /** Drops every entry outright, in memory and mirrored. */
    clear(): void;

    /** Registers a listener. The returned function removes it. */
    subscribe(listener: CacheListener): () => void;
}

/** What the store is built with. Both have production defaults. */
export interface CacheStoreOptions {
    mirror?: SessionMirror<CacheEntry>;

    /** Handed in only so a test can place an entry at a chosen age. */
    now?: () => number;
}

/** The namespace the mirror owns inside session storage. */
export const cacheNamespace = "cache";

/**
 * Builds a store.
 *
 * Note:
 * - a 304 must call `touch` and never `save`. Touching keeps the body the
 *   subscriber already rendered and tells nobody, because nothing changed. A
 *   `save` would replace an identical body and repaint the screen for no
 *   reason.
 * - invalidating ages an entry rather than deleting it. The body is unusable
 *   either way, since a cold entry is never rendered before the api answers.
 *   The tag is what survives, and it is worth keeping: if the mutation did not
 *   move that particular list, the next read is a 304 rather than a full body.
 *   A sign out is the opposite case and uses `clear`, because there the point
 *   is that nothing at all is left behind.
 * - the mirror is read lazily, on the first miss for a key, rather than by
 *   walking storage at construction. Nothing is paid for entries never asked
 *   for.
 *
 * Param:
 * options - CacheStoreOptions (a mirror and a clock, both optional)
 *
 * Return:
 * - a store that survives a reload and never throws from storage
 */
export function createCacheStore(options: CacheStoreOptions = {}): CacheStore {
    const mirror = options.mirror ?? createSessionMirror<CacheEntry>(cacheNamespace);
    const now = options.now ?? (() => Date.now());

    const entries = new Map<string, CacheEntry>();
    const listeners = new Set<CacheListener>();

    function read(key: string): CacheEntry | null {
        const held = entries.get(key);

        if (held !== undefined) {
            return held;
        }

        const mirrored = mirror.read(key);

        if (mirrored === null) {
            return null;
        }

        entries.set(key, mirrored);

        return mirrored;
    }

    function put(key: string, entry: CacheEntry): void {
        entries.set(key, entry);
        mirror.write(key, entry);
    }

    /** Rewrites an entry's time, leaving body and tag exactly as they are. */
    function restamp(key: string, at: number): void {
        const held = read(key);

        if (held === null) {
            return;
        }

        put(key, { body: held.body, etag: held.etag, storedAt: at });
    }

    /** The stamp that makes an entry read as cold for every read from now on. */
    function coldAsOfNow(): number {
        return now() - staleWindowMilliseconds;
    }

    return {
        read,

        save(key: string, body: unknown, etag: string): void {
            put(key, { body, etag, storedAt: now() });

            for (const listener of listeners) {
                listener(key);
            }
        },

        touch(key: string): void {
            restamp(key, now());
        },

        invalidate(key: string): void {
            restamp(key, coldAsOfNow());
        },

        invalidateAll(): void {
            // The mirror is walked as well as the map. After a reload the map
            // is empty and the mirror is not, and an entry nobody has read yet
            // is exactly the one that would still be believed.
            const everyKey = new Set([...entries.keys(), ...mirror.keys()]);

            for (const key of everyKey) {
                restamp(key, coldAsOfNow());
            }
        },

        clear(): void {
            entries.clear();
            mirror.clear();
        },

        subscribe(listener: CacheListener): () => void {
            listeners.add(listener);

            return () => {
                listeners.delete(listener);
            };
        },
    };
}

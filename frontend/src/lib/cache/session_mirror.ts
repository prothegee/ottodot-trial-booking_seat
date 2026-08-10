/**
 * The copy of the cache that survives a page reload.
 *
 * It is its own file because talking to `sessionStorage` is a different job
 * from deciding what may be cached: it serializes, it namespaces, and above
 * all it fails quietly. Quota exhaustion, a disabled storage, and private
 * browsing all throw from the same calls, and none of them is a reason for a
 * page to break. Every failure here degrades to no mirror at all, and the
 * in-memory map carries on.
 *
 * `sessionStorage` and not `localStorage`, on purpose: the entries belong to
 * one browsing session and must not outlive it on a shared machine.
 */

/** Reads, writes, and forgets values under one namespace. */
export interface SessionMirror<T> {
    read(key: string): T | null;
    write(key: string, value: T): void;
    remove(key: string): void;

    /** Every key held in this namespace, with the namespace stripped off. */
    keys(): string[];

    /** Removes every key in this namespace, and nothing outside it. */
    clear(): void;
}

/** The storage this mirror needs, narrowed to what it uses. */
type StorageLike = Pick<Storage, "getItem" | "setItem" | "removeItem" | "key" | "length">;

/**
 * Finds the session storage, or reports that there is none.
 *
 * Reading the global itself can throw in a browser where storage is blocked by
 * policy, which is why even this is wrapped.
 */
function availableStorage(): StorageLike | null {
    try {
        if (typeof sessionStorage === "undefined") {
            return null;
        }

        return sessionStorage;
    } catch {
        return null;
    }
}

/**
 * Builds a mirror over one namespace.
 *
 * Note:
 * - nothing here throws. A caller is never handed a storage failure, because
 *   the mirror is an optimisation and a page that depends on one is broken.
 * - a value that will not serialize is simply not mirrored. The in-memory copy
 *   is still correct, it just does not survive a reload.
 *
 * Param:
 * namespace - string (prefixed to every key, so this mirror owns a clear slice
 *             of the origin's storage)
 * storage - StorageLike (handed in only so a test can supply a failing one)
 *
 * Return:
 * - a mirror that never raises
 */
export function createSessionMirror<T>(
    namespace: string,
    storage: StorageLike | null = availableStorage(),
): SessionMirror<T> {
    const prefix = namespace + ":";

    function read(key: string): T | null {
        if (storage === null) {
            return null;
        }

        try {
            const raw = storage.getItem(prefix + key);

            if (raw === null) {
                return null;
            }

            return JSON.parse(raw) as T;
        } catch {
            // A half-written or hand-edited entry is treated as no entry, so
            // the read falls through to the network.
            return null;
        }
    }

    function write(key: string, value: T): void {
        if (storage === null) {
            return;
        }

        try {
            storage.setItem(prefix + key, JSON.stringify(value));
        } catch {
            return;
        }
    }

    function remove(key: string): void {
        if (storage === null) {
            return;
        }

        try {
            storage.removeItem(prefix + key);
        } catch {
            return;
        }
    }

    function keys(): string[] {
        if (storage === null) {
            return [];
        }

        try {
            const owned: string[] = [];

            for (let i = 0; i < storage.length; i += 1) {
                const name = storage.key(i);

                if (name !== null && name.startsWith(prefix)) {
                    owned.push(name.slice(prefix.length));
                }
            }

            return owned;
        } catch {
            return [];
        }
    }

    function clear(): void {
        for (const key of keys()) {
            remove(key);
        }
    }

    return { read, write, remove, keys, clear };
}

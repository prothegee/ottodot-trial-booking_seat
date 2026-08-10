/**
 * Which requests this cache is allowed to hold, and under what key.
 *
 * The list is deliberately short. A class list and a single class are advisory
 * by construction, so a stale copy can only cost a wasted click. Everything
 * else either decides something or is specific to one parent, and a stale copy
 * of that is a wrong answer rather than a slow one.
 */

/** The class list endpoint, the only collection this cache holds. */
export const classListPath = "/api/v1/classes";

/** The prefix a single class sits under. */
const singleClassPrefix = classListPath + "/";

/**
 * Reports whether a path names a class resource this cache may hold.
 *
 * Note:
 * - a nested path such as the roster under a class is not cacheable. It names
 *   real children by name, and it is never advisory.
 *
 * Param:
 * path - string (the request path, query included)
 *
 * Return:
 * - true for the class list and for a single class
 * - false for everything else
 */
export function isCacheableClassPath(path: string): boolean {
    const route = path.split("?")[0];

    if (route === classListPath) {
        return true;
    }

    if (!route.startsWith(singleClassPrefix)) {
        return false;
    }

    const identifier = route.slice(singleClassPrefix.length);

    return identifier.length > 0 && !identifier.includes("/");
}

/**
 * Derives the cache key for one request, or reports that it has none.
 *
 * Note:
 * - the key is the path with its query string attached verbatim, so a filtered
 *   list can never be served for an unfiltered one. Two orderings of the same
 *   parameters are two entries, which costs one extra conditional request and
 *   never a wrong answer.
 * - only GET is cacheable. A mutation is never an entry, it drops them.
 *
 * Param:
 * method - string (the http method)
 * path - string (the request path, query included)
 *
 * Return:
 * - the key to store under
 * - null when the request may not be cached at all
 */
export function cacheKeyFor(method: string, path: string): string | null {
    if (method.toUpperCase() !== "GET") {
        return null;
    }

    if (!isCacheableClassPath(path)) {
        return null;
    }

    return path;
}

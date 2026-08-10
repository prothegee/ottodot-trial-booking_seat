/**
 * How old a cached entry is allowed to be, and what may be done with it.
 *
 * This file decides nothing about storage and knows nothing about the network.
 * It is one pure function over an age in milliseconds, which is what makes the
 * boundary cases testable without a clock, a timer, or a transport.
 */

/** What a cached entry may be used for, decided only by its age. */
export type CacheTier = "fresh" | "stale" | "cold";

/**
 * Below this age an entry is served straight from memory and no request is
 * sent at all.
 */
export const freshWindowMilliseconds = 5_000;

/**
 * Below this age a stale entry may still be rendered, as long as a
 * revalidation is started behind it. At or past it, nothing may be rendered
 * before the api has answered.
 */
export const staleWindowMilliseconds = 30_000;

/**
 * Decides the tier for an entry of a given age.
 *
 * Note:
 * - both boundaries are exclusive at the low end. Exactly five seconds is
 *   stale, and exactly thirty seconds is cold. An entry sitting on a boundary
 *   costs one conditional request, which is the cheap side of the mistake.
 * - a negative age means the entry claims to have been written in the future,
 *   which happens when the system clock moves backwards. The timestamp cannot
 *   be trusted at that point, so the entry is treated as cold and revalidated.
 *
 * Param:
 * ageMilliseconds - number (now minus the moment the entry was stored)
 *
 * Return:
 * - "fresh" when no request is needed
 * - "stale" when the stored body may be rendered while a revalidation runs
 * - "cold" when nothing may be rendered until the api has answered
 */
export function tierForAge(ageMilliseconds: number): CacheTier {
    if (ageMilliseconds < 0) {
        return "cold";
    }

    if (ageMilliseconds < freshWindowMilliseconds) {
        return "fresh";
    }

    if (ageMilliseconds < staleWindowMilliseconds) {
        return "stale";
    }

    return "cold";
}

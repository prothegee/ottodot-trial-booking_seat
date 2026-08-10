/**
 * The cache-aware way in and out for class data, wired once.
 *
 * Reads go through `classReader` so a repeat view of the list costs nothing.
 * Writes go through `classMutator` so a successful booking cannot leave a seat
 * count on screen that it just made untrue. A store reaching past these two to
 * the api client directly is the one way this cache can go wrong, so there is
 * exactly one pair to reach for.
 *
 * The class list store in phase 4 is the first consumer.
 */
import { createCachedReader } from "$lib/cache/read_through";
import { createCacheAwareMutator } from "$lib/cache/mutation";
import { api } from "$lib/session/client";
import { classCache } from "$lib/session/cache";

/** Every read of the class list and of a single class. */
export const classReader = createCachedReader({ client: api, store: classCache });

/** Every booking, payment, and cancellation that changes a seat count. */
export const classMutator = createCacheAwareMutator({ client: api, store: classCache });

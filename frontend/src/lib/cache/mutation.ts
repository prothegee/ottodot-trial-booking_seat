/**
 * The write path: a mutation, and the entries it makes untrue.
 *
 * Invalidation is not left to each caller to remember. A booking, a payment,
 * or a cancellation goes through here, and dropping the affected entries is
 * part of the call rather than a line somebody has to add afterwards. That is
 * the whole reason this file exists.
 */
import type { ApiClient } from "$lib/api/client";
import type { TransportRequest } from "$lib/api/transport";
import type { CacheStore } from "$lib/cache/store";

/** Sends a mutation and drops what it changed. */
export interface CacheAwareMutator {
    send<T>(request: TransportRequest): Promise<T>;
}

/** What the mutator is built with. */
export interface CacheAwareMutatorOptions {
    client: ApiClient;
    store: CacheStore;
}

/**
 * Builds the mutator.
 *
 * Note:
 * - only a success invalidates. A failure is told nothing about the seat, and
 *   a 500 in particular says nothing at all about what happened, so dropping
 *   the entry would claim knowledge this client does not have.
 * - the drop happens before the caller's promise resolves, so a screen that
 *   navigates back to the list on success can never be served the count it
 *   just changed.
 * - every entry is invalidated rather than a chosen one. This cache holds
 *   class data and nothing else, and every mutation in this system moves a
 *   seat count on a class, so the precise version would drop the same set for
 *   more code. A read costs one conditional request, which is the cheap side.
 * - the tags are kept. The entries are cold, so nothing is rendered from them
 *   before the api answers, and if the mutation did not move that particular
 *   list the answer is a 304 rather than a full body.
 * - calls that change nothing a parent reads, telemetry above all, go straight
 *   through the api client and never through here.
 *
 * Param:
 * options - CacheAwareMutatorOptions (the api client and the store to drop from)
 *
 * Return:
 * - a mutator whose successful calls leave no stale class entry behind
 */
export function createCacheAwareMutator(options: CacheAwareMutatorOptions): CacheAwareMutator {
    const { client, store } = options;

    return {
        async send<T>(request: TransportRequest): Promise<T> {
            const answer = await client.request<T>(request);

            store.invalidateAll();

            return answer;
        },
    };
}

/**
 * The one cache store the application uses, wired once.
 *
 * It is its own file, and it deliberately imports nothing from the api client.
 * The hard sign out has to be able to empty this store, and the api client has
 * to be able to report a hard sign out, so anything that joined the two would
 * make a cycle out of a two-line wiring file.
 */
import { createCacheStore } from "$lib/cache/store";

/** Holds the class list and single classes, mirrored into session storage. */
export const classCache = createCacheStore();

/**
 * The values vite.config.ts injects at build time.
 *
 * This module is the only place those constants are read. Every other file
 * imports from here, so pointing the client at a different backend is one
 * environment variable rather than a search across components.
 */

export interface BuildIdentity {
    version: string;
    commit: string;
}

/** Where every api call goes. Never written into a component. */
export const apiBaseUrl: string = __API_BASE_URL__;

/**
 * What the footer and the status route show.
 *
 * Version and commit only. Nothing here identifies a person, because this
 * string is on screen for the whole of a recorded walkthrough.
 */
export const buildIdentity: BuildIdentity = {
    version: __BUILD_VERSION__,
    commit: __BUILD_COMMIT__,
};

/**
 * The api address this page is allowed to call, decided from the page it is on.
 *
 * A cookie belongs to a host, and a port is not part of that host. A client
 * served from `http://localhost:9001` calling `http://127.0.0.1:9000` is
 * therefore cross-site, so the browser refuses to keep the session cookies the
 * api sets: sign in answers 204, nothing is stored, and the next call is a 401
 * with no visible reason for it.
 *
 * Both names reach the same loopback interface, and the api allows both origins
 * on purpose so a reviewer may type whichever comes to mind. This makes that
 * true: when the page is on one loopback name and the api is configured with the
 * other, the api host follows the page. Same host, different port, which is
 * same-site, so the cookies are kept.
 *
 * Nothing else is touched. A deployed client pointing at a real api is left
 * exactly as configured, because a real host is not a loopback name.
 */

import { apiBaseUrl } from "$lib/config/environment";

/**
 * The names that mean this machine.
 *
 * IPv6 is deliberately absent. The dev server binds `127.0.0.1`, so a page on
 * `[::1]` is not a case this client can reach in the first place.
 */
const loopbackHosts: ReadonlySet<string> = new Set(["localhost", "127.0.0.1"]);

/**
 * Rewrites the configured api host to match the page host, when both are
 * loopback names and they differ.
 *
 * Param:
 * configured - string (the address the build was given)
 * pageHostname - string (the host the page itself was served from)
 *
 * Return:
 * - the configured address, unchanged, in every case but one
 * - the same address with its host replaced, when both hosts are loopback names
 *   that do not match
 */
export function apiBaseUrlForPage(configured: string, pageHostname: string): string {
    if (!loopbackHosts.has(pageHostname)) return configured;

    let target: URL;

    try {
        target = new URL(configured);
    } catch {
        return configured;
    }

    if (!loopbackHosts.has(target.hostname)) return configured;
    if (target.hostname === pageHostname) return configured;

    target.hostname = pageHostname;

    // The transport joins this to a path that already starts with a slash, so a
    // trailing one here would send every request to a doubled slash.
    const aligned = target.toString();

    return aligned.endsWith("/") ? aligned.slice(0, -1) : aligned;
}

/**
 * The address the running page should use.
 *
 * Note:
 * - Outside a browser there is no page to follow, so the configured value stands.
 *
 * Return:
 * - the api base url, aligned with the page host where that applies
 */
export function apiBaseUrlForThisPage(): string {
    if (typeof window === "undefined") return apiBaseUrl;

    return apiBaseUrlForPage(apiBaseUrl, window.location.hostname);
}

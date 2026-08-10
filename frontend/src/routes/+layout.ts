/**
 * No server rendering, anywhere.
 *
 * Every decision in this system is made inside a transaction on the backend, so
 * a rendering server in front of the client would add a moving part without
 * adding an answer. The build is a static bundle and the client asks the api
 * for everything it shows.
 *
 * Prerendering is off for the same reason: there is no page here whose content
 * is known before a parent signs in.
 */
export const ssr = false;
export const prerender = false;

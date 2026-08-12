/**
 * Who is signed in, asked once when a page loads.
 *
 * The session lives in two HttpOnly cookies this code cannot read, so a fresh
 * tab starts with an empty store while the parent is still perfectly signed in.
 * Without this, the header offers nothing and the sign-in screen offers itself
 * to somebody who has no reason to be there.
 *
 * It is a file of its own rather than a few lines in the shell, for the reason
 * `sign_out_request.ts` is: it reaches for the api client, and the shell has no
 * other business doing that.
 */
import type { ApiClient } from "$lib/api/client";
import { api } from "$lib/session/client";
import { auth } from "$lib/stores/auth";

/** What this needs from the api client, and nothing more. */
export type SessionReader = Pick<ApiClient, "whoIsSignedIn">;

/**
 * Asks the api who the cookies belong to, and records the answer.
 *
 * Note:
 * - it asks `whoIsSignedIn` rather than `me`, because nobody being signed in is
 *   the ordinary answer on a first visit. `me` reports that as a session ending,
 *   which would send somebody reading `/status` to the sign-in screen.
 * - a session that genuinely ended is still reported, by the api client, and
 *   the notice it raises is left where it is.
 *
 * Param:
 * client - SessionReader (the api client, injected so a test needs no network)
 *
 * Return:
 * - true when a session was found and recorded
 * - false when nobody was signed in
 */
export async function restoreSession(client: SessionReader = api): Promise<boolean> {
    const session = await client.whoIsSignedIn();
    if (session === null) {
        return false;
    }

    auth.signIn(session);
    auth.acknowledgeNotice();

    return true;
}

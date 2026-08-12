/**
 * What happens when the parent asks to sign out.
 *
 * It is the other half of `sign_out.ts`. That file ends the session on this
 * device and is what runs when the api has already ended it: an expired session,
 * a refresh token used twice. This one runs when nothing is wrong and the parent
 * simply asked, so it has one extra job to do first, which is to tell the api.
 *
 * The two are separate files because `client.ts` imports the local half, so the
 * half that reaches for the client cannot live beside it.
 */
import type { ApiClient } from "$lib/api/client";
import { api } from "$lib/session/client";
import { hardSignOut } from "$lib/session/sign_out";

/** What this needs from the api client, and nothing more. */
export type SignOutCaller = Pick<ApiClient, "logout">;

/**
 * Ends the session everywhere, then clears this device.
 *
 * Note:
 * - the api is told first, because that is the call that revokes the refresh
 *   token and expires both cookies. Clearing this tab alone would leave a
 *   session a stolen cookie could still be used with, which is the one thing a
 *   sign out is for.
 * - a refused or unreachable api does not stop the local half. A parent who
 *   pressed sign out on a shared machine must end up signed out of the screen in
 *   front of them whatever the network did, so the failure is swallowed here on
 *   purpose rather than shown.
 * - the reason is `requested`, which is the one reason the sign-in screen shows
 *   no notice for. Nothing went wrong, so there is nothing to explain.
 *
 * Param:
 * client - SignOutCaller (the api client, injected so a test needs no network)
 *
 * Return:
 * - resolves once the parent is on the sign-in screen
 */
export async function requestedSignOut(client: SignOutCaller = api): Promise<void> {
    try {
        await client.logout();
    } catch {
        // Deliberately nothing. The local half below is what the parent can see,
        // and it runs either way.
    }

    await hardSignOut("requested");
}

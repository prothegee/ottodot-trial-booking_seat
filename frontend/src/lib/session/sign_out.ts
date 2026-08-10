/**
 * What a hard sign out does.
 *
 * It is one function rather than three lines repeated at every call site,
 * because forgetting one of them is how a signed-out screen ends up still
 * showing the previous parent's children.
 */
import { goto } from "$app/navigation";

import { tokenReusedCode } from "$lib/api/errors";
import { classCache } from "$lib/session/cache";
import { auth, type SignOutReason } from "$lib/stores/auth";

/** Where a signed-out parent lands. */
export const signInRoute = "/sign-in";

/**
 * Turns an api error code into the reason the sign-in screen shows.
 *
 * `token_reused` is called out on its own because it means a revoked refresh
 * token was presented. That is either a stale tab or someone else holding a
 * copy, and the parent deserves to be told rather than quietly returned to the
 * sign-in form.
 */
export function reasonForCode(code: string): SignOutReason {
    if (code === tokenReusedCode) {
        return "token_reused";
    }

    return "session_ended";
}

/**
 * Ends the session on this device and sends the parent back to sign in.
 *
 * Note:
 * - the cache is emptied in memory as well as in storage. Clearing storage
 *   alone would leave the previous parent's class list in a map that outlives
 *   the sign out, since nothing reloads the page.
 * - sessionStorage is cleared wholesale rather than key by key. This client is
 *   the only thing writing to it, and a stale entry surviving a sign out would
 *   be the one that leaks between two parents on a shared machine.
 *
 * Param:
 * reason - SignOutReason (what the sign-in screen should say)
 *
 * Return:
 * - resolves once the parent is on the sign-in route
 */
export async function hardSignOut(reason: SignOutReason): Promise<void> {
    auth.signOut(reason);
    classCache.clear();
    clearClientStorage();

    await goto(signInRoute);
}

/** Empties the browser storage this client owns, if there is any. */
function clearClientStorage(): void {
    if (typeof sessionStorage === "undefined") {
        return;
    }

    sessionStorage.clear();
}

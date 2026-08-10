/**
 * What this client knows about who is signed in.
 *
 * Memory only. It is deliberately not mirrored into sessionStorage, because the
 * session itself lives in HttpOnly cookies that this code cannot read. Writing
 * a copy of the parent's details to disk would create the only durable trace of
 * them anywhere in the browser, for no benefit: a reload asks the api again and
 * gets the truth.
 */
import { writable } from "svelte/store";

import type { Child, Session } from "$lib/api/types";

/** Why the parent is looking at the sign-in screen. */
export type SignOutReason =
    | "requested"
    | "session_ended"
    | "token_reused";

/** Everything the store holds. */
export interface AuthState {
    session: Session | null;

    /** Set only when the sign out was not asked for, so the screen can say why. */
    signedOutReason: SignOutReason | null;
}

const signedOutState: AuthState = { session: null, signedOutReason: null };

/**
 * Copies across only the fields this client is allowed to hold.
 *
 * This is the guard, not a formality. If the api ever starts sending an email
 * or anything else alongside the session, it stops here rather than ending up
 * in a store that a component might render.
 */
function onlyWhatIsDisplayed(session: Session): Session {
    const children: Child[] = (session.children ?? []).map((child) => ({
        id: child.id,
        full_name: child.full_name,
        grade_level: child.grade_level,
    }));

    return {
        parent_id: session.parent_id,
        display_name: session.display_name,
        role: session.role,
        children,
    };
}

function createAuthStore() {
    const { subscribe, set, update } = writable<AuthState>(signedOutState);

    return {
        subscribe,

        /** Records the session the api just described. */
        signIn(session: Session): void {
            set({ session: onlyWhatIsDisplayed(session), signedOutReason: null });
        },

        /** Drops everything held about the parent and records why. */
        signOut(reason: SignOutReason): void {
            set({ session: null, signedOutReason: reason });
        },

        /** Clears the notice once the sign-in screen has shown it. */
        acknowledgeNotice(): void {
            update((state) => ({ ...state, signedOutReason: null }));
        },
    };
}

/** The one auth store. */
export const auth = createAuthStore();

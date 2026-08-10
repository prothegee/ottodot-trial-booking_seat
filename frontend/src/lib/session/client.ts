/**
 * The api client the application actually uses, wired once.
 *
 * Everything is injected everywhere else so that tests can supply a fake. This
 * is the one file where the real transport, the real base url, and the real
 * sign-out behaviour are put together, which keeps that wiring out of every
 * route.
 */
import { createApiClient } from "$lib/api/client";
import { createFetchTransport } from "$lib/api/transport";
import { apiBaseUrl } from "$lib/config/environment";
import { hardSignOut, reasonForCode } from "$lib/session/sign_out";

/** The application wide api client. */
export const api = createApiClient({
    transport: createFetchTransport(apiBaseUrl),
    onSignOut: (failure) => {
        // Fire and forget on purpose. The call that failed is already on its
        // way back to its caller with the reason, and the navigation must not
        // be something that caller has to wait for.
        void hardSignOut(reasonForCode(failure.code));
    },
});

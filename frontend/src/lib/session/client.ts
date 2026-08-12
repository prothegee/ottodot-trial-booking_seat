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
import { apiBaseUrlForThisPage } from "$lib/config/api_base_url";
import { hardSignOut, reasonForCode } from "$lib/session/sign_out";

/** The application wide api client. */
export const api = createApiClient({
    // The configured address, with its host aligned to the page's when both are
    // loopback names. Without that, a reviewer who opens localhost:9001 signs in
    // successfully and is refused by the very next call.
    transport: createFetchTransport(apiBaseUrlForThisPage()),
    onSignOut: (failure) => {
        // Fire and forget on purpose. The call that failed is already on its
        // way back to its caller with the reason, and the navigation must not
        // be something that caller has to wait for.
        void hardSignOut(reasonForCode(failure.code));
    },
});

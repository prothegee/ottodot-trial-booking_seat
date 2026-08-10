/**
 * The single owner of every call to the Go api.
 *
 * Nothing else in this frontend calls fetch, and nothing else decides what a
 * failure means. That concentration is what makes the silent refresh possible:
 * there is exactly one place where a 401 can be noticed.
 */
import { ApiError, toApiError, tokenExpiredCode } from "$lib/api/errors";
import { createRefreshCoordinator } from "$lib/api/refresh";
import type { Transport, TransportRequest, TransportResponse } from "$lib/api/transport";
import type { LoginRequest, Session } from "$lib/api/types";

/** Where the auth calls live. */
export const authPaths = {
    login: "/api/v1/auth/login",
    refresh: "/api/v1/auth/refresh",
    logout: "/api/v1/auth/logout",
    me: "/api/v1/auth/me",
} as const;

/** The request header that carries a stored tag back to the api. */
export const ifNoneMatchHeader = "if-none-match";

/** The response header the api answers with. */
export const etagHeader = "etag";

/**
 * The answer to a conditional GET, before anything decides what it means.
 *
 * The status is not exposed. A caller needs to know whether its stored copy
 * still stands, not which number said so.
 */
export interface ConditionalAnswer<T> {
    /** True when the api said the stored copy is still current. */
    notModified: boolean;

    /** The new body, present only when the api sent one. */
    body: T | undefined;

    /** The tag that came with a new body, or an empty string. */
    etag: string;
}

/** What the client is built with. */
export interface ApiClientOptions {
    transport: Transport;

    /**
     * Called once when the session is over and cannot be recovered. The client
     * only reports it, the caller decides what a sign out does.
     */
    onSignOut: (failure: ApiError) => void;
}

/** Every call this phase of the client can make. */
export interface ApiClient {
    /** One call, with the refresh and the single retry already handled. */
    request<T>(request: TransportRequest): Promise<T>;

    /**
     * One GET that offers a stored tag back to the api.
     *
     * The tag is sent verbatim and never inspected. An empty tag sends no
     * header at all, which is the plain unconditional GET.
     */
    conditionalGet<T>(path: string, etag: string): Promise<ConditionalAnswer<T>>;

    login(email: string): Promise<void>;
    me(): Promise<Session>;
    logout(): Promise<void>;
}

/**
 * Builds the client.
 *
 * Note:
 * - a 401 token_expired triggers one refresh and one retry, ever. Never a
 *   loop. If the retry comes back unauthorised too, the session is over.
 * - the refresh call itself does not go through that path, which is what keeps
 *   a failed refresh from signing the parent out twice.
 *
 * Param:
 * options - ApiClientOptions (the transport, and what to do on a hard sign out)
 *
 * Return:
 * - the client
 */
export function createApiClient(options: ApiClientOptions): ApiClient {
    const { transport, onSignOut } = options;

    /** One round trip. It maps failures, and does nothing else about them. */
    async function sendOnce(request: TransportRequest): Promise<TransportResponse> {
        const response = await transport.send(request);

        // A 304 is a success carrying no body, never a failure. The internal
        // cache depends on this staying true, and so does every caller that
        // never asked a conditional question and can ignore it.
        if (response.status < 400) {
            return response;
        }

        throw toApiError(response.status, response.body);
    }

    const coordinator = createRefreshCoordinator({
        refresh: async () => {
            await sendOnce({ method: "POST", path: authPaths.refresh });
        },
        onFailure: (failure: unknown) => {
            reportSignedOut(failure);
        },
    });

    /** Reports a session that is over, once, and only for that kind. */
    function reportSignedOut(failure: unknown): void {
        if (failure instanceof ApiError && failure.kind === "SignedOut") {
            onSignOut(failure);
        }
    }

    /** The whole pipeline: one call, one refresh, one retry, and no more. */
    async function sendWithRefresh(outgoing: TransportRequest): Promise<TransportResponse> {
        try {
            return await sendOnce(outgoing);
        } catch (failure) {
            if (!(failure instanceof ApiError)) {
                throw failure;
            }

            if (failure.code !== tokenExpiredCode) {
                reportSignedOut(failure);

                throw failure;
            }

            // One refresh, shared with every other caller waiting on it. If it
            // fails, the coordinator has already reported the sign out.
            await coordinator.run();

            try {
                return await sendOnce(outgoing);
            } catch (afterRetry) {
                reportSignedOut(afterRetry);

                throw afterRetry;
            }
        }
    }

    async function request<T>(outgoing: TransportRequest): Promise<T> {
        const response = await sendWithRefresh(outgoing);

        return response.body as T;
    }

    return {
        request,

        async conditionalGet<T>(path: string, etag: string): Promise<ConditionalAnswer<T>> {
            // An empty tag means nothing worth revalidating is held, so the
            // header is left off rather than sent empty. An empty
            // If-None-Match is not the same question.
            const headers = etag === "" ? undefined : { [ifNoneMatchHeader]: etag };

            const response = await sendWithRefresh({ method: "GET", path, headers });

            return {
                notModified: response.status === 304,
                body: response.body as T | undefined,
                etag: response.headers[etagHeader] ?? "",
            };
        },

        async login(email: string): Promise<void> {
            const body: LoginRequest = { email };

            // Signing in deliberately skips the refresh path. A refusal here
            // means the wrong email, and no amount of refreshing fixes that.
            await sendOnce({ method: "POST", path: authPaths.login, body });
        },

        me(): Promise<Session> {
            return request<Session>({ method: "GET", path: authPaths.me });
        },

        async logout(): Promise<void> {
            await sendOnce({ method: "POST", path: authPaths.logout });
        },
    };
}

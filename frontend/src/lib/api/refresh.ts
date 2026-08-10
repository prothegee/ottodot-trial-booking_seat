/**
 * One refresh at a time, however many calls are waiting on it.
 *
 * Five parallel requests that all come back 401 must cause exactly one refresh,
 * not five. Five refreshes would rotate the refresh token five times, and the
 * backend treats a second use of a rotated token as reuse, which revokes the
 * whole family and signs the parent out. So the naive version does not merely
 * waste calls, it breaks the session it was trying to save.
 */

/** Runs the refresh, collapsing concurrent callers into one call. */
export interface RefreshCoordinator {
    run(): Promise<void>;
}

/** What the coordinator needs to do its job. */
export interface RefreshOptions {
    /** Performs one refresh call. It rejects when the refresh is refused. */
    refresh: () => Promise<void>;

    /**
     * Called once per failed refresh, never once per waiting caller. This is
     * what keeps a hard sign out from happening five times over.
     */
    onFailure: (failure: unknown) => void;
}

/**
 * Builds the coordinator.
 *
 * Note:
 * - the in-flight promise is cleared once it settles, so a later 401 starts a
 *   fresh attempt rather than reusing a stale rejection.
 *
 * Param:
 * options - RefreshOptions (the call to make, and what to do when it fails)
 *
 * Return:
 * - a coordinator whose run() resolves when the refresh succeeded, and rejects
 *   with the refresh failure otherwise
 */
export function createRefreshCoordinator(options: RefreshOptions): RefreshCoordinator {
    let inFlight: Promise<void> | null = null;

    return {
        run(): Promise<void> {
            if (inFlight !== null) {
                return inFlight;
            }

            const attempt = Promise.resolve()
                .then(() => options.refresh())
                .catch((failure: unknown) => {
                    options.onFailure(failure);

                    throw failure;
                })
                .finally(() => {
                    inFlight = null;
                });

            inFlight = attempt;

            return attempt;
        },
    };
}

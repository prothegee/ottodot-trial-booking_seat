<script lang="ts">
    import VersionFooter from "$lib/components/VersionFooter.svelte";
    import { restoreSession } from "$lib/session/restore";
    import { requestedSignOut } from "$lib/session/sign_out_request";
    import { auth } from "$lib/stores/auth";

    let { children } = $props();

    // Asked once, when the page loads. The session is two cookies this code
    // cannot read, so every fresh tab starts knowing nothing about a parent who
    // is still signed in: the header offered them no way to their bookings, and
    // the sign-in screen offered them a form they had already filled in.
    //
    // It reads nothing reactive, so it runs on mount and never again.
    $effect(() => {
        void restoreSession();
    });

    /**
     * True while the sign out is in flight.
     *
     * Two presses would send two logouts at the same refresh token, and the
     * second one is refused as a reuse. That refusal is the api telling the
     * truth about a token it has already revoked, but it would reach the parent
     * as the alarming notice meant for a stolen session.
     */
    let signingOut = $state(false);

    async function signOut(): Promise<void> {
        if (signingOut) {
            return;
        }

        signingOut = true;

        try {
            await requestedSignOut();
        } finally {
            signingOut = false;
        }
    }
</script>

<div class="shell">
    <header class="shell-header">
        <a class="shell-title" href="/">Trial Class Booking</a>

        <!--
            Shown only to somebody who is signed in. There is nothing to end
            otherwise, and a sign out offered on the sign-in screen is a control
            that can only confuse.
        -->
        {#if $auth.session !== null}
            <div class="shell-session">
                <!--
                    The only way back to a booking once the screen that made it
                    has gone. Every link to one is on the payment screen, and a
                    parent who closed the tab after paying had nothing left to
                    click. It is here rather than on the class list because the
                    question it answers, did the payment land, arrives while
                    they are looking at something else.
                -->
                <a class="shell-link" href="/bookings" data-testid="my-bookings">Your bookings</a>

                <span class="shell-parent" data-testid="signed-in-as">{$auth.session.display_name}</span>

                <button
                    type="button"
                    class="shell-sign-out"
                    disabled={signingOut}
                    onclick={signOut}
                    data-testid="sign-out"
                >
                    {signingOut ? "Signing out" : "Sign out"}
                </button>
            </div>
        {/if}
    </header>

    <main class="shell-main">
        {@render children()}
    </main>

    <VersionFooter />
</div>

<style>
    /*
        The palette, named once.

        Every screen in this client drew from the same set of colours already,
        written out as hex in each file. Naming them here changes no colour and
        makes one thing true that was not: red means a refusal in exactly one
        place, so it cannot be nearly-red on one screen and quite-red on the
        next.

        The names say what a colour is for rather than what it looks like. A
        token called --danger survives a decision to make refusals orange, and
        one called --red does not.
    */
    :global(:root) {
        --ink: #111827;
        --ink-soft: #374151;
        --muted: #6b7280;
        --muted-soft: #9ca3af;

        --line: #e5e7eb;
        --line-strong: #d1d5db;

        --surface: #ffffff;
        --page: #f9fafb;

        --accent: #1d4ed8;
        --on-accent: #ffffff;

        --danger: #b91c1c;
        --danger-surface: #fee2e2;

        --good: #047857;
        --good-strong: #15803d;

        --warn: #92400e;
        --warn-strong: #b45309;
        --warn-surface: #fef3c7;
    }

    :global(body) {
        margin: 0;
        font-family:
            system-ui,
            -apple-system,
            "Segoe UI",
            sans-serif;
        color: var(--ink);
        background: var(--page);
    }

    .shell {
        display: flex;
        flex-direction: column;
        min-height: 100vh;
    }

    .shell-header {
        display: flex;
        align-items: baseline;
        justify-content: space-between;
        gap: 1rem;
        padding: 1rem 1.5rem;
        background: var(--surface);
        border-bottom: 1px solid var(--line);
    }

    .shell-title {
        font-size: 1.1rem;
        font-weight: 600;
        color: var(--ink);
        text-decoration: none;
    }

    .shell-session {
        display: flex;
        align-items: baseline;
        gap: 0.75rem;
    }

    .shell-link {
        font-size: 0.9rem;
        font-weight: 600;
        color: var(--accent);
        text-decoration: none;
    }

    .shell-parent {
        font-size: 0.9rem;
        color: var(--muted);
    }

    /*
        Deliberately quieter than the buttons on the screens below it. Every one
        of those moves a booking forward, and the one control that undoes a
        session should not compete with them for a tired parent's attention.
    */
    .shell-sign-out {
        padding: 0.35rem 0.75rem;
        font-size: 0.85rem;
        font-weight: 600;
        font-family: inherit;
        color: var(--ink-soft);
        background: var(--surface);
        border: 1px solid var(--line-strong);
        border-radius: 0.25rem;
        cursor: pointer;
    }

    .shell-sign-out:disabled {
        color: var(--muted);
        cursor: progress;
    }

    /*
        Centred and capped. Each screen already sets the width its own content
        reads well at, and without this they all sit against the left edge of a
        wide monitor with the rest of it empty.
    */
    .shell-main {
        flex: 1;
        width: 100%;
        max-width: 64rem;
        margin-inline: auto;
        padding: 1.5rem;
        box-sizing: border-box;
    }

    /*
        A phone. The padding is what has to give first: at 360px, 1.5rem on each
        side is a sixth of the screen spent on nothing.
    */
    @media (max-width: 40rem) {
        .shell-header {
            padding: 0.75rem 1rem;
        }

        .shell-main {
            padding: 1rem;
        }

        /*
            The name is the first thing to give. At 360px it pushes the title and
            the control onto two lines, and it is the only one of the three that
            tells a parent nothing they did not already know.
        */
        .shell-parent {
            display: none;
        }
    }
</style>

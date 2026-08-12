<script lang="ts">
    import { onDestroy, onMount } from "svelte";

    import { mockCaptchaToken } from "$lib/booking/bot_signals";

    interface Props {
        /**
         * Called once with the token this widget produced.
         *
         * The token is passed through untouched. That is the contract a real
         * provider has: the client never reads it, never shortens it, and never
         * decides anything from it, because only the provider can say what it
         * means.
         */
        onToken: (token: string) => void;
    }

    let { onToken }: Props = $props();

    /**
     * How long the mock takes to "solve".
     *
     * A real widget takes a moment, and a screen that renders as though it were
     * instant would hide the one state worth designing for: submitting while the
     * challenge has not answered yet.
     */
    const solveDelayMs = 300;

    /** Where the challenge is: waiting, or done. */
    let solved = $state(false);

    let timer: ReturnType<typeof setTimeout> | undefined;

    onMount(() => {
        timer = setTimeout(() => {
            solved = true;

            onToken(mockCaptchaToken);
        }, solveDelayMs);
    });

    onDestroy(() => {
        // A timer that outlives the screen would call back into a component
        // nobody is looking at, which in a test is a warning and in a browser is
        // a leak.
        if (timer !== undefined) {
            clearTimeout(timer);
        }
    });
</script>

<!--
    A mock, in the shape a real provider's widget has: it mounts, it takes a
    moment, and it hands back one opaque token. Swapping in Turnstile or
    hCaptcha replaces this file and nothing else, because nothing outside it
    knows what a token looks like.

    It is deliberately the weakest layer. Everything above it, the token, the
    ownership check, the rate limit, and the hold cap, works on properties a bot
    cannot argue with. A challenge only raises the cost of pretending to be a
    person.
-->
<div class="captcha" data-testid="captcha-widget" data-solved={solved}>
    <span class="mark" aria-hidden="true">{solved ? "[ok]" : "[..]"}</span>

    <p class="label" role="status">
        {#if solved}
            Verified that you are a person
        {:else}
            Checking that you are a person
        {/if}
    </p>

    <p class="note">Mock challenge. No third party is contacted.</p>
</div>

<style>
    .captcha {
        display: grid;
        grid-template-columns: auto 1fr;
        align-items: center;
        gap: 0.25rem 0.5rem;
        padding: 0.5rem 0.75rem;
        background: var(--page);
        border: 1px solid var(--line);
        border-radius: 0.25rem;
    }

    .mark {
        font-size: 1.1rem;
        color: var(--good-strong);
    }

    .label {
        margin: 0;
        font-size: 0.9rem;
    }

    .note {
        grid-column: 2;
        margin: 0;
        font-size: 0.75rem;
        color: var(--muted);
    }
</style>

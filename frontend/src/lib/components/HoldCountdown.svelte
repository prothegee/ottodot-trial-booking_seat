<script lang="ts">
    import { onDestroy } from "svelte";

    import { remainingFor } from "$lib/booking/countdown";

    interface Props {
        /** The hold_expires_at the api sent, or null when nothing is held. */
        deadline: string | null;

        /**
         * Called once, the first time the deadline is reached.
         *
         * Once rather than every tick, because what it does is ask the api what
         * really happened, and asking every second would be a poll nobody
         * asked for.
         */
        onExpire?: () => void;

        /** Handed in only so a test can move the clock instead of waiting. */
        now?: () => number;
    }

    let { deadline, onExpire, now = () => Date.now() }: Props = $props();

    // The tick is what makes the label move. It is not the source of truth:
    // every render works the remainder out from the deadline and the clock, so
    // a tab that was suspended for five minutes shows the right number on its
    // first frame back rather than catching up one second at a time.
    let tick = $state(0);

    const remaining = $derived.by(() => {
        // Read so this recomputes on every tick. Without it the label is
        // worked out once and then never again.
        void tick;

        return remainingFor(deadline, now());
    });

    // Fired at most once. A parent whose hold ran out is asked about it once,
    // and then left alone.
    let reported = false;

    const timer = setInterval(() => {
        tick += 1;
    }, 1000);

    onDestroy(() => {
        clearInterval(timer);
    });

    $effect(() => {
        if (remaining.expired && !reported) {
            reported = true;

            onExpire?.();
        }
    });
</script>

<p class="countdown" class:expired={remaining.expired} data-testid="hold-countdown">
    {#if remaining.expired}
        <span data-testid="hold-countdown-label">Your hold has ended</span>
    {:else}
        <span data-testid="hold-countdown-label">{remaining.label} left to pay</span>
    {/if}
</p>

<style>
    .countdown {
        margin: 0;
        font-size: 1.1rem;
        font-weight: 600;
        font-variant-numeric: tabular-nums;
        color: var(--good);
    }

    .countdown.expired {
        color: var(--danger);
    }
</style>

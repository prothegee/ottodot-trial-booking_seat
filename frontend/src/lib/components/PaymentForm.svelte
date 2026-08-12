<script lang="ts">
    import { onMount } from "svelte";

    import { botSignals, type BotSignals } from "$lib/booking/bot_signals";
    import CaptchaWidget from "$lib/components/CaptchaWidget.svelte";

    interface Props {
        /** What this trial costs, in the smallest unit, as the api states it. */
        amountCents: number;

        /** The currency the amount is in. */
        currency: string;

        /** True while a call the parent is waiting on is in flight. */
        submitting: boolean;

        /**
         * True when there is nothing left to pay for: the hold ended, or the
         * booking is no longer waiting on money.
         */
        closed?: boolean;

        /** What the parent is trying again after, or an empty string. */
        retryOf?: string;

        /**
         * Called with what this form measured about its own submission.
         *
         * The signals are handed up rather than sent from here, because this
         * component owns a form and not a request. The screen owns the call.
         */
        onSubmit: (signals: BotSignals) => void;
    }

    let { amountCents, currency, submitting, closed = false, retryOf = "", onSubmit }: Props = $props();

    /**
     * The amount as a person reads it.
     *
     * The api sends cents because money in a floating point number is how a
     * cent goes missing. Dividing here, at the last moment before it is
     * rendered, is the only place that division is safe.
     */
    const amount = $derived(
        new Intl.NumberFormat(undefined, { style: "currency", currency }).format(amountCents / 100),
    );

    /**
     * Whether the control is dead.
     *
     * The in-flight half is the double submit guard: one click disables the
     * button for the whole round trip, so a parent tapping twice sends one
     * request. The idempotency key would make a second one harmless anyway,
     * which is the point. This is the cheap layer on top of the correct one,
     * not instead of it.
     */
    const stopped = $derived(submitting || closed);

    /**
     * The honeypot.
     *
     * It is bound rather than read from the DOM at submit time, so what is sent
     * is what the field holds. A person never touches it: it is off screen, it
     * is not focusable, it is hidden from assistive technology, and the browser
     * is told not to fill it in.
     */
    let honeypot = $state("");

    /**
     * When this form appeared.
     *
     * The measurement runs from mount rather than from the first keystroke,
     * because there is nothing here to type: the parent reads an amount and
     * presses a button, and the time between those two is the whole signal.
     */
    let mountedAt = 0;

    /** What the challenge widget produced, empty until it answers. */
    let captchaToken = $state("");

    onMount(() => {
        mountedAt = now();
    });

    /**
     * The clock the measurement uses.
     *
     * performance.now is monotonic, so a system clock correction mid-form
     * cannot produce a negative elapsed time. Date.now is the fallback for an
     * environment without it, and the arithmetic guards the negative case
     * anyway.
     */
    function now(): number {
        if (typeof performance !== "undefined" && typeof performance.now === "function") {
            return performance.now();
        }

        return Date.now();
    }

    function receiveToken(token: string): void {
        captchaToken = token;
    }

    function submit(event: SubmitEvent): void {
        event.preventDefault();

        if (stopped) {
            return;
        }

        // Nothing is checked here. A client that refused its own submission
        // would teach a script exactly which value to change, and would turn a
        // slow connection into a refusal. The backend weighs the evidence.
        onSubmit(botSignals(honeypot, mountedAt, now(), captchaToken));
    }
</script>

<form class="payment" onsubmit={submit} data-testid="payment-form">
    <p class="amount" data-testid="payment-amount">{amount}</p>

    <p class="note">
        This is a mock payment. No card details are asked for, and none are ever
        sent or stored.
    </p>

    <!--
        The honeypot. Four things keep it away from a person and none of them is
        `type="hidden"`, which is the one thing a script does check for:

        it is moved off screen rather than removed, so it is a real field
        aria-hidden keeps it out of the accessibility tree
        tabindex -1 keeps it out of the keyboard order
        autocomplete off stops a browser filling it in on the parent's behalf
    -->
    <div class="trap" aria-hidden="true">
        <label for="website">Website</label>
        <input
            id="website"
            name="website"
            type="text"
            tabindex="-1"
            autocomplete="off"
            bind:value={honeypot}
            data-testid="payment-honeypot"
        />
    </div>

    <CaptchaWidget onToken={receiveToken} />

    <button type="submit" disabled={stopped} data-testid="payment-submit">
        {#if submitting}
            Paying
        {:else if closed}
            Payment closed
        {:else if retryOf !== ""}
            Try the payment again
        {:else}
            Pay {amount}
        {/if}
    </button>
</form>

<style>
    .payment {
        display: flex;
        flex-direction: column;
        gap: 0.75rem;
        padding: 1rem;
        background: var(--surface);
        border: 1px solid var(--line);
        border-radius: 0.5rem;
    }

    .amount {
        margin: 0;
        font-size: 1.75rem;
        font-weight: 700;
    }

    .note {
        margin: 0;
        font-size: 0.85rem;
        color: var(--muted);
    }

    /*
        Off screen rather than display:none. A field that is not rendered is a
        field a script can see is not rendered, and `type="hidden"` is the first
        thing one looks for. This is still a real, laid out input that nobody
        can see, reach with the keyboard, or hear read out.
    */
    .trap {
        position: absolute;
        left: -9999px;
        width: 1px;
        height: 1px;
        overflow: hidden;
    }

    button {
        padding: 0.5rem;
        font-size: 1rem;
        font-weight: 600;
        color: var(--on-accent);
        background: var(--accent);
        border: none;
        border-radius: 0.25rem;
        cursor: pointer;
    }

    button:disabled {
        background: var(--muted-soft);
        cursor: not-allowed;
    }
</style>

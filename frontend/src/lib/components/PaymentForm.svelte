<script lang="ts">
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

        onSubmit: () => void;
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

    function submit(event: SubmitEvent): void {
        event.preventDefault();

        if (stopped) {
            return;
        }

        onSubmit();
    }
</script>

<form class="payment" onsubmit={submit} data-testid="payment-form">
    <p class="amount" data-testid="payment-amount">{amount}</p>

    <p class="note">
        This is a mock payment. No card details are asked for, and none are ever
        sent or stored.
    </p>

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
        background: #ffffff;
        border: 1px solid #e5e7eb;
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
        color: #6b7280;
    }

    button {
        padding: 0.5rem;
        font-size: 1rem;
        font-weight: 600;
        color: #ffffff;
        background: #1d4ed8;
        border: none;
        border-radius: 0.25rem;
        cursor: pointer;
    }

    button:disabled {
        background: #9ca3af;
        cursor: not-allowed;
    }
</style>

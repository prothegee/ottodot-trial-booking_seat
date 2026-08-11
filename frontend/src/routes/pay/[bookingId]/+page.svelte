<script lang="ts">
    import { goto } from "$app/navigation";
    import { page } from "$app/state";

    import type { BotSignals } from "$lib/api/types";
    import { trialCurrency, trialPayment, trialPriceCents } from "$lib/booking/price";
    import HoldCountdown from "$lib/components/HoldCountdown.svelte";
    import PaymentForm from "$lib/components/PaymentForm.svelte";
    import { booking } from "$lib/stores/booking";

    const bookingId = $derived(page.params.bookingId ?? "");

    // The booking is read on arrival rather than taken from whatever the store
    // happens to hold. A parent who opened this link in a second tab, or came
    // back to it an hour later, has to see the real state and not the one this
    // tab remembers.
    $effect(() => {
        void booking.load(bookingId);
    });

    const held = $derived($booking.booking);
    const failure = $derived($booking.failure);

    /** True while the booking is still the one waiting for money. */
    const awaitingPayment = $derived(held !== null && held.status === "pending_payment");

    /**
     * Whether the hold has run out on this screen.
     *
     * It is tracked here rather than read from the booking, because the whole
     * point of the countdown is that it reaches zero before the api has been
     * asked. The control closes at once, and the real answer is confirmed a
     * moment later.
     */
    let holdEnded = $state(false);

    /**
     * What the parent is being told, and what they can do about it.
     *
     * The three cases are separated because they are genuinely different, and
     * running them together is how a screen ends up claiming something it does
     * not know:
     *
     * a decline took no money and can be tried again
     * a lost seat took money and is already being refunded, with nothing to do
     * an `internal_error` says nothing at all, so it claims nothing and offers
     * the retry the idempotency key makes safe
     */
    const declined = $derived(failure?.kind === "PaymentDeclined");
    const seatLost = $derived(failure?.kind === "SeatLost");
    const broke = $derived(failure?.kind === "Unavailable");

    /** A retry is offered for the two failures that leave a way forward. */
    const canRetry = $derived(declined || broke);

    /** The control is dead once the hold ends or the booking stops waiting. */
    const closed = $derived(holdEnded || !awaitingPayment || seatLost);

    async function pay(signals: BotSignals): Promise<void> {
        if ($booking.submitting || closed) {
            return;
        }

        // The signals travel with the charge. Nothing here reads them: the form
        // measured, this screen carries, and the api decides.
        const settled = await booking.pay(bookingId, trialPayment(signals));

        if (settled === null) {
            // The reason is in the store and this screen is already showing it.
            // Nothing is thrown away: the booking stays on screen, because a
            // declined payment leaves a hold that is still standing.
            return;
        }

        await goto(`/booking/${settled.id}`);
    }

    /**
     * What happens when the countdown reaches zero.
     *
     * The control closes first, without waiting for anything, so a parent
     * cannot submit into a hold that has gone. Then the api is asked what
     * really happened, because a clock in a browser is not what decides.
     */
    function holdReachedZero(): void {
        holdEnded = true;

        void booking.load(bookingId);
    }
</script>

<section class="pay">
    <h1>Pay for your trial</h1>

    {#if held === null}
        <p class="loading" data-testid="pay-loading">Loading your booking</p>
    {:else}
        {#if awaitingPayment}
            <HoldCountdown deadline={held.hold_expires_at} onExpire={holdReachedZero} />
        {/if}

        <PaymentForm
            amountCents={trialPriceCents}
            currency={trialCurrency}
            submitting={$booking.submitting}
            {closed}
            retryOf={canRetry ? (failure?.kind ?? "") : ""}
            onSubmit={pay}
        />

        {#if failure !== null}
            <p class="failure" role="alert" data-testid="pay-failure">{failure.message}</p>
        {/if}

        {#if seatLost}
            <!--
                Terminal, and deliberately without a retry control. The money
                moved and is already coming back, so there is nothing here for
                the parent to do, and a button would invite them to pay twice.
            -->
            <p class="terminal" data-testid="pay-seat-lost">
                There is nothing to do. Your payment is being refunded
                automatically.
            </p>

            <p class="onward"><a href="/booking/{bookingId}">See this booking</a></p>
        {/if}

        {#if broke}
            <!--
                The one failure that says nothing about the booking. It never
                claims the seat was lost and never claims the payment failed,
                because neither is known from here.
            -->
            <p class="terminal" data-testid="pay-unknown-outcome">
                Your booking has not been changed. You can try the payment
                again.
            </p>

            {#if failure?.requestId !== ""}
                <p class="reference" data-testid="pay-request-id">
                    Quote reference {failure?.requestId} if you get in touch.
                </p>
            {/if}
        {/if}

        {#if holdEnded && awaitingPayment}
            <p class="terminal" data-testid="pay-hold-ended">
                Checking what happened to your hold.
            </p>
        {/if}

        {#if !awaitingPayment}
            <p class="onward" data-testid="pay-not-holding">
                <a href="/booking/{bookingId}">See where this booking stands</a>
            </p>
        {/if}
    {/if}

    <p class="back"><a href="/">Back to the class list</a></p>
</section>

<style>
    .pay {
        display: flex;
        flex-direction: column;
        gap: 0.75rem;
        max-width: 32rem;
    }

    h1 {
        margin: 0;
        font-size: 1.5rem;
    }

    .loading {
        margin: 0;
        color: #6b7280;
    }

    .failure {
        margin: 0;
        padding: 0.5rem 0.75rem;
        font-size: 0.9rem;
        color: #b91c1c;
        background: #fee2e2;
        border-radius: 0.25rem;
    }

    .terminal {
        margin: 0;
        font-size: 0.9rem;
        color: #374151;
    }

    .reference {
        margin: 0;
        font-size: 0.75rem;
        color: #6b7280;
    }

    .onward {
        margin: 0;
        font-size: 0.9rem;
    }

    .back {
        margin-top: 1rem;
        font-size: 0.9rem;
    }
</style>

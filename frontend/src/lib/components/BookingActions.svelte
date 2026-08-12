<script lang="ts">
    import { goto } from "$app/navigation";

    import { ApiError } from "$lib/api/errors";
    import type { Booking } from "$lib/api/types";
    import { requestedCancel } from "$lib/booking/cancel_request";

    interface Props {
        booking: Booking;

        /** Whether to offer the way in to this booking's own screen. */
        showOpen?: boolean;

        /** Whether to offer the payment screen for a booking still holding. */
        showPay?: boolean;

        /**
         * Called once a cancel has gone through, so the screen above can read
         * the booking back. This component changed one booking and knows
         * nothing about what else is on screen.
         */
        onCancelled?: () => void;
    }

    let { booking, showOpen = false, showPay = false, onCancelled }: Props = $props();

    /**
     * Whether this booking can still be withdrawn here.
     *
     * Only a hold. A confirmed booking took money, and the api sends none of it
     * back on a cancel, so offering the control there would be a button that
     * quietly costs a parent the fee.
     */
    const withdrawable = $derived(booking.status === "pending_payment");

    /** True once the parent asked, before they said yes or no. */
    let confirming = $state(false);

    /** True only while the cancel is in flight. */
    let cancelling = $state(false);

    let failure = $state("");

    /**
     * What a refused cancel says.
     *
     * `InvalidRequest` is called out because the api answers a booking that
     * already moved on with it, and the shared wording for that kind talks
     * about a form. There is no form here.
     */
    function cancelFailure(error: unknown): string {
        if (!(error instanceof ApiError)) {
            return "something went wrong, your booking was not changed";
        }

        if (error.kind === "InvalidRequest") {
            return "this booking has already moved on, so there was nothing to cancel";
        }

        return error.message;
    }

    async function cancel(): Promise<void> {
        if (cancelling) {
            return;
        }

        cancelling = true;
        failure = "";

        try {
            await requestedCancel(booking.id);

            confirming = false;

            onCancelled?.();
        } catch (error) {
            failure = cancelFailure(error);

            // The question is closed either way. A refusal that stayed on "are
            // you sure" invites the parent to press again at the same answer.
            confirming = false;
        } finally {
            cancelling = false;
        }
    }
</script>

<div class="actions" data-testid="booking-actions">
    {#if showOpen}
        <!--
            A button rather than a link. Every other control on this card moves
            the booking, and one of them looking like body text is how a parent
            reads it as a footnote instead of the way onward.
        -->
        <button
            type="button"
            class="primary"
            onclick={() => goto(`/booking/${booking.id}`)}
            data-testid="booking-open"
        >
            Open this booking
        </button>
    {/if}

    {#if showPay && withdrawable}
        <button
            type="button"
            class="primary"
            onclick={() => goto(`/pay/${booking.id}`)}
            data-testid="booking-pay"
        >
            Go to the payment screen
        </button>
    {/if}

    {#if withdrawable}
        {#if confirming}
            <!--
                Asked in the card rather than in a browser dialog. A cancel
                gives up a seat somebody else can take a second later, so it is
                worth one deliberate press, and the wording has to be this
                client's own.
            -->
            <p class="question" data-testid="booking-cancel-question">
                Cancel this booking? The seat goes back to the class.
            </p>

            <div class="answer">
                <button
                    type="button"
                    class="danger"
                    disabled={cancelling}
                    onclick={cancel}
                    data-testid="booking-cancel-confirm"
                >
                    {cancelling ? "Cancelling" : "Yes, cancel it"}
                </button>

                <button
                    type="button"
                    class="quiet"
                    disabled={cancelling}
                    onclick={() => (confirming = false)}
                    data-testid="booking-cancel-keep"
                >
                    Keep it
                </button>
            </div>
        {:else}
            <button
                type="button"
                class="quiet"
                onclick={() => (confirming = true)}
                data-testid="booking-cancel"
            >
                Cancel this booking
            </button>
        {/if}
    {/if}

    {#if failure !== ""}
        <p class="failure" role="alert" data-testid="booking-cancel-failure">{failure}</p>
    {/if}
</div>

<style>
    .actions {
        display: flex;
        flex-direction: column;
        gap: 0.35rem;
    }

    button {
        padding: 0.5rem;
        font-size: 0.95rem;
        font-weight: 600;
        font-family: inherit;
        border-radius: 0.25rem;
        cursor: pointer;
    }

    button:disabled {
        cursor: progress;
    }

    .primary {
        color: var(--on-accent);
        background: var(--accent);
        border: none;
    }

    /*
        Quieter than the control above it on purpose. Opening a booking is what
        a parent came for, and giving one up is not something a card should be
        pushing them towards.
    */
    .quiet {
        color: var(--ink-soft);
        background: var(--surface);
        border: 1px solid var(--line-strong);
    }

    .danger {
        color: var(--on-accent);
        background: var(--danger);
        border: none;
    }

    .question {
        margin: 0.25rem 0 0;
        font-size: 0.9rem;
        color: var(--ink-soft);
    }

    .answer {
        display: flex;
        gap: 0.35rem;
    }

    .answer button {
        flex: 1;
    }

    .failure {
        margin: 0;
        padding: 0.5rem 0.75rem;
        font-size: 0.9rem;
        color: var(--danger);
        background: var(--danger-surface);
        border-radius: 0.25rem;
    }
</style>

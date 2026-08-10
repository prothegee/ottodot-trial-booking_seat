<script lang="ts">
    import type { Booking, BookingStatus } from "$lib/api/types";

    interface Props {
        booking: Booking;
    }

    let { booking }: Props = $props();

    /**
     * What each status means, in this client's own words.
     *
     * Every status the backend enum can hold is here. A screen that fell
     * through to "unknown" for a state the api genuinely reports would be a
     * parent staring at their own booking with nothing to read.
     */
    const headlines: Readonly<Record<BookingStatus, string>> = {
        pending_payment: "Waiting for your payment",
        confirmed: "Your place is confirmed",
        payment_failed: "The payment did not go through",
        refund_required: "The seat went to someone else",
        expired: "The hold ran out",
        cancelled: "This booking was cancelled",
    };

    /**
     * What the parent should do next, if anything.
     *
     * Two of these say to do nothing, and they say it for opposite reasons.
     * `refund_required` took money and is giving it back, `expired` never took
     * any. Telling them apart is the difference between a parent who waits and
     * a parent who telephones.
     */
    const guidance: Readonly<Record<BookingStatus, string>> = {
        pending_payment: "Your seat is held until the countdown ends.",
        confirmed: "Nothing else to do. We will see your child in class.",
        payment_failed: "No money was taken. You can try the payment again.",
        refund_required:
            "Your payment is being refunded automatically. There is nothing for you to do.",
        expired: "No money was taken. You can book the class again if there is still room.",
        cancelled: "No money was taken.",
    };

    const status = $derived(booking.status);
    const settled = $derived(status === "confirmed");
</script>

<section class="status" data-testid="booking-status" data-status={status}>
    <h2 data-testid="booking-status-headline">{headlines[status]}</h2>

    <p class="guidance" data-testid="booking-status-guidance">{guidance[status]}</p>

    {#if settled && booking.seat_no !== null}
        <p class="seat" data-testid="booking-status-seat">Seat {booking.seat_no}</p>
    {/if}

    <!--
        The identifier is rendered small and last. It is what a parent quotes
        when they get in touch, and it is the only identifier on this screen:
        no child name, no email, nothing that would matter in a screenshot.
    -->
    <p class="reference" data-testid="booking-status-reference">Booking reference {booking.id}</p>
</section>

<style>
    .status {
        display: flex;
        flex-direction: column;
        gap: 0.5rem;
        padding: 1rem;
        background: #ffffff;
        border: 1px solid #e5e7eb;
        border-radius: 0.5rem;
    }

    h2 {
        margin: 0;
        font-size: 1.25rem;
    }

    .guidance {
        margin: 0;
        color: #374151;
    }

    .seat {
        margin: 0;
        font-size: 1.5rem;
        font-weight: 700;
        color: #047857;
    }

    .reference {
        margin: 0.5rem 0 0;
        font-size: 0.75rem;
        color: #6b7280;
        word-break: break-all;
    }
</style>

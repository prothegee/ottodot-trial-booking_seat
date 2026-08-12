<script lang="ts">
    import type { Snippet } from "svelte";

    import type { Booking, BookingStatus } from "$lib/api/types";

    interface Props {
        booking: Booking;

        /**
         * The controls for this booking, rendered inside the card.
         *
         * They were a line of links under it, which reads as a footnote about
         * the card rather than as something to do with the booking. What the
         * controls are is not this component's business, only where they go.
         */
        children?: Snippet;
    }

    let { booking, children }: Props = $props();

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

    /**
     * Whether this booking can say what it is for.
     *
     * The title is what decides. The api leaves all three class fields empty
     * when it could not read the class, and that read decides nothing, so a
     * booking still arrives. Rendering the block anyway would put an empty
     * heading above the status.
     */
    const named = $derived(booking.class_title !== "");

    /**
     * When the class starts, in whatever the reader's browser is set to.
     *
     * The same formatting the class list uses, so a parent recognises the
     * booking as the class they picked. Nothing here parses a date to decide
     * anything: every decision in this system is made on the backend.
     */
    const startsAt = $derived(
        booking.class_starts_at === null
            ? ""
            : new Date(booking.class_starts_at).toLocaleString(undefined, {
                  weekday: "short",
                  day: "numeric",
                  month: "short",
                  hour: "2-digit",
                  minute: "2-digit",
              }),
    );
</script>

<section class="status" data-testid="booking-status" data-status={status}>
    <!--
        What was booked, before what happened to it. A card that opened with
        "Your place is confirmed" and never said which class left a parent with
        a seat number and no way to tell one of their bookings from another.
    -->
    {#if named}
        <p class="subject" data-testid="booking-status-subject">{booking.class_subject}</p>

        <h2 data-testid="booking-status-class">{booking.class_title}</h2>

        {#if startsAt !== ""}
            <p class="when" data-testid="booking-status-when">{startsAt}</p>
        {/if}
    {/if}

    <!--
        The class is the card's heading when there is one, so the status below
        reads as what happened to it. When the class could not be read the
        status is the heading instead, rather than leaving the card with none.
    -->
    <svelte:element
        this={named ? "p" : "h2"}
        class="headline"
        data-testid="booking-status-headline"
    >
        {headlines[status]}
    </svelte:element>

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

    {#if children}
        {@render children()}
    {/if}
</section>

<style>
    .status {
        display: flex;
        flex-direction: column;
        gap: 0.5rem;
        padding: 1rem;
        background: var(--surface);
        border: 1px solid var(--line);
        border-radius: 0.5rem;
    }

    /*
        The same three lines the class list opens a card with, so a parent
        recognises the booking as the class they picked rather than having to
        match an identifier.
    */
    .subject {
        margin: 0;
        font-size: 0.75rem;
        font-weight: 600;
        text-transform: uppercase;
        letter-spacing: 0.05em;
        color: var(--muted);
    }

    h2 {
        margin: 0;
        font-size: 1.25rem;
    }

    .when {
        margin: 0 0 0.25rem;
        font-size: 0.9rem;
        color: var(--ink-soft);
    }

    .headline {
        margin: 0;
        font-size: 1.05rem;
        font-weight: 600;
    }

    .guidance {
        margin: 0;
        color: var(--ink-soft);
    }

    .seat {
        margin: 0;
        font-size: 1.5rem;
        font-weight: 700;
        color: var(--good);
    }

    .reference {
        margin: 0.5rem 0 0;
        font-size: 0.75rem;
        color: var(--muted);
        word-break: break-all;
    }
</style>

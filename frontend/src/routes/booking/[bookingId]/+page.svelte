<script lang="ts">
    import { page } from "$app/state";

    import BookingActions from "$lib/components/BookingActions.svelte";
    import BookingStatus from "$lib/components/BookingStatus.svelte";
    import { booking } from "$lib/stores/booking";

    const bookingId = $derived(page.params.bookingId ?? "");

    // Always read from the api. This screen is where a parent comes to find out
    // what happened, so it is the last place that should be showing something
    // remembered.
    $effect(() => {
        void booking.load(bookingId);
    });

    const held = $derived($booking.booking);
    const failure = $derived($booking.failure);
</script>

<section class="booking">
    <h1>Your booking</h1>

    {#if held !== null}
        <BookingStatus booking={held}>
            <!--
                The booking is read again rather than taken from what the cancel
                answered. This screen exists to say where a booking stands, and
                the api is what decides that.
            -->
            <BookingActions booking={held} showPay onCancelled={() => booking.load(bookingId)} />
        </BookingStatus>
    {:else if failure !== null}
        <p class="failure" role="alert" data-testid="booking-failure">{failure.message}</p>
    {:else}
        <p class="loading" data-testid="booking-loading">Loading your booking</p>
    {/if}

    <p class="back"><a href="/">Back to the class list</a></p>
</section>

<style>
    .booking {
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
        color: var(--muted);
    }

    .failure {
        margin: 0;
        padding: 0.5rem 0.75rem;
        font-size: 0.9rem;
        color: var(--danger);
        background: var(--danger-surface);
        border-radius: 0.25rem;
    }

    .back {
        margin-top: 1rem;
        font-size: 0.9rem;
    }
</style>

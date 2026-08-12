<script lang="ts">
    import BookingActions from "$lib/components/BookingActions.svelte";
    import BookingStatus from "$lib/components/BookingStatus.svelte";
    import { bookings } from "$lib/stores/bookings";

    // Always read from the api, and read again every time the screen opens.
    // This is where a parent comes to find out whether the money landed, so a
    // remembered answer is the one thing it must not show.
    $effect(() => {
        void bookings.load();
    });

    const listed = $derived($bookings.bookings);

    /**
     * True only once a read has come back with nothing.
     *
     * Without the `loaded` check the empty state would flash on the way in,
     * telling a parent they have booked nothing while the answer is still in
     * flight.
     */
    const nothingBooked = $derived($bookings.loaded && listed.length === 0);
</script>

<section class="bookings">
    <h1>Your bookings</h1>

    {#if $bookings.failure !== ""}
        <p class="failure" role="alert" data-testid="bookings-failure">{$bookings.failure}</p>
    {/if}

    {#if listed.length > 0}
        <ul class="list" data-testid="bookings-list">
            {#each listed as one (one.id)}
                <li data-testid="bookings-row">
                    <!--
                        The same card the booking screen shows, so the wording
                        for a status is written once. A list with its own
                        shorter vocabulary is a list that eventually disagrees
                        with the screen it links to.
                    -->
                    <BookingStatus booking={one}>
                        <!--
                            The list is read again rather than patched in place.
                            A cancel frees a seat, and the api is what decides
                            what every row says afterwards.
                        -->
                        <BookingActions booking={one} showOpen onCancelled={() => bookings.load()} />
                    </BookingStatus>
                </li>
            {/each}
        </ul>
    {:else if nothingBooked}
        <p class="note" data-testid="bookings-empty">
            You have not booked a trial class yet.
        </p>
    {:else if $bookings.loading}
        <p class="note" data-testid="bookings-loading">Loading your bookings</p>
    {/if}

    <p class="back"><a href="/">Back to the class list</a></p>
</section>

<style>
    .bookings {
        display: flex;
        flex-direction: column;
        gap: 0.75rem;
        max-width: 32rem;
    }

    h1 {
        margin: 0;
        font-size: 1.5rem;
    }

    .list {
        display: flex;
        flex-direction: column;
        gap: 1rem;
        margin: 0;
        padding: 0;
        list-style: none;
    }

    .note {
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

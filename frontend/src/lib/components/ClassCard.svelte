<script lang="ts">
    import type { TrialClass } from "$lib/api/types";

    interface Props {
        trialClass: TrialClass;
    }

    let { trialClass }: Props = $props();

    /**
     * Whether this class has room, as far as anyone here can tell.
     *
     * A count at or below zero is full. The two cases the plan names, no seats
     * left and every seat confirmed, are the same number by the time the api
     * has done the subtraction, and a negative would only arrive if capacity
     * were lowered under confirmed bookings. That reads as full too, which is
     * the honest answer rather than a nonsense number on screen.
     */
    const full = $derived(trialClass.seats_remaining <= 0);

    /**
     * The start time, in whatever the reader's browser is set to.
     *
     * The api sends RFC 3339 and this is the only thing done with it. Nothing
     * here parses a date to decide anything, because every decision in this
     * system is made on the backend.
     */
    const startsAt = $derived(
        new Date(trialClass.starts_at).toLocaleString(undefined, {
            weekday: "short",
            day: "numeric",
            month: "short",
            hour: "2-digit",
            minute: "2-digit",
        }),
    );
</script>

<article class="class-card" data-testid="class-card" data-class-id={trialClass.id}>
    <p class="subject">{trialClass.subject}</p>

    <h2>{trialClass.title}</h2>

    <p class="when">{startsAt}, {trialClass.duration_minutes} minutes</p>

    {#if full}
        <p class="seats full" data-testid="class-card-seats">Full</p>
    {:else}
        <p class="seats" data-testid="class-card-seats">
            {trialClass.seats_remaining} of {trialClass.capacity} seats left
        </p>
    {/if}

    {#if full}
        <!--
            No link at all rather than a disabled one. A count on screen is a
            hint, so a parent who is sure is not stopped from trying by anything
            here. What stops them is the api, and this is only what saves the
            wasted click.
        -->
        <p class="closed" data-testid="class-card-closed">no seats showing right now</p>
    {:else}
        <a class="book" href="/book/{trialClass.id}" data-testid="class-card-book">Book a place</a>
    {/if}
</article>

<style>
    .class-card {
        display: flex;
        flex-direction: column;
        gap: 0.35rem;
        padding: 1rem;
        background: #ffffff;
        border: 1px solid #e5e7eb;
        border-radius: 0.5rem;
    }

    .subject {
        margin: 0;
        font-size: 0.75rem;
        font-weight: 600;
        text-transform: uppercase;
        letter-spacing: 0.05em;
        color: #6b7280;
    }

    h2 {
        margin: 0;
        font-size: 1.1rem;
    }

    .when {
        margin: 0;
        font-size: 0.9rem;
        color: #374151;
    }

    .seats {
        margin: 0;
        font-size: 0.9rem;
        font-weight: 600;
        color: #047857;
    }

    .seats.full {
        color: #b91c1c;
    }

    .closed {
        margin: 0.5rem 0 0;
        font-size: 0.85rem;
        color: #6b7280;
    }

    .book {
        margin-top: 0.5rem;
        padding: 0.5rem;
        font-size: 0.95rem;
        font-weight: 600;
        text-align: center;
        text-decoration: none;
        color: #ffffff;
        background: #1d4ed8;
        border-radius: 0.25rem;
    }
</style>

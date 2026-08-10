<script lang="ts">
    import { goto } from "$app/navigation";
    import { page } from "$app/state";

    import ChildPicker from "$lib/components/ChildPicker.svelte";
    import { auth } from "$lib/stores/auth";
    import { booking } from "$lib/stores/booking";
    import { classes } from "$lib/stores/classes";

    const classId = $derived(page.params.classId ?? "");

    let selected = $state("");

    const childrenOnAccount = $derived($auth.session?.children ?? []);
    const chosenClass = $derived($classes.classes.find((held) => held.id === classId) ?? null);

    // The class list is needed for the title, and it is very often already in
    // the cache from the screen the parent came from, so this usually costs no
    // request at all.
    $effect(() => {
        void classes.load();
    });

    /**
     * What went wrong, in this client's own words.
     *
     * A duplicate is the one failure with somewhere to go, so it is separated
     * out. Everything else is a sentence and a form the parent can try again
     * with.
     */
    const failure = $derived($booking.failure);
    const duplicateOf = $derived(failure?.kind === "AlreadyBooked" ? failure.bookingId : "");

    async function requestHold(event: SubmitEvent): Promise<void> {
        event.preventDefault();

        if ($booking.submitting || selected === "") {
            return;
        }

        const held = await booking.create({ student_id: selected, class_id: classId });

        if (held === null) {
            // The reason is in the store and the screen is already showing it.
            // Nothing is thrown away: a parent whose class filled can pick
            // another one, and a duplicate has a link to follow.
            return;
        }

        await goto(`/pay/${held.id}`);
    }
</script>

<section class="book">
    <h1>{chosenClass === null ? "Book a place" : chosenClass.title}</h1>

    {#if chosenClass !== null}
        <p class="lead">
            {chosenClass.seats_remaining} of {chosenClass.capacity} seats showing. That count is a
            hint, and the seat itself is decided when your payment settles.
        </p>
    {/if}

    <form onsubmit={requestHold}>
        <ChildPicker {childrenOnAccount} bind:selected disabled={$booking.submitting} />

        <button
            type="submit"
            disabled={$booking.submitting || selected === ""}
            data-testid="book-submit"
        >
            {$booking.submitting ? "Holding a place" : "Hold a place"}
        </button>
    </form>

    {#if failure !== null}
        <p class="failure" role="alert" data-testid="book-failure">{failure.message}</p>
    {/if}

    {#if duplicateOf !== ""}
        <p class="duplicate">
            <a href="/booking/{duplicateOf}" data-testid="book-existing-link">
                Open the booking this child already has
            </a>
        </p>
    {/if}

    <p class="back"><a href="/">Back to the class list</a></p>
</section>

<style>
    .book {
        max-width: 32rem;
    }

    h1 {
        margin-top: 0;
        font-size: 1.5rem;
    }

    .lead {
        color: #6b7280;
        font-size: 0.9rem;
    }

    form {
        display: flex;
        flex-direction: column;
        gap: 0.75rem;
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

    .failure {
        padding: 0.5rem 0.75rem;
        font-size: 0.9rem;
        color: #b91c1c;
        background: #fee2e2;
        border-radius: 0.25rem;
    }

    .duplicate {
        font-size: 0.9rem;
    }

    .back {
        margin-top: 1.5rem;
        font-size: 0.9rem;
    }
</style>

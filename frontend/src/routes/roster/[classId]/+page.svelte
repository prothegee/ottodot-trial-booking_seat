<script lang="ts">
    import { onDestroy } from "svelte";

    import { page } from "$app/state";

    import { auth } from "$lib/stores/auth";
    import { roster } from "$lib/stores/roster";

    const classId = $derived(page.params.classId ?? "");

    $effect(() => {
        void roster.load(classId);
    });

    onDestroy(() => {
        // A roster is the only thing in this client that carries another
        // family's name. It is dropped the moment the screen goes, so it does
        // not sit in memory behind whatever the teacher looks at next.
        roster.reset();
    });

    const listed = $derived($roster.roster);
    const entries = $derived(listed?.entries ?? []);

    /**
     * How many seats are taken, counted rather than read.
     *
     * The api sends the entries and the capacity, and the count is the length of
     * the list. A separate number on the wire would be a second thing that could
     * disagree with the rows underneath it.
     */
    const seatsTaken = $derived(entries.length);

    const isAdmin = $derived($auth.session?.role === "admin");
</script>

<section class="roster">
    <h1>Class roster</h1>

    {#if $roster.forbidden}
        <!--
            A parent who typed this route in. The api refused it, which is the
            rule, and this is only the wording. Nothing here says what the route
            would have shown.
        -->
        <p class="failure" role="alert" data-testid="roster-forbidden">
            This page is for teachers and is not available on your account.
        </p>
    {:else if $roster.failure !== ""}
        <p class="failure" role="alert" data-testid="roster-failure">{$roster.failure}</p>
    {:else if $roster.loading}
        <p class="note" data-testid="roster-loading">Loading the roster</p>
    {:else if listed !== null}
        <p class="summary" data-testid="roster-summary">
            {seatsTaken} of {listed.capacity} seats confirmed
        </p>

        {#if entries.length > 0}
            <!--
                Confirmed bookings only. A hold is somebody who has not paid and
                a refund_required booking is somebody whose seat went to
                somebody else, and neither of them is coming to the class. The
                api is what leaves them out, and this screen has no way to ask
                for them.
            -->
            <!--
                The wrapper is what keeps a phone usable. The confirmed column
                holds a full local date and time, so on a narrow screen this
                table is wider than the viewport, and without somewhere to
                scroll it the whole page slides sideways instead.
            -->
            <div class="table-scroll">
                <table data-testid="roster-table">
                    <thead>
                        <tr>
                            <th scope="col">Seat</th>
                            <th scope="col">Student</th>
                            <th scope="col">Confirmed</th>
                        </tr>
                    </thead>
                    <tbody>
                        {#each entries as entry (entry.student_id)}
                            <tr data-testid="roster-row">
                                <td data-testid="roster-seat">{entry.seat_no}</td>
                                <td>{entry.student_name}</td>
                                <td>{new Date(entry.confirmed_at).toLocaleString()}</td>
                            </tr>
                        {/each}
                    </tbody>
                </table>
            </div>
        {:else}
            <p class="note" data-testid="roster-empty">Nobody has confirmed a seat in this class yet.</p>
        {/if}
    {/if}

    {#if isAdmin}
        <p class="back"><a href="/">Back to the class list</a></p>
    {/if}
</section>

<style>
    .roster {
        display: flex;
        flex-direction: column;
        gap: 0.75rem;
        max-width: 34rem;
    }

    h1 {
        margin: 0;
        font-size: 1.5rem;
    }

    .summary {
        margin: 0;
        font-weight: 600;
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

    .table-scroll {
        overflow-x: auto;
    }

    table {
        border-collapse: collapse;
        font-size: 0.9rem;
        white-space: nowrap;
    }

    th,
    td {
        padding: 0.35rem 1rem 0.35rem 0;
        text-align: left;
        border-bottom: 1px solid var(--line);
    }

    .back {
        margin-top: 1rem;
        font-size: 0.9rem;
    }
</style>

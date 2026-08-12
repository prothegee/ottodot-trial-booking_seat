<script lang="ts">
    import ClassCard from "$lib/components/ClassCard.svelte";
    import { auth } from "$lib/stores/auth";
    import { classes } from "$lib/stores/classes";

    // The read goes through the cache, so coming back to this screen a moment
    // later costs no request at all, and coming back after a booking costs one
    // conditional request. Neither is decided here: the store and the cache
    // policy own that, and this screen only asks.
    $effect(() => {
        void classes.load();
    });

    const nothingToShow = $derived(!$classes.loading && $classes.failure === "" && $classes.classes.length === 0);

    // The roster link is shown to an operator and hidden from a parent.
    //
    // Hiding it is a courtesy and not the rule. Anybody can type the route, and
    // what actually refuses them is the api answering forbidden_role. A client
    // that treated a hidden link as protection would be one developer tools
    // window away from handing over every other family's name.
    const isAdmin = $derived($auth.session?.role === "admin");
</script>

<section class="class-list">
    <h1>Trial classes</h1>

    <p class="lead">
        Pick a class, then pick a child. A seat is held while you pay, and the seat itself is
        decided when the payment settles.
    </p>

    {#if $classes.failure !== ""}
        <!--
            The list already on screen is left where it is. It is no more wrong
            than it was a second ago, and blanking the page would take away the
            one thing the parent could still act on.
        -->
        <p class="failure" role="alert" data-testid="class-list-failure">{$classes.failure}</p>
    {/if}

    {#if $classes.loading && $classes.classes.length === 0}
        <p class="waiting" data-testid="class-list-loading">Loading classes</p>
    {/if}

    {#if nothingToShow}
        <p class="waiting" data-testid="class-list-empty">There are no trial classes scheduled.</p>
    {/if}

    <div class="cards">
        {#each $classes.classes as trialClass (trialClass.id)}
            <ClassCard {trialClass} showRoster={isAdmin} />
        {/each}
    </div>
</section>

<style>
    .class-list {
        max-width: 60rem;
    }

    h1 {
        margin-top: 0;
        font-size: 1.5rem;
    }

    .lead {
        max-width: 40rem;
        color: var(--muted);
        font-size: 0.9rem;
    }

    .cards {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(16rem, 1fr));
        gap: 1rem;
        margin-top: 1rem;
    }

    .waiting {
        font-size: 0.9rem;
        color: var(--muted);
    }

    .failure {
        padding: 0.5rem 0.75rem;
        font-size: 0.9rem;
        color: var(--danger);
        background: var(--danger-surface);
        border-radius: 0.25rem;
    }
</style>

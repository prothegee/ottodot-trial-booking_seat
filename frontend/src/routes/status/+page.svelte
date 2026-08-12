<script lang="ts">
    import { onDestroy, onMount } from "svelte";

    import ReadinessDot from "$lib/components/ReadinessDot.svelte";
    import { buildIdentity } from "$lib/config/environment";
    import { status } from "$lib/stores/status";

    // The only screen in this client that polls. It starts when the screen
    // mounts and stops when it is destroyed, so a tab left on the class list is
    // never asking a readiness probe anything.
    onMount(() => {
        void status.open();
    });

    onDestroy(() => {
        status.close();
        status.reset();
    });

    const identity = $derived($status.version);
    const readiness = $derived($status.readiness);

    /**
     * The dependency rows, sorted by name.
     *
     * Sorted rather than left in object order, because object order is whatever
     * the json happened to carry and a row that moves between polls is a row
     * nobody can read.
     */
    const checks = $derived(Object.entries(readiness?.checks ?? {}).sort(([first], [second]) => first.localeCompare(second)));
</script>

<section class="status">
    <h1>Status</h1>

    <div class="readiness-line">
        <ReadinessDot status={readiness?.status ?? null} />

        {#if $status.loading}
            <span class="note" data-testid="status-loading">asking the backend</span>
        {/if}
    </div>

    {#if $status.failure !== ""}
        <p class="failure" role="alert" data-testid="status-failure">{$status.failure}</p>
    {/if}

    <h2>Dependencies</h2>

    {#if checks.length > 0}
        <table data-testid="status-dependencies">
            <thead>
                <tr>
                    <th scope="col">Dependency</th>
                    <th scope="col">Answer</th>
                </tr>
            </thead>
            <tbody>
                {#each checks as [name, answer] (name)}
                    <tr data-testid="status-dependency-{name}">
                        <th scope="row">{name}</th>
                        <td>{answer}</td>
                    </tr>
                {/each}
            </tbody>
        </table>
    {:else}
        <p class="note" data-testid="status-no-dependencies">The backend has not said which dependencies it checks.</p>
    {/if}

    <h2>Build</h2>

    <!--
        Two builds, side by side. The client's own identity comes from the
        environment it was built with, and the backend's comes from its /version
        route, and the interesting case is when they disagree: a deployment that
        moved one and not the other.
    -->
    <dl class="build">
        <dt>Client version</dt>
        <dd data-testid="client-version">{buildIdentity.version}</dd>

        <dt>Client commit</dt>
        <dd data-testid="client-commit">{buildIdentity.commit}</dd>

        <dt>Backend service</dt>
        <dd data-testid="backend-service">{identity?.service ?? "unknown"}</dd>

        <dt>Backend version</dt>
        <dd data-testid="backend-version">{identity?.version ?? "unknown"}</dd>

        <dt>Backend commit</dt>
        <dd data-testid="backend-commit">{identity?.commit ?? "unknown"}</dd>

        <dt>Backend built at</dt>
        <dd data-testid="backend-built-at">{identity?.built_at === "" || identity === null ? "unknown" : identity.built_at}</dd>

        <dt>Backend runtime</dt>
        <dd data-testid="backend-runtime">{identity?.runtime ?? "unknown"}</dd>
    </dl>

    <p class="back"><a href="/">Back to the class list</a></p>
</section>

<style>
    .status {
        display: flex;
        flex-direction: column;
        gap: 0.75rem;
        max-width: 34rem;
    }

    h1 {
        margin: 0;
        font-size: 1.5rem;
    }

    h2 {
        margin: 0.5rem 0 0;
        font-size: 1rem;
    }

    .readiness-line {
        display: flex;
        align-items: center;
        gap: 0.75rem;
    }

    .note {
        margin: 0;
        font-size: 0.9rem;
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

    table {
        border-collapse: collapse;
        font-size: 0.9rem;
    }

    th,
    td {
        padding: 0.35rem 0.75rem 0.35rem 0;
        text-align: left;
        border-bottom: 1px solid var(--line);
    }

    .build {
        display: grid;
        grid-template-columns: auto 1fr;
        gap: 0.25rem 1rem;
        margin: 0;
        font-size: 0.9rem;
    }

    .build dt {
        color: var(--muted);
    }

    .build dd {
        margin: 0;
        font-family: ui-monospace, monospace;
    }

    .back {
        margin-top: 1rem;
        font-size: 0.9rem;
    }
</style>

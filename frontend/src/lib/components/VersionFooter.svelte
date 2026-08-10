<script lang="ts">
    import { buildIdentity } from "$lib/config/environment";

    /** Enough of a commit to identify a build, short enough to read on video. */
    const shortCommitLength = 7;

    const commit =
        buildIdentity.commit.length > shortCommitLength
            ? buildIdentity.commit.slice(0, shortCommitLength)
            : buildIdentity.commit;
</script>

<footer class="version-footer" data-testid="version-footer">
    <span data-testid="version-footer-version">version {buildIdentity.version}</span>
    <span aria-hidden="true">&middot;</span>
    <span data-testid="version-footer-commit">commit {commit}</span>
</footer>

<style>
    .version-footer {
        display: flex;
        gap: 0.5rem;
        justify-content: center;
        padding: 1rem;
        font-size: 0.8rem;
        color: #6b7280;
        border-top: 1px solid #e5e7eb;
    }
</style>

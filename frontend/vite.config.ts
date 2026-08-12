import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig, loadEnv } from "vite";

import { commitFromGit, versionFromManifest } from "./src/lib/config/build_identity";

/**
 * Three values reach the bundle from outside it, and none of them is ever
 * written in a component.
 *
 * API_BASE_URL: where the client sends every call. Hardcoding it in a component
 * would mean a rebuild is the only way to point at a different backend, and it
 * would mean finding every copy.
 *
 * BUILD_VERSION and BUILD_COMMIT: what the footer and the /status route show.
 * A reviewer has to be able to tell which build is on screen. Neither has to be
 * set: an unnamed build takes the version package.json already states and the
 * commit git already recorded, so a plain "npm run dev" names itself.
 *
 * They are injected as compile time constants rather than read at runtime, so
 * nothing has to be fetched before the first paint. That is also why .env is a
 * build time file here: changing one needs a rebuild, not a restart.
 */

/**
 * Reads .env, then falls back to the environment.
 *
 * What the file states wins, which is the same rule the backend applies to
 * config.json. One rule for both stacks is worth more than the small surprise of
 * a shell variable being ignored, and .env.template says which values are
 * deliberately left out so the build script can hand them in.
 *
 * The empty prefix is what makes loadEnv return names that do not begin with
 * VITE_. Nothing here is exposed to the page except through the three constants
 * below, so the prefix would only be a rule to remember.
 *
 * Param:
 * mode - string (which .env files vite should consider)
 *
 * Return:
 * - a reader that answers with the file's value, then the environment's, then
 *   the fallback
 */
function settingReader(mode: string): (key: string, fallback: string) => string {
    const stated = loadEnv(mode, process.cwd(), "");

    return (key, fallback) => {
        const value = stated[key] ?? process.env[key] ?? "";

        return value.trim() === "" ? fallback : value.trim();
    };
}

export default defineConfig(({ mode }) => {
    const setting = settingReader(mode);

    const projectRoot = process.cwd();

    const apiBaseUrl = setting("API_BASE_URL", "http://127.0.0.1:9000");
    const buildVersion = setting("BUILD_VERSION", versionFromManifest(projectRoot));
    const buildCommit = setting("BUILD_COMMIT", commitFromGit(projectRoot));

    const port = Number(setting("FRONTEND_PORT", "9001"));

    // Bound to the loopback address explicitly.
    //
    // The default resolves "localhost", which can land on the IPv6 address
    // alone. A reviewer following how-to.md would then get a refused connection
    // on 127.0.0.1 with a server that says it is running. Set FRONTEND_HOST to
    // 0.0.0.0 to reach it from another machine or from a container.
    const host = setting("FRONTEND_HOST", "127.0.0.1");

    return {
        plugins: [sveltekit()],
        // Under vitest the browser build of Svelte is the one that mounts into
        // jsdom. Without this, the server build is resolved and no component
        // ever renders.
        resolve: process.env.VITEST ? { conditions: ["browser"] } : undefined,
        define: {
            __API_BASE_URL__: JSON.stringify(apiBaseUrl),
            __BUILD_VERSION__: JSON.stringify(buildVersion),
            __BUILD_COMMIT__: JSON.stringify(buildCommit),
        },
        server: {
            host,
            port,
            strictPort: true,
        },
        preview: {
            host,
            port,
            strictPort: true,
        },
        test: {
            environment: "jsdom",
            globals: true,
            include: ["src/**/*.test.ts"],
            setupFiles: ["src/tests/setup.ts"],
        },
    };
});

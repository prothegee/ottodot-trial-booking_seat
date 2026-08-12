import adapter from "@sveltejs/adapter-static";
import { vitePreprocess } from "@sveltejs/vite-plugin-svelte";

/**
 * The build is a static bundle served by nginx, with one fallback document.
 *
 * There is no server rendering here, which is a decision recorded in ADR.md:
 * every meaningful decision in this system happens inside a transaction on the
 * backend, so a rendering server in front of it would add a moving part without
 * adding an answer.
 *
 * The fallback exists because routes like /book/[classId] are only known at
 * runtime. nginx hands the same document to every unknown path and the client
 * router takes it from there.
 *
 * @type {import('@sveltejs/kit').Config}
 */
const config = {
    preprocess: vitePreprocess(),
    kit: {
        adapter: adapter({
            pages: "build",
            assets: "build",
            fallback: "index.html",
            strict: false,
        }),
    },
};

export default config;

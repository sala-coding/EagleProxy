// vite.harness.config.ts — the dev server for the browser test harness.
//
// A separate config rather than a change to vite.config.ts, so the production
// build is left exactly as it is. The only real difference is the watcher: the
// harness keeps a headless Chrome profile inside the tree, and Chrome holds its
// files open, which makes Vite's file watcher fail with EBUSY on a cookie store
// it has no business watching.
//
// The root is the front end, because that is where the application under test
// lives and where its dependencies are installed. The harness's own files live
// outside it (they are under tests/), and they never go through this server:
// the fake backend is injected into the page by the driver (see cdp.mjs), so
// the page the harness drives is the application's own page.
//
// The two imports below reach into the front end's node_modules by path,
// because this file sits outside that directory and Node would not find them
// there: the front end's package.json is what pins them.
//
//   node_modules\.bin\vite.cmd --config ../tests/browser/vite.harness.config.ts
import {defineConfig} from '../../frontend/node_modules/vite/dist/node/index.js'
import vue from '../../frontend/node_modules/@vitejs/plugin-vue/dist/index.mjs'
import {fileURLToPath} from 'node:url'
import {dirname, resolve} from 'node:path'

const here = dirname(fileURLToPath(import.meta.url))
const frontend = resolve(here, '..', '..', 'frontend')

export default defineConfig({
    root: frontend,
    plugins: [vue()],
    build: {target: 'esnext'},
    server: {
        host: '127.0.0.1',
        port: 5199,
        strictPort: true,
        watch: {
            // The headless Chrome profile is not source. Watching it is how the
            // dev server dies with EBUSY on a locked cookie store.
            ignored: ['**/tests/browser/.chrome-profile/**', '**/tests/browser/out/**'],
        },
    },
})

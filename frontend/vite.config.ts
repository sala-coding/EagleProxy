import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [vue()],
  // main.ts awaits the Chinese word table before the first paint (see tips.ts),
  // and top-level await needs a build target that has it. esnext is safe here:
  // the window is a WebView2, which is the browser this app already assumes.
  build: {target: 'esnext'},
  server: {
    // The IPv4 loopback, named rather than left at "localhost". Node 17 and later
    // hand back addresses in the order the system resolver returns them instead of
    // sorting IPv4 first, and on Windows that puts ::1 ahead — so the dev server
    // listened on [::1] alone while wails polled 127.0.0.1 and sat there waiting
    // for a server that was already up (see the "Waiting for frontend DevServer"
    // loop). Naming the address keeps the resolver out of the decision.
    host: '127.0.0.1',
    port: 5173,
    // A dev server that quietly moves to 5174 is one wails is not looking at, and
    // the wait it produces is indistinguishable from a hang. Failing loudly beats
    // appearing to start.
    strictPort: true,
  },
})

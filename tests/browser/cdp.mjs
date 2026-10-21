// cdp.mjs — a tiny Chrome DevTools Protocol driver.
//
// The session's browser tooling is not available, so this speaks CDP to a Chrome
// started with --remote-debugging-port. It is deliberately small: the app under
// test is a page, and everything this needs to do to a page is "evaluate some
// JavaScript in it" and "take a picture of it".
//
//   node cdp.mjs open <url>          install the error collector, navigate, settle
//   node cdp.mjs run <file.js>       evaluate a file's contents, print the result
//   node cdp.mjs shot <out.png>      screenshot the page
//   node cdp.mjs logs                print the collected console/page errors
//   node cdp.mjs close               close the page target
//
// The error collector is installed with Page.addScriptToEvaluateOnNewDocument so
// it is in place before the app's own module runs: a page error thrown during
// boot is exactly the one worth seeing.

import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
// Node 22+ ships a global WebSocket (undici). No dependency needed.
const {WebSocket} = globalThis;

// This file's own directory: the helpers and the probes live beside it, and
// they are found from here rather than from wherever the caller happened to
// run us — npm runs a script with the package root as its working directory.
const HERE = path.dirname(fileURLToPath(import.meta.url));

const PORT = Number(process.env.CDP_PORT || 9333);
const HOST = `127.0.0.1:${PORT}`;

const COLLECTOR = `
(() => {
  if (window.__err) return;
  // The app remembers a chosen language in localStorage (settings-meta.ts,
  // startLanguage / rememberLanguage) and wears it at the next start-up. Left
  // alone, that makes the harness non-deterministic twice over: a probe that
  // switches the language is carried into the next page, and a runner whose
  // system language is English starts the app in English — where the probes,
  // which look controls up by their Chinese labels, find nothing and report
  // an app that is fine as broken. The mark is therefore pinned to Chinese
  // before the app's own code runs. (0 is the file's number for it; see
  // LANGUAGES in settings-meta.ts.) The other keys (ui-scale, custom-palette)
  // are left alone: they are the state the probes are about.
  try { localStorage.setItem('lang-chosen', '0'); } catch (e) {}
  window.__err = [];
  const push = (kind, text, extra) => {
    try {
      window.__err.push({kind, text: String(text), extra: extra == null ? '' : String(extra), at: Date.now()});
    } catch (e) {}
  };
  window.addEventListener('error', ev => {
    push('error', ev.message || 'error', (ev.error && ev.error.stack) || (ev.filename || '') + ':' + (ev.lineno || ''));
  }, true);
  window.addEventListener('unhandledrejection', ev => {
    const r = ev.reason;
    push('unhandledrejection', (r && r.message) || r, (r && r.stack) || '');
  });
  const origError = console.error.bind(console);
  console.error = function (...a) {
    push('console.error', a.map(x => (x && x.stack) ? x.stack : (typeof x === 'object' ? JSON.stringify(x) : String(x))).join(' '), '');
    return origError(...a);
  };
  const origWarn = console.warn.bind(console);
  console.warn = function (...a) {
    push('console.warn', a.map(x => String(x)).join(' '), '');
    return origWarn(...a);
  };
})();
`;

async function http(path) {
    const r = await fetch(`http://${HOST}${path}`);
    if (!r.ok) throw new Error(`${path} -> HTTP ${r.status}`);
    return r.json();
}

class Session {
    constructor(ws) {
        this.ws = ws;
        this.id = 0;
        this.pending = new Map();
        ws.addEventListener('message', ev => {
            let msg;
            try { msg = JSON.parse(ev.data); } catch { return; }
            if (msg.id && this.pending.has(msg.id)) {
                const {resolve, reject} = this.pending.get(msg.id);
                this.pending.delete(msg.id);
                if (msg.error) reject(new Error(msg.error.message + ' ' + (msg.error.data || '')));
                else resolve(msg.result);
            }
        });
    }

    send(method, params = {}) {
        const id = ++this.id;
        this.ws.send(JSON.stringify({id, method, params}));
        return new Promise((resolve, reject) => {
            this.pending.set(id, {resolve, reject});
            setTimeout(() => {
                if (this.pending.has(id)) {
                    this.pending.delete(id);
                    reject(new Error(`CDP timeout: ${method}`));
                }
            }, 15000);
        });
    }

    async evaluate(expr) {
        const r = await this.send('Runtime.evaluate', {
            expression: expr,
            returnByValue: true,
            awaitPromise: true,
            userGesture: true,
        });
        if (r.exceptionDetails) {
            const e = r.exceptionDetails;
            throw new Error('page exception: ' + (e.exception?.description || e.text));
        }
        return r.result?.value;
    }
}

async function connect() {
    const targets = await http('/json/list');
    let page = targets.find(t => t.type === 'page');
    if (!page) {
        const t = await http(`/json/new?about:blank`);
        page = t;
    }
    const ws = new WebSocket(page.webSocketDebuggerUrl, {maxPayload: 256 * 1024 * 1024});
    await new Promise((res, rej) => {
        ws.addEventListener('open', res, {once: true});
        ws.addEventListener('error', rej, {once: true});
    });
    const s = new Session(ws);
    await s.send('Runtime.enable');
    await s.send('Page.enable');
    return s;
}

const sleep = ms => new Promise(r => setTimeout(r, ms));

// The browser to drive. Chrome first, then Edge — a Windows machine has one of
// the two, and a CI runner has both. CHROME_PATH overrides the search.
const BROWSER_CANDIDATES = [
    () => process.env.CHROME_PATH,
    () => 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe',
    () => 'C:\\Program Files (x86)\\Google\\Chrome\\Application\\chrome.exe',
    () => 'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe',
    () => 'C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe',
];

function findBrowser() {
    for (const pick of BROWSER_CANDIDATES) {
        const p = pick();
        if (p && fs.existsSync(p)) return p;
    }
    return '';
}

async function main() {
    const cmd = process.argv[2];

    if (cmd === 'launch') {
        // Spawn a headless browser of our own and wait for its CDP port to answer.
        // stdio is 'ignore' rather than the default pipe: a piped child cannot
        // be captured in this sandbox, and the browser's own output is of no use here.
        const {spawn} = await import('node:child_process');
        const port = Number(process.env.CDP_PORT || 9333);
        const browser = findBrowser();
        if (!browser) {
            console.error('no Chrome or Edge found — set CHROME_PATH to the browser to use');
            process.exitCode = 1;
            return;
        }
        // A profile of its own for every run, and the old ones cleared out: a
        // profile left behind holds a lock on its own files, and a browser that
        // cannot take the lock answers the CDP port with nothing — which reads
        // as a browser that will not start.
        const base = process.env.CDP_PROFILE_BASE
            || (process.env.TEMP + '\\eagle-cdp-' + port);
        const profile = base + '-' + process.pid;
        try {
            for (const name of fs.readdirSync(base)) {
                if (name === profile.slice(base.length + 1)) continue;
                fs.rmSync(base + '\\' + name, {recursive: true, force: true});
            }
        } catch { /* the directory is not there yet — nothing to clear */ }
        const child = spawn(browser, [
            '--headless=new',
            `--remote-debugging-port=${port}`,
            `--user-data-dir=${profile}`,
            '--no-first-run', '--no-default-browser-check', '--disable-gpu',
            '--window-size=1440,900', '--hide-scrollbars', 'about:blank',
        ], {stdio: 'ignore', detached: true});
        child.unref();
        for (let i = 0; i < 40; i++) {
            await sleep(500);
            try {
                await http('/json/version');
                console.log(`browser up on ${port} (pid ${child.pid}, profile ${profile})`);
                return;
            } catch { /* not up yet */ }
        }
        console.error('the browser did not come up on ' + port);
        process.exitCode = 1;
        return;
    }

    const s = await connect();

    if (cmd === 'open') {
        const url = process.argv[3];
        await s.send('Page.addScriptToEvaluateOnNewDocument', {source: COLLECTOR});
        // The fake backend, injected the same way as the collector: it has to
        // be in place before the application's own module runs, because the
        // first thing the app does is call into window.go. Injecting it (rather
        // than loading it from a page of its own) means the page under test is
        // the application's own page.
        const mock = fs.readFileSync(path.join(HERE, 'mock-bridge.js'), 'utf8');
        await s.send('Page.addScriptToEvaluateOnNewDocument', {source: mock});
        // What the current document is, before the navigation. Page.navigate
        // answers before the new document commits, so a poll that only asks
        // "is the app painted" is answered by the document that is about to be
        // replaced — and a probe started then loses its elements halfway
        // through, as an intermittent "element not found" that looks like an
        // app fault. performance.timeOrigin is per document: waiting for it to
        // change is waiting for the new one, not for either of them to look
        // ready.
        const before = await s.evaluate(`(() => {
            const app = document.getElementById('app');
            return {origin: performance.timeOrigin, painted: !!(app && app.children.length)};
        })()`);
        await s.send('Page.navigate', {url});
        // Wait for the app to have painted something, not merely for the load
        // event: the app fetches its word table and paints after that.
        const deadline = Date.now() + 30000;
        let ready = false;
        while (Date.now() < deadline) {
            await sleep(150);
            try {
                const v = await s.evaluate(`(() => {
                    const app = document.getElementById('app');
                    return {
                        origin: performance.timeOrigin,
                        state: document.readyState,
                        painted: !!(app && app.children.length && document.querySelector('.app')),
                    };
                })()`);
                const fresh = !before || v.origin !== before.origin;
                if (fresh && v.state === 'complete' && v.painted) { ready = true; break; }
            } catch (e) { /* page still swapping documents */ }
        }
        // The first frames after a paint are still the app settling: the mock
        // answers synchronously, but the layout that follows the settings read
        // lands on the next frame.
        if (ready) await sleep(250);
        console.log(JSON.stringify({ready, url}, null, 2));
    } else if (cmd === 'run') {
        const file = process.argv[3];
        const libPath = path.join(HERE, 'lib.js');
        const lib = fs.existsSync(libPath) ? fs.readFileSync(libPath, 'utf8') : '';
        const src = fs.readFileSync(file, 'utf8');
        const out = await s.evaluate(`(async () => { ${lib}\n${src} })()`);
        console.log(typeof out === 'string' ? out : JSON.stringify(out, null, 2));
    } else if (cmd === 'viewport') {
        // Emulate a device size: headless Chrome's window.resizeTo is a no-op, so
        // the narrow-window questions need the emulation domain instead.
        const w = Number(process.argv[3] || 1440);
        const h = Number(process.argv[4] || 900);
        await s.send('Emulation.setDeviceMetricsOverride', {
            width: w, height: h, deviceScaleFactor: 1, mobile: false,
        });
        console.log(`viewport ${w}x${h}`);
    } else if (cmd === 'shot') {
        const out = process.argv[3];
        const r = await s.send('Page.captureScreenshot', {format: 'png', captureBeyondViewport: false});
        fs.writeFileSync(out, Buffer.from(r.data, 'base64'));
        console.log('saved ' + out);
    } else if (cmd === 'mouse') {
        // A real (trusted) mouse event: mouse <type> <x> <y> [button] [buttons]
        const type = process.argv[3];
        const x = Number(process.argv[4]);
        const y = Number(process.argv[5]);
        const button = process.argv[6] || 'left';
        const buttons = Number(process.argv[7] ?? 0);
        await s.send('Input.dispatchMouseEvent', {
            type, x, y, button,
            buttons: type === 'mouseMoved' ? buttons : (type === 'mousePressed' ? 1 : 0),
            clickCount: type === 'mousePressed' || type === 'mouseReleased' ? 1 : 0,
            pointerType: 'mouse',
        });
        console.log('sent ' + type + ' ' + x + ' ' + y);
    } else if (cmd === 'logs') {
        const out = await s.evaluate('JSON.stringify(window.__err || [], null, 2)');
        console.log(out);
    } else if (cmd === 'close') {
        console.log('ok');
    } else {
        console.error('unknown command: ' + cmd);
        process.exitCode = 2;
    }
    s.ws.close();
    // The socket close is asynchronous; without this the process lingers on the
    // event loop and the caller waits for a timeout instead of a result.
    process.exit(process.exitCode || 0);
}

main().catch(e => {
    console.error('cdp error: ' + e.message);
    process.exit(1);
});

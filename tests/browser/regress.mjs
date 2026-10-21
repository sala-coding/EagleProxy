// regress.mjs — the front end's regression suite, in one command.
//
// It starts what it needs (the dev server and a headless browser), runs every
// probe against the real application with a mock backend, and reports what broke.
//
//   node regress.mjs                 everything
//   node regress.mjs probe-05 ...    only the probes named
//   node regress.mjs --keep          leave the server and the browser running
//
// A probe is a small piece of JavaScript evaluated in the page (see probe-*.mjs).
// It returns a JSON object; the runner looks for the keys "errs" and "crash" in
// it. A probe that reports an error or a crash fails the run.

import {execFileSync, spawn} from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const FRONTEND = path.resolve(HERE, '..', '..', 'frontend');
const PORT = Number(process.env.HARNESS_PORT || 5199);
// The application's own page. The fake backend is injected into it by the
// driver (see cdp.mjs), so the page under test is the one a reader would get.
const PAGE_URL = `http://127.0.0.1:${PORT}/index.html`;

const ALL = fs.readdirSync(HERE)
    .filter(f => /^probe-.*\.mjs$/.test(f))
    .map(f => f.replace(/\.mjs$/, ''))
    // The numeric ones are the first walk-through, kept in order; the named ones
    // are the second round's, each about one thing.
    .sort((a, b) => {
        const na = Number(a.match(/\d+/)?.[0] ?? 999);
        const nb = Number(b.match(/\d+/)?.[0] ?? 999);
        return na - nb || a.localeCompare(b);
    });

const args = process.argv.slice(2);
const keep = args.includes('--keep');
const named = args.filter(a => !a.startsWith('--'));
const probes = named.length ? named : ALL;

const sleep = ms => new Promise(r => setTimeout(r, ms));

function run(cmd, cmdArgs, opts = {}) {
    try {
        return execFileSync(cmd, cmdArgs, {encoding: 'utf8', timeout: 180000, ...opts});
    } catch (e) {
        return (opts.allowFail ? '' : '') + (e.stdout || '') + (e.stderr || '');
    }
}

async function up(url) {
    try {
        const r = await fetch(url, {signal: AbortSignal.timeout(3000)});
        return r.ok;
    } catch { return false; }
}

const started = {server: null, browser: false};

async function ensureServer() {
    if (await up(PAGE_URL)) {
        console.log(`dev server already up on ${PORT}`);
        return;
    }
    console.log(`starting the dev server on ${PORT}…`);
    const child = spawn(process.execPath, [
        path.join(FRONTEND, 'node_modules', 'vite', 'bin', 'vite.js'),
        '--config', path.join(HERE, 'vite.harness.config.ts'),
    ], {cwd: FRONTEND, stdio: 'ignore', detached: true});
    child.unref();
    started.server = child;
    for (let i = 0; i < 60; i++) {
        await sleep(500);
        if (await up(PAGE_URL)) {
            console.log('dev server up');
            return;
        }
    }
    throw new Error('the dev server did not come up on ' + PORT);
}

async function ensureBrowser() {
    if (await up('http://127.0.0.1:9333/json/version')) {
        console.log('browser already up on 9333');
        return;
    }
    console.log('starting the headless browser…');
    const out = run(process.execPath, [path.join(HERE, 'cdp.mjs'), 'launch'], {allowFail: true});
    console.log((out || '').trim());
    if (!await up('http://127.0.0.1:9333/json/version')) {
        throw new Error('the browser did not come up on 9333');
    }
    started.browser = true;
}

function stop() {
    if (keep) {
        console.log('\nleft running (--keep): the dev server on ' + PORT + ', the browser on 9333');
        return;
    }
    if (started.browser) {
        // The browser was started by this run: it is ours to close.
        try { execFileSync('taskkill', ['/F', '/IM', 'chrome.exe'], {stdio: 'ignore'}); } catch { /* not there */ }
        try { execFileSync('taskkill', ['/F', '/IM', 'msedge.exe'], {stdio: 'ignore'}); } catch { /* not there */ }
    }
    if (started.server && started.server.pid) {
        try { process.kill(started.server.pid); } catch { /* already gone */ }
    }
}

let failures = 0;
try {
    await ensureServer();
    await ensureBrowser();

    for (const p of probes) {
        if (!fs.existsSync(path.join(HERE, p + '.mjs'))) {
            console.log(`${p.padEnd(18)} (missing)`);
            continue;
        }
        run(process.execPath, [path.join(HERE, 'cdp.mjs'), 'open', PAGE_URL]);
        const out = run(process.execPath, [path.join(HERE, 'cdp.mjs'), 'run', path.join(HERE, p + '.mjs')]);
        const errBlock = (out.match(/"errs[^"]*":\s*\[([\s\S]*?)\]/) || [, ''])[1];
        const errCount = (errBlock.match(/"kind"/g) || []).length;
        const crash = /"crash"\s*:\s*true/.test(out) || /"crashScreen"\s*:\s*true/.test(out);
        const gone = /"appStillThere"\s*:\s*false/.test(out);
        // The driver failing is a failure too: a probe that never ran says nothing
        // about the app, and counting it as a pass is how a broken suite stays
        // green.
        const driverBroke = /cdp error|^ERROR/i.test(out);
        const bad = driverBroke || errCount > 0 || crash || gone;
        if (bad) failures++;
        console.log(
            `${p.padEnd(18)} ${bad ? 'FAIL' : 'ok  '}  errs=${errCount} crash=${crash}` +
            (bad ? '\n    ' + (errBlock || out).replace(/\s+/g, ' ').trim().slice(0, 300) : ''));
    }
} catch (e) {
    console.error('the run could not start: ' + e.message);
    failures++;
} finally {
    stop();
}

console.log(failures === 0
    ? `\n${probes.length} 个探针全部通过`
    : `\n${failures} 个探针有问题`);
process.exit(failures === 0 ? 0 : 1);

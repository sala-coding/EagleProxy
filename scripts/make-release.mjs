// Creates the GitHub release for a tag and uploads the built exe — using the
// OAuth token Git's credential manager already holds (git credential fill, read
// fresh here; it is never written to disk or echoed).
//
// Usage: node scripts/make-release.mjs <version>   e.g. v1.0.0
import {execFileSync} from 'node:child_process';
import {copyFileSync, existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';

const version = process.argv[2];
if (!version) {
    console.error('usage: node scripts/make-release.mjs <version>');
    process.exit(1);
}
const repo = 'sala-coding/EagleProxy';

const cred = execFileSync('git', ['credential', 'fill'], {input: `protocol=https\nhost=github.com\n\n`}).toString();
const m = cred.match(/password=(\S+)/);
if (!m) { console.error('no stored credential'); process.exit(1); }
const token = m[1];

const API = `https://api.github.com/repos/${repo}`;
const UP = `https://uploads.github.com/repos/${repo}/releases`;
const H = {'User-Agent': 'eagle-release', Authorization: `Bearer ${token}`, Accept: 'application/vnd.github+json'};

// The notes for this version, when there are any: one file per release under
// scripts/notes, read here so what changed is written down where it can be reviewed —
// instead of the same paragraph being pasted into this script and left unchanged for
// the next version, which is how every release came to say the same thing.
const notesPath = `scripts/notes/${version}.md`;
const notes = existsSync(notesPath) ? readFileSync(notesPath, 'utf8').trim() : '';
const fallback = [
    `EagleProxy ${version}`,
    '',
    'Wails 桌面代理客户端：订阅管理、节点表格、TUN / 系统代理一键切换',
    '内核 sing-box；需要 Windows 10/11（WebView2 缺失时启动会引导安装）',
    '组内热切换、DNS 管理卡、规则集下载、全局热键与托盘',
    '',
    '下载 `Eagle-' + version + '-windows.exe` 即为 Windows 版本。',
].join('\n');
const body = notes || fallback;

// 1. create (or reuse) the release for the tag
let rel = await (await fetch(`${API}/releases/tags/${version}`, {headers: H})).json();
if (!rel.id) {
    const r = await fetch(`${API}/releases`, {
        method: 'POST',
        headers: {...H, 'Content-Type': 'application/json'},
        body: JSON.stringify({
            tag_name: version,
            name: version,
            target_commitish: 'main',
            body,
        }),
    });
    rel = await r.json();
} else if (notes && rel.body !== body) {
    // A re-run is how a release's notes are corrected: the tag and the asset are
    // already there, and only the paragraph above the download was wrong.
    const r = await fetch(`${API}/releases/${rel.id}`, {
        method: 'PATCH',
        headers: {...H, 'Content-Type': 'application/json'},
        body: JSON.stringify({body}),
    });
    console.log(r.ok ? 'release notes updated' : `notes update failed: HTTP ${r.status}`);
}
if (!rel.id) {
    console.error('release create failed:', JSON.stringify(rel).slice(0, 300));
    process.exit(1);
}
console.log('release:', rel.html_url);

// The full download: the program AND the core it runs, zipped — what a reader on a
// fresh machine actually needs. The core is kept out of the repository (.gitignore)
// and out of the exe (nothing is embedded beside the front end), so the only way to
// hand it over is beside the program, in the `sing-box/` folder the auto-detection
// looks for. Unzipping that folder and the exe side by side is the whole install.
function buildFullZip() {
    if (!existsSync(join('sing-box', 'sing-box.exe'))) return null;
    const stage = mkdtempSync(join(tmpdir(), 'eagle-full-'));
    copyFileSync('build/bin/Eagle.exe', join(stage, 'Eagle.exe'));
    mkdirSync(join(stage, 'sing-box'));
    // Every core the folder holds, not just the one auto-detection names first: the
    // settings' core dropdown switches between what is sitting here, so a zip carrying
    // one build would land a reader with nothing to switch to.
    for (const f of readdirSync('sing-box')) {
        if (!/\.(exe|dll)$/i.test(f)) continue;
        copyFileSync(join('sing-box', f), join(stage, 'sing-box', f));
    }
    const zip = join(tmpdir(), `Eagle-${version}-windows-full.zip`);
    rmSync(zip, {force: true});
    execFileSync('powershell.exe', ['-NoProfile', '-NonInteractive', '-Command',
        `Compress-Archive -Path '${join(stage, '*')}' -DestinationPath '${zip}' -Force`]);
    rmSync(stage, {recursive: true, force: true});
    return zip;
}

// 2. upload the assets; an asset of the same name already sitting on the release is
//    deleted first, so a re-release replaces it rather than failing
//
// Two of them, because two readers want different things. The updater picks the .exe
// (see update.WindowsAsset), so in-place updates keep working; the zip is for a first
// install on a machine with no core yet.
const assets = [{name: `Eagle-${version}-windows.exe`, file: 'build/bin/Eagle.exe'}];
const zip = buildFullZip();
if (zip) assets.push({name: `Eagle-${version}-windows-full.zip`, file: zip});
else console.warn('no sing-box/ beside the repo: publishing the exe only');

for (const a of assets) {
    for (const old of (rel.assets ?? [])) {
        if (old.name === a.name) {
            const del = await fetch(`${API}/releases/assets/${old.id}`, {method: 'DELETE', headers: H});
            console.log(del.status === 204 ? `deleted old asset: ${a.name}` : `delete failed: HTTP ${del.status}`);
        }
    }
    const up = await fetch(`${UP}/${rel.id}/assets?name=${encodeURIComponent(a.name)}`, {
        method: 'POST',
        headers: {Authorization: `Bearer ${token}`, 'Content-Type': 'application/octet-stream'},
        body: readFileSync(a.file),
    });
    const j = await up.json();
    console.log(up.status === 201 ? `uploaded: ${j.browser_download_url}` : `upload failed: ${JSON.stringify(j).slice(0, 300)}`);
}

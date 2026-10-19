// Creates the GitHub release for a tag and uploads the built exe — using the
// OAuth token Git's credential manager already holds (git credential fill, read
// fresh here; it is never written to disk or echoed).
//
// Usage: node scripts/make-release.mjs <version>   e.g. v1.0.0
import {execFileSync} from 'node:child_process';
import {readFileSync} from 'node:fs';

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
            body: [
                `EagleProxy ${version}`,
                '',
                'Wails 桌面代理客户端：订阅管理、节点表格、TUN / 系统代理一键切换',
                '内核 sing-box；需要 Windows 10/11（WebView2 缺失时启动会引导安装）',
                '组内热切换、DNS 管理卡、规则集下载、全局热键与托盘',
                '',
                '下载 `Eagle-' + version + '-windows.exe` 即为 Windows 版本。',
            ].join('\n'),
        }),
    });
    rel = await r.json();
}
if (!rel.id) {
    console.error('release create failed:', JSON.stringify(rel).slice(0, 300));
    process.exit(1);
}
console.log('release:', rel.html_url);

// 2. upload the exe as an asset; an asset of the same name already sitting on
//    the release is deleted first, so a re-release replaces it rather than
//    failing
//
// The name carries the platform, because that is what tells a build for this
// machine apart from a build for another one: the updater reads the name to
// pick the Windows asset out of a release that may carry several.
const name = `Eagle-${version}-windows.exe`;
const exe = readFileSync('build/bin/Eagle.exe');
for (const a of (rel.assets ?? [])) {
    if (a.name === name) {
        const del = await fetch(`${API}/releases/assets/${a.id}`, {method: 'DELETE', headers: H});
        console.log(del.status === 204 ? `deleted old asset: ${name}` : `delete failed: HTTP ${del.status}`);
    }
}
const up = await fetch(`${UP}/${rel.id}/assets?name=${encodeURIComponent(name)}`, {
    method: 'POST',
    headers: {Authorization: `Bearer ${token}`, 'Content-Type': 'application/octet-stream'},
    body: exe,
});
const j = await up.json();
console.log(up.status === 201 ? `uploaded: ${j.browser_download_url}` : `upload failed: ${JSON.stringify(j).slice(0, 300)}`);

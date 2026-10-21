// diff-settings-keys.mjs — 前端设置表单写的键 vs 后端 setting.Setting 真正有的键。
// 前端写而后端没有的键 = 存盘时被 json.Unmarshal 丢掉 = 静默失效的设置。
import fs from 'node:fs';

const root = 'E:/project/eagle-desk';

// --- 后端：setting.Setting 的 JSON 标签 ---
const go = fs.readFileSync(root + '/internal/setting/setting.go', 'utf8');
const goKeys = new Set();
for (const m of go.matchAll(/json:"([A-Za-z0-9_]+)/g)) goKeys.add(m[1]);
// 嵌套结构体的字段也算（inbound_auth / extraCore 里的）
for (const m of go.matchAll(/type\s+(\w+)\s+struct\s*\{([\s\S]*?)\n\}/g)) {
    for (const t of m[2].matchAll(/json:"([A-Za-z0-9_]+)/g)) goKeys.add(t[1]);
}

// --- 前端：设置表单涉及的键 ---
const meta = fs.readFileSync(root + '/frontend/src/lib/settings-meta.ts', 'utf8');
const front = new Set();
for (const m of meta.matchAll(/'([A-Za-z0-9_]+)'/g)) {
    // 只收 SETTING_GROUPS 的 keys 与 SETTING_HIDDEN 的元素
}
const groupsBlock = meta.slice(meta.indexOf('export const SETTING_GROUPS'),
    meta.indexOf('export const FRAME_KEYS'));
for (const m of groupsBlock.matchAll(/'([A-Za-z0-9_]+)'/g)) front.add(m[1]);
const hiddenBlock = meta.slice(meta.indexOf('export const SETTING_HIDDEN'),
    meta.indexOf('export const LANGUAGES'));
for (const m of hiddenBlock.matchAll(/'([A-Za-z0-9_]+)'/g)) front.add(m[1]);

// TUN 卡与路由卡自己拥有的键
for (const f of ['frontend/src/components/settings/TunCard.vue',
    'frontend/src/components/settings/RoutingCard.vue',
    'frontend/src/components/settings/DnsCard.vue',
    'frontend/src/components/settings/RuleSetsCard.vue']) {
    const s = fs.readFileSync(root + '/' + f, 'utf8');
    for (const m of s.matchAll(/'([a-z][a-z0-9_]{2,})'/gi)) front.add(m[1]);
}

const onlyFront = [...front].filter(k => !goKeys.has(k)).sort();
const onlyBack = [...goKeys].filter(k => !front.has(k)).sort();

console.log('=== 前端用到、后端没有的键（存盘会被丢掉）===');
console.log(onlyFront.join('\n') || '(none)');
console.log('\n=== 后端有、前端表单没覆盖的键 ===');
console.log(onlyBack.join('\n') || '(none)');
console.log(`\n后端键 ${goKeys.size} / 前端键 ${front.size}`);

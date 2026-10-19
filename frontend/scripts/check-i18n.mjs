// QA 静态检查：提取源码中 t('...') / tip('...') 键，核对 zh/en/ru 字典与 tips.*.json。
// 用法: node scripts/check-i18n.mjs  （只读，不修改任何文件）
import {readFileSync, readdirSync, statSync, existsSync} from 'node:fs';
import {join, dirname} from 'node:path';
import {fileURLToPath} from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const SRC = join(root, 'src');
const TIPS = join(root, 'public', 'fa', 'tips');

// ---- 1. 收集源码文件 ----
function walk(dir, out = []) {
    for (const name of readdirSync(dir)) {
        const p = join(dir, name);
        const st = statSync(p);
        if (st.isDirectory()) walk(p, out);
        else if (/\.(ts|vue)$/.test(name)) out.push(p);
    }
    return out;
}
const files = walk(SRC);

// ---- 2. 提取键 ----
// 静态键: t('key') / t("key") / tip('key') / tip("key")
// 动态键: t(`egress.${mode}`) 等 —— 单独记录为动态模板
const used = new Map(); // key -> [file:line]
const usedTip = new Map(); // tip() 专用
const dynamic = [];     // {file, line, expr}
const KEY_RE = /\b(t|tip)\(\s*(['"])((?:\\.|(?!\2).)+)\2/g;
const DYN_RE = /\b(t|tip)\(\s*`([^`]*\$\{[^`]*\}[^`]*)`/g;

for (const f of files) {
    const text = readFileSync(f, 'utf8');
    const rel = f.slice(root.length + 1);
    const lines = text.split('\n');
    lines.forEach((line, i) => {
        let m;
        KEY_RE.lastIndex = 0;
        while ((m = KEY_RE.exec(line))) {
            const key = m[3].replace(/\\'/g, "'").replace(/\\"/g, '"');
            const target = m[1] === 'tip' ? usedTip : used;
            if (!target.has(key)) target.set(key, []);
            target.get(key).push(`${rel}:${i + 1}`);
        }
        DYN_RE.lastIndex = 0;
        while ((m = DYN_RE.exec(line))) {
            dynamic.push({where: `${rel}:${i + 1}`, expr: `${m[1]}(\`${m[2]}\`)`});
        }
    });
}

// ---- 3. 载入字典 ----
function extractDict(tsPath, varName) {
    const text = readFileSync(tsPath, 'utf8');
    // 找到 const <varName>: Record<string,string> = { ... }; 的对象体
    const start = text.search(new RegExp(`const ${varName}\\b`));
    const braceAt = text.indexOf('{', text.indexOf('=', start));
    // 简单括号配平（文件里没有会干扰的嵌套字符串大括号风险低，但字符串里可能有 {0}）
    let depth = 0, end = -1, inStr = null;
    for (let i = braceAt; i < text.length; i++) {
        const c = text[i];
        if (inStr) {
            if (c === '\\') { i++; continue; }
            if (c === inStr) inStr = null;
            continue;
        }
        if (c === '\'' || c === '"' || c === '`') { inStr = c; continue; }
        if (c === '{') depth++;
        else if (c === '}') { depth--; if (depth === 0) { end = i; break; } }
    }
    const body = text.slice(braceAt + 1, end);
    const dict = new Map();
    const KV = /['"]((?:\\.|[^'"])+)['"]\s*:\s*(?:"((?:\\.|[^"])*)"|'((?:\\.|[^'])*)')/g;
    let m;
    while ((m = KV.exec(body))) {
        const k = m[1].replace(/\\'/g, "'");
        dict.set(k, m[2] ?? m[3] ?? '');
    }
    return dict;
}

const zh = extractDict(join(SRC, 'i18n.ts'), 'zh');
const en = extractDict(join(SRC, 'i18n.ts'), 'en');
const ru = extractDict(join(SRC, 'i18n', 'ru.ts'), 'ru');

const tips = {};
for (const lg of ['zh', 'en', 'ru', 'fa', 'ko', 'my', 'tk']) {
    const p = join(TIPS, `tips.${lg}.json`);
    tips[lg] = existsSync(p) ? new Map(Object.entries(JSON.parse(readFileSync(p, 'utf8')))) : new Map();
}

// 动态键展开：手工列出已知动态前缀对应的候选键
const dynamicExpansion = {
    't(`egress.${mode}`)': ['egress.rule', 'egress.direct', 'egress.global'],
};

// ---- 4. 汇总 ----
console.log(`\n=== 源码使用的 t() 静态键: ${used.size} 个; tip() 静态键: ${usedTip.size} 个 ===\n`);

const usedKeys = new Set(used.keys());
const usedTipKeys = new Set(usedTip.keys());
for (const [expr, cands] of Object.entries(dynamicExpansion)) {
    console.log(`[动态键] ${expr} → 候选: ${cands.join(', ')}`);
    for (const c of cands) usedKeys.add(c);
}

function reportMissing(name, dict, keySet, getWhere) {
    const missing = [...keySet].filter(k => !dict.has(k));
    if (missing.length) {
        console.log(`\n[缺失] ${name} 缺少 ${missing.length} 个被使用的键:`);
        for (const k of missing.sort()) {
            console.log(`   ${k}   ← 使用处: ${(getWhere(k) ?? ['(动态展开)']).join(', ')}`);
        }
    } else {
        console.log(`\n[OK] ${name}: 被使用的键全部存在`);
    }
}

reportMissing('zh (i18n.ts)', zh, usedKeys, k => used.get(k));
reportMissing('en (i18n.ts)', en, usedKeys, k => used.get(k));
reportMissing('ru (i18n/ru.ts)', ru, usedKeys, k => used.get(k));
for (const lg of Object.keys(tips)) reportMissing(`tips.${lg}.json`, tips[lg], usedTipKeys, k => usedTip.get(k));

// ---- 5. 反向：定义但未使用 ----
console.log('\n=== 反向核对：定义但从未使用的键 ===\n');
function reportUnused(name, dict, keySet) {
    const unused = [...dict.keys()].filter(k => !keySet.has(k));
    if (unused.length) {
        console.log(`[未使用] ${name}: ${unused.length} 个`);
        for (const k of unused.sort()) console.log(`   ${k}`);
    } else {
        console.log(`[OK] ${name}: 无未使用键`);
    }
}
reportUnused('zh (i18n.ts)', zh, usedKeys);
reportUnused('en (i18n.ts)', en, usedKeys);
reportUnused('ru (i18n/ru.ts)', ru, usedKeys);
for (const lg of Object.keys(tips)) reportUnused(`tips.${lg}.json`, tips[lg], usedTipKeys);

// ---- 6. 表间差异（zh 有而 en/ru 没有的定义键）----
console.log('\n=== 表间差异（定义层面）===\n');
function diff(name, dict, base) {
    const miss = [...base.keys()].filter(k => !dict.has(k));
    if (miss.length) {
        console.log(`[差异] ${name} 相对 zh 缺 ${miss.length} 个键: ${miss.sort().join(', ')}`);
    } else {
        console.log(`[OK] ${name}: 与 zh 键集一致`);
    }
}
diff('en', en, zh);
diff('ru', ru, zh);
for (const lg of Object.keys(tips)) diff(`tips.${lg}`, tips[lg], tips.zh);

console.log('\n=== 动态键调用点（供人工核对）===');
for (const d of dynamic) console.log(`   ${d.where}: ${d.expr}`);

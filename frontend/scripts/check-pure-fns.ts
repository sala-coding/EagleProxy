// QA 纯函数抽查：format.ts / text.ts / dns.ts（仅纯函数部分）。
// 用法: npx tsx scripts/check-pure-fns.ts
import * as f from '../src/lib/format';
import * as tx from '../src/lib/text';

let pass = 0, fail = 0;
function eq(actual: unknown, expected: unknown, label: string) {
    const a = JSON.stringify(actual), e = JSON.stringify(expected);
    if (a === e) { pass++; console.log(`  ok  ${label} → ${a}`); }
    else { fail++; console.log(`FAIL  ${label}: got ${a}, want ${e}`); }
}

console.log('--- fmtBytes ---');
eq(f.fmtBytes(0), '0 B', '0');
eq(f.fmtBytes(-5), '0 B', '负数');
eq(f.fmtBytes(NaN), '0 B', 'NaN');
eq(f.fmtBytes(1), '1 B', '1B 无小数');
eq(f.fmtBytes(1023), '1023 B', '1023B');
eq(f.fmtBytes(1024), '1.0 KB', '1KB');
eq(f.fmtBytes(1536), '1.5 KB', '1.5KB');
eq(f.fmtBytes(1024 ** 3), '1.0 GB', '1GB');
eq(f.fmtBytes(1024 ** 4), '1.0 TB', '1TB');
eq(f.fmtBytes(1024 ** 5), '1024.0 TB', '超大数封顶在 TB（不进 PB）');
eq(f.fmtBytes(0.5), '1 B', '0.5 → toFixed(0) 四舍五入为 1 B（记录行为）');
console.log('--- fmtRate ---');
eq(f.fmtRate(1024), '1.0 KB/s', '1KB/s');

console.log('--- charCols/fitName ---');
eq(f.charCols('中'), 2, '中文=2列');
eq(f.charCols('a'), 1, '拉丁=1列');
eq(f.charCols('Ａ'), 2, '全角=2列');
eq(f.fitName('abc', 5).text, 'abc', '截断保留');
eq(f.fitName('abc', 5).pad.length, 2, '填充列数');
eq(f.fitName('中文', 3).text, '中', '全角不拆半：第二字放不下');
eq(f.fitName('', 3).pad.length, 3, '空串填充');
eq(f.fitName('中文', 0).text, '', 'cols=0 → 空');
eq(f.fitName('中文', 4).pad, '', '恰好填满无填充');

console.log('--- parseSubUsage ---');
eq(f.parseSubUsage(undefined), null, 'undefined');
eq(f.parseSubUsage(''), null, '空串');
eq(f.parseSubUsage('upload=1; download=2'), {used: 3, total: 0, left: 0, expire: 0}, '仅 used');
eq(f.parseSubUsage('upload=1; download=2; total=10'), {used: 3, total: 10, left: 7, expire: 0}, 'left 计算');
eq(f.parseSubUsage('total=0; upload=5'), {used: 5, total: 0, left: 0, expire: 0}, 'total=0 不限量');
// 超限: used > total → left 钳到 0
eq(f.parseSubUsage('upload=20; download=20; total=10').left, 0, '超限钳 0');
// expire=0 → "永不到期"，无其它字段时整体返回 null（正确语义）
eq(f.parseSubUsage('expire=0'), null, 'expire=0 单独 → null');
// expire 日期串
const d = f.parseSubUsage('expire=2030-01-01');
console.log('  info expire 日期串 →', JSON.stringify(d && d.expire > 1700000000 ? 'ok' : d));
// 非法键值
eq(f.parseSubUsage(';;=;x'), null, '全垃圾 → null');
// 大小写与空格
eq(f.parseSubUsage(' Upload=1 ;  Download=2 ; TOTAL=10 ').left, 7, '大小写/空格容错');
// 负数被拒
eq(f.parseSubUsage('upload=-5; total=10'), {used: 0, total: 10, left: 10, expire: 0}, '负数拒绝');

console.log('--- addressInLine / hostFirstSegment ---');
eq(tx.addressInLine('connection to example.com:443'), 'example.com', '域名提取');
eq(tx.addressInLine('outbound/direct → 142.250.0.1:80'), '142.250.0.1', 'IP 优先');
eq(tx.addressInLine('starting sing-box.exe'), '', '文件名不算域名');
eq(tx.addressInLine('no address here'), '', '无地址');
eq(tx.addressInLine(''), '', '空行');
eq(tx.hostFirstSegment('https://example-sub.example.com/feed/xxx'), 'example-sub', '订阅名提取');
eq(tx.hostFirstSegment('example.com/feed'), 'example', '裸 host 补 scheme');
eq(tx.hostFirstSegment(''), '', '空串');
eq(tx.hostFirstSegment('not a url:://'), '', '非法 URL');
eq(tx.hostFirstSegment('https://'), '', '仅 scheme');

console.log(`\n${pass} passed, ${fail} failed`);
process.exit(fail ? 1 : 0);

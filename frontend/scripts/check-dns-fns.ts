// QA：dns.ts 纯函数抽查（dnsServerToRow/dnsRowToServer/roundChoice/dnsCandidates）
// 用法: npx tsx scripts/check-dns-fns.ts
let pass = 0, fail = 0;
function eq(actual: unknown, expected: unknown, label: string) {
    const a = JSON.stringify(actual), e = JSON.stringify(expected);
    if (a === e) { pass++; console.log(`  ok  ${label} → ${a}`); }
    else { fail++; console.log(`FAIL  ${label}: got ${a}, want ${e}`); }
}

const dns = await import('../src/lib/dns');

console.log('--- dnsServerToRow / dnsRowToServer 往返 ---');
// 普通 UDP 带端口
const r1 = dns.dnsServerToRow({tag: 'dns-direct', type: 'udp', server: '223.5.5.5', server_port: 53});
eq(r1.addr, '223.5.5.5:53', 'addr 拼端口');
eq(dns.dnsRowToServer(r1), {type: 'udp', tag: 'dns-direct', server: '223.5.5.5', server_port: 53}, '往返无损');
// IPv6 带端口
const r2 = dns.dnsServerToRow({type: 'udp', server: '2400:3200::1', server_port: 53});
eq(r2.addr, '[2400:3200::1]:53', 'IPv6 加方括号');
eq(dns.dnsRowToServer(r2).server, '2400:3200::1', 'IPv6 去括号还原');
// 多冒号 IPv6 无端口
const r2b = dns.dnsServerToRow({type: 'udp', server: '2400:3200:baba::1'});
eq(dns.dnsRowToServer(r2b).server, '2400:3200:baba::1', '多冒号无端口不误拆');
// local 类型：清空所有地址字段
const r3 = dns.dnsServerToRow({type: 'local', server: 'x', server_port: 1, detour: 'proxy'});
const back3 = dns.dnsRowToServer({...r3, type: 'local', addr: 'x:1', detour: 'proxy'});
eq('server' in back3, false, 'local 清 server');
eq('detour' in back3, false, 'local 清 detour');
// 空串字段被删
const r4 = dns.dnsRowToServer(dns.dnsServerToRow({type: 'https', server: 'doh.pub', path: ''}));
eq('path' in r4, false, '空 path 删除');
// 非对象输入
eq(dns.dnsServerToRow(null).type, 'udp', 'null → 默认 udp');
eq(dns.dnsServerToRow(null).raw, {}, 'null → 空 raw');

console.log('--- roundChoice ---');
const mk = (o: any) => o;
// 优选 on：无条件取最快
eq(dns.roundChoice('remote', mk({dns_smart_select: 1}), {remote: {value: 'x', name: 'x', ms: 5}, direct: null, latency: {}, note: ''})?.value, 'x', '优选 on');
// 都关：不动
eq(dns.roundChoice('remote', mk({}), {remote: {value: 'x', name: 'x', ms: 5}, direct: null, latency: {}, note: ''}), null, '都关 → null');
// 切换 on，当前慢且有更快的
eq(dns.roundChoice('direct', mk({dns_smart_switch: 1, direct_dns: 'slow'}),
    {remote: null, direct: {value: 'fast', name: 'f', ms: 100}, latency: {slow: 300}, note: ''})?.value, 'fast', '切换 on：慢→换');
// 当前不慢 → 保持
eq(dns.roundChoice('direct', mk({dns_smart_switch: 1, direct_dns: 'ok'}),
    {remote: null, direct: {value: 'fast', name: 'f', ms: 100}, latency: {ok: 100}, note: ''}), null, '100ms < 200 → 保持');
// 当前未测得（latency 无记录）→ 不猜
eq(dns.roundChoice('direct', mk({dns_smart_switch: 1, direct_dns: 'unknown'}),
    {remote: null, direct: {value: 'fast', name: 'f', ms: 100}, latency: {}, note: ''}), null, '未测得 → 不动');
// 提升不足 30ms → 不动
eq(dns.roundChoice('direct', mk({dns_smart_switch: 1, direct_dns: 'cur'}),
    {remote: null, direct: {value: 'alt', name: 'a', ms: 180}, latency: {cur: 200}, note: ''}), null, '只快 20ms → 不动');

console.log('--- dnsCandidates ---');
// 加密过滤：remote 只留 doh 组
const c1 = dns.dnsCandidates(dns.REMOTE_DNS_PRESETS, true);
eq(c1.every(v => v.startsWith('https://') || v.startsWith('tls://')), true, 'remote 全加密');
eq(c1.includes('local'), false, '不含 local');
// 去重
const c2 = dns.dnsCandidates(dns.DIRECT_DNS_PRESETS, false);
eq(new Set(c2).size === c2.length, true, 'direct 去重');
// 直连列表包含过滤组（参与排序）
eq(c2.includes('114.114.114.110'), true, '家庭组参与测量');

console.log(`\n${pass} passed, ${fail} failed`);
process.exit(fail ? 1 : 0);

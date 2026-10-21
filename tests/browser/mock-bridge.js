// mock-bridge.js — a fake Wails backend for driving the real UI in a browser.
//
// The app talks to Go through window.go.main.App.* (see wailsjs/go/main/App.js) and
// receives backend pushes through window.runtime.EventsOn. This file stands in for
// both, with a small in-memory dataset shaped exactly like the Go models in
// wailsjs/go/models.ts.
//
// It is a test harness, not production code: nothing in src/ imports it. The driver
// injects it into the page before the app's own module runs (see cdp.mjs), so the
// page under test is the application's own page.
//
// The harness exposes window.__mock for the test driver:
//   __mock.calls          every backend call, as {name, args}
//   __mock.emit(ev, ...)  push a backend event into the app
//   __mock.fail(name)     make the named call reject from now on
//   __mock.unfail(name)   stop rejecting
//   __mock.state          the fake dataset
(function () {
    'use strict';

    const calls = [];
    const failures = new Set();

    // ---- the fake dataset -------------------------------------------------

    const now = Date.now();
    const HOUR = 3600 * 1000;

    const state = {
        runningId: -1919,          // core.StoppedID
        runningName: '',
        spMode: {vpn: false, systemProxy: false},
        egress: 'rule',
        activeRouting: 'Default',
        nextProfileId: 100,
        nextGroupId: 10,
        traffic: {up: 0, down: 0, upRate: 0, downRate: 0, count: 0},
        exitIP: {ip: '1.2.3.4', country: 'United States', countryCode: 'US'},
        notices: [],
    };

    // info is the raw Subscription-UserInfo header the backend stores
    // (internal/sub/update.go: g.Info = subUserInfo), so it is written in the
    // header's own "k=v; k=v" form and not as prose.
    const groups = [
        {id: 1, name: '香港中转', archive: false, profileCount: 9, url: 'https://example.com/sub1', subLastUpdate: now - 2 * HOUR, info: 'upload=1181116006; download=18119393249; total=1073741824000; expire=1798761600'},
        {id: 2, name: '美国原生', archive: false, profileCount: 4, url: 'https://example.com/sub2', subLastUpdate: now - 30 * HOUR, info: 'upload=1073741824; download=1073741824; total=107374182400; expire=1798761600'},
        {id: 3, name: '自建节点', archive: true, profileCount: 2, url: '', subLastUpdate: 0, info: ''},
    ];

    // Latency/speed deliberately mixed: some measured, some untested (0), one
    // failed (-1 is the app's "failed" marker — check the Go side for the exact
    // sentinel), and one with a huge value to trip up any unit math.
    // speed is MB/s, not bytes/s: the backend divides by 1024/1024 before it
    // stores it (internal/speedtest/speedtest.go, downloadSpeed).
    const profileSeeds = [
        ['🇭🇰 香港 IEPL 专线 01', 'hk01.example.com', 2080, 45, 88.5, 'vmess'],
        ['🇭🇰 香港 IEPL 专线 02', 'hk02.example.com', 2081, 52, 74.5, 'vless'],
        ['🇭🇰 香港 BGP 中转', 'hk03.example.com', 2082, 120, 32.0, 'trojan'],
        ['🇭🇰 香港 流媒体解锁', 'hk04.example.com', 2083, 210, 12.0, 'hysteria2'],
        ['🇺🇸 美国 原生 IP 01', 'us01.example.com', 2084, 165, 55.0, 'vmess'],
        ['🇺🇸 美国 原生 IP 02', 'us02.example.com', 2085, 178, 51.0, 'tuic'],
        ['🇯🇵 日本 东京 软银', 'jp01.example.com', 2086, 95, 41.0, 'vless'],
        ['🇸🇬 新加坡 狮城', 'sg01.example.com', 2087, 88, 38.0, 'ss'],
        ['unmeasured node', 'un.example.com', 2088, 0, 0, 'vmess'],
    ];

    const profiles = [];
    profileSeeds.forEach((s, i) => {
        const gid = i < 6 ? 1 : (i < 8 ? 2 : 3);
        profiles.push({
            id: i + 1,
            gid,
            type: s[5],
            name: s[0],
            address: s[1],
            latency: s[3],
            report: s[3] > 0 ? `${s[3]}ms` : '',
            speed: s[4],
            up: i === 0 ? 1234 : 0,
            down: i === 0 ? 987654 : 0,
            port: s[2],
            network: 'tcp',
            security: 'tls',
            tlsNote: '',
            group: groups.find(g => g.id === gid).name,
        });
    });

    const routings = {
        'Default': {
            direct_ip: 'geoip:cn\ngeoip:private',
            direct_domain: 'geosite:cn',
            proxy_ip: '',
            proxy_domain: '',
            block_ip: '',
            block_domain: 'geosite:category-ads-all',
            def_outbound: 'proxy',
            custom: '{"rules": []}',
            remote_dns: 'https://dns.google/dns-query',
            remote_dns_strategy: '',
            direct_dns: 'https://doh.pub/dns-query',
            direct_dns_strategy: '',
            domain_strategy: '',
            outbound_domain_strategy: '',
            dns_routing: true,
            sniffing_mode: 1,
            use_dns_object: false,
            dns_object: '',
            dns_final_out: 'proxy',
            dns_smart_select: false,
            dns_smart_minutes: 30,
            dns_smart_switch: false,
        },
        'Bypass LAN and Mainland': {
            direct_ip: 'geoip:cn\ngeoip:private',
            direct_domain: 'geosite:cn',
            proxy_ip: '', proxy_domain: '', block_ip: '', block_domain: '',
            def_outbound: 'proxy', custom: '{"rules": []}',
            remote_dns: 'https://dns.google/dns-query', remote_dns_strategy: '',
            direct_dns: 'https://doh.pub/dns-query', direct_dns_strategy: '',
            domain_strategy: '', outbound_domain_strategy: '',
            dns_routing: true, sniffing_mode: 1, use_dns_object: false,
            dns_object: '', dns_final_out: 'proxy',
            dns_smart_select: false, dns_smart_minutes: 30, dns_smart_switch: false,
        },
    };

    const ruleSets = [
        {tag: 'geoip-cn', url: 'https://raw.githubusercontent.com/x/geoip-cn.srs', needed: true, local: false, size: 1_200_000, modified: '2026-10-01', country: 'cn'},
        {tag: 'geosite-cn', url: 'https://raw.githubusercontent.com/x/geosite-cn.srs', needed: true, local: false, size: 3_400_000, modified: '2026-10-01', country: 'cn'},
        {tag: 'geoip-private', url: 'https://raw.githubusercontent.com/x/geoip-private.srs', needed: true, local: true, size: 800, modified: '2026-09-30', country: 'private'},
        {tag: 'category-ads-all', url: 'https://raw.githubusercontent.com/x/ads.srs', needed: false, local: false, size: 55_000, modified: '2026-08-12', country: 'ads'},
    ];

    const logLines = [
        ['sing-box 1.10.0 starting', 'core'],
        ['inbound/socks: tcp server started at 127.0.0.1:2080', 'core'],
        ['outbound/vmess[h01] connection established', 'core'],
        ['WARN rule-set geoip-cn download failed: timeout', 'core'],
        ['DEBUG dns query example.com -> 93.184.216.34', 'core'],
        ['ERROR outbound/hysteria2 handshake failed', 'core'],
        ['INFO traffic: up=1.2KB down=8.4KB', 'core'],
        ['DEBUG tls handshake complete sni=hk01.example.com', 'core'],
    ];

    const connections = {
        downloadTotal: 987654321,
        uploadTotal: 12345678,
        connections: [
            {id: 'conn-1', upload: 1024, download: 20480, start: '2026-10-20T05:00:00Z', chains: ['h01'], rule: 'final', rulePayload: '', metadata: {network: 'tcp', type: 'HTTP', sourceIP: '192.168.1.10', destinationIP: '93.184.216.34', sourcePort: '52344', destinationPort: '443', host: 'example.com', processPath: 'C:\\Windows\\System32\\svchost.exe'}},
            {id: 'conn-2', upload: 512, download: 4096, start: '2026-10-20T05:02:00Z', chains: ['us01'], rule: 'geosite-cn', rulePayload: 'cn', metadata: {network: 'tcp', type: 'TLS', sourceIP: '192.168.1.10', destinationIP: '1.1.1.1', sourcePort: '52350', destinationPort: '443', host: 'cloudflare-dns.com', processPath: 'C:\\Program Files\\Chrome\\chrome.exe'}},
            {id: 'conn-3', upload: 0, download: 0, start: '2026-10-20T05:03:00Z', chains: [], rule: 'direct', rulePayload: '', metadata: {network: 'udp', type: 'DNS', sourceIP: '192.168.1.10', destinationIP: '8.8.8.8', sourcePort: '53000', destinationPort: '53', host: 'dns.google', processPath: ''}},
        ],
    };

    const settings = {
        extraCore: {core_map: '{}'},
        inbound_auth: {user: '', pass: ''},
        user_agent2: 'mihomo/1.19.0',
        test_url: 'http://cp.cloudflare.com/',
        test_url_dl: 'http://cachefly.cachefly.net/10mb.test',
        test_dl_timeout: 30,
        current_group: 1,
        inbound_address: '127.0.0.1',
        inbound_socks_port: 2080,
        log_level: 'info',
        mux_protocol: 'h2mux',
        mux_concurrency: 8,
        mux_padding: false,
        mux_default_on: false,
        traffic_loop_interval: 1000,
        test_concurrent: 5,
        theme: '0',
        theme_schema: '2',
        custom_inbound: '{"inbounds": []}',
        custom_route: '{"rules": []}',
        sub_use_proxy: false,
        remember_id: -1919,
        remember_enable: false,
        language: 0,
        spmode2: [],
        remember_spmode: false,
        skip_cert: false,
        hk_mw: '', hk_group: '', hk_route: '', hk_spmenu: '', hk_toggle_node: '',
        hk_egress: '', hk_toggle_tun: '', hk_next_node: '', hk_restart_node: '',
        hk_connections: '', hk_logs: '', hk_datadir: '',
        fakedns: false,
        active_routing: 'Default',
        egress_mode: 'rule',
        mw_size: '',
        conn_stat: true,
        vpn_impl: 0,
        vpn_mtu: 9000,
        vpn_ipv6: false,
        ipv6_auto: true,
        vpn_hide_console: true,
        vpn_strict_route: false,
        vpn_bypass_process: '',
        vpn_bypass_cidr: '',
        vpn_rule_white: false,
        check_include_pre: false,
        sp_format: 'http://127.0.0.1:2080',
        sp_bypass: '',
        sp_pac: '',
        sub_clear: false,
        sub_insecure: false,
        sub_auto_update: -30,
        log_ignore: [],
        start_minimal: false,
        max_log_line: 200,
        splitter_state: '',
        utlsFingerprint: '',
        core_box_clash_api: 19090,
        core_box_clash_api_secret: '',
        core_box_underlying_dns: '',
        core_box_cache_file: true,
        vpn_internal_tun: false,
        pending_tun: false,
        pending_tun_profile: -1919,
        keep_tun_front: true,
    };

    // ---- the event bus ----------------------------------------------------

    const handlers = new Map();     // event name -> [fn]
    const offTokens = new Map();    // token -> fn

    function emit(name, ...args) {
        (handlers.get(name) || []).slice().forEach(fn => {
            try {
                fn(...args);
            } catch (e) {
                console.error('[mock] handler for', name, 'threw', e);
            }
        });
    }

    window.runtime = {
        EventsOn: function (name, cb) {
            const list = handlers.get(name) || [];
            list.push(cb);
            handlers.set(name, list);
            const token = {name, cb};
            offTokens.set(token, cb);
            return function () {
                const l = handlers.get(name) || [];
                const i = l.indexOf(cb);
                if (i >= 0) l.splice(i, 1);
            };
        },
        EventsOnMultiple: function (name, cb) {
            return window.runtime.EventsOn(name, cb);
        },
        EventsOnce: function (name, cb) {
            const off = window.runtime.EventsOn(name, cb);
            const wrapped = function (...a) {
                off();
                cb(...a);
            };
            return off;
        },
        EventsOff: function (name) {
            handlers.delete(name);
        },
        EventsOffAll: function () {
            handlers.clear();
        },
        EventsEmit: function (name, ...args) {
            emit(name, ...args);
        },
        LogPrint: function () {},
        LogTrace: function () {},
        LogDebug: function () {},
        LogInfo: function () {},
        LogWarning: function () {},
        LogError: function () {},
        LogFatal: function () {},
        WindowMinimise: function () {},
        WindowMaximise: function () {},
        Unmaximise: function () {},
        WindowIsMaximised: function () { return Promise.resolve(false); },
        WindowSetTitle: function () {},
        WindowSetDarkTheme: function () {},
        Quit: function () {},
        Hide: function () {},
        Show: function () {},
        ClipboardGetText: function () { return Promise.resolve(''); },
        ClipboardSetText: function (t) { state.clipboard = t; return Promise.resolve(); },
        BrowserOpenURL: function () {},
        OnBeforeClose: function () { return Promise.resolve(); },
    };

    // ---- the fake backend -------------------------------------------------

    function ok(v) { return Promise.resolve(v); }

    // record logs the call and, when the test has armed a failure for it, throws.
    // Throwing rather than returning a rejected promise matters: most of the
    // methods below call record() as a statement and then return their own
    // value, so a rejected promise returned from here would be dropped on the
    // floor and the failure would never reach the caller.
    function record(name, args) {
        calls.push({name, args});
        if (failures.has(name)) {
            throw new Error('[mock] ' + name + ' failed on purpose');
        }
    }

    const app = {
        // --- groups / subscriptions ---
        ListGroups: function () { record('ListGroups', []); return ok(groups.slice()); },
        CreateGroup: function (name) {
            record('CreateGroup', [name]);
            const g = {id: state.nextGroupId++, name, archive: false, profileCount: 0, url: '', subLastUpdate: 0, info: ''};
            groups.push(g);
            return ok(g.id);
        },
        CreateSubscription: function (url) {
            record('CreateSubscription', [url]);
            const g = {id: state.nextGroupId++, name: '新订阅', archive: false, profileCount: 0, url, subLastUpdate: 0, info: ''};
            groups.push(g);
            return ok(g.id);
        },
        GetGroup: function (id) {
            record('GetGroup', [id]);
            const g = groups.find(x => x.id === id);
            return ok(g ? {id: g.id, front_proxy_id: 0, archive: g.archive, name: g.name, order: [], url: g.url, info: g.info, lastup: g.subLastUpdate, sub_interval: 60, manually_column_width: false, column_width: []} : null);
        },
        UpdateGroup: function (g) { record('UpdateGroup', [g]); return ok(); },
        DeleteGroup: function (id) {
            record('DeleteGroup', [id]);
            const i = groups.findIndex(x => x.id === id);
            if (i >= 0) groups.splice(i, 1);
            return ok();
        },
        UpdateAllSubscriptions: function () { record('UpdateAllSubscriptions', []); return ok(); },
        UpdateSubscription: function (gid) {
            record('UpdateSubscription', [gid]);
            return ok({gid, added: ['🇭🇰 香港 IEPL 专线 03'], deleted: [], skipped: [], message: '更新成功'});
        },
        DeduplicateGroup: function (gid) { record('DeduplicateGroup', [gid]); return ok(3); },
        ResolveGroupDomains: function (gid) { record('ResolveGroupDomains', [gid]); return ok(9); },
        RemoveFailedProfiles: function (gid) { record('RemoveFailedProfiles', [gid]); return ok(1); },

        // --- profiles / nodes ---
        ListProfiles: function (gid) {
            record('ListProfiles', [gid]);
            return ok(profiles.filter(p => p.gid === gid));
        },
        CreateProfile: function (name, gid) {
            record('CreateProfile', [name, gid]);
            const p = {id: state.nextProfileId++, gid, type: 'vmess', name, address: 'new.example.com', latency: 0, report: '', speed: 0, up: 0, down: 0, port: 2080, network: 'tcp', security: 'tls', tlsNote: '', group: (groups.find(g => g.id === gid) || {}).name || ''};
            profiles.push(p);
            return ok(p.id);
        },
        DeleteProfile: function (id) {
            record('DeleteProfile', [id]);
            const i = profiles.findIndex(p => p.id === id);
            if (i >= 0) profiles.splice(i, 1);
            return ok();
        },
        CloneProfile: function (id) {
            record('CloneProfile', [id]);
            const src = profiles.find(p => p.id === id);
            if (!src) return ok(0);
            const p = Object.assign({}, src, {id: state.nextProfileId++, name: src.name + ' 副本'});
            profiles.push(p);
            return ok(p.id);
        },
        MoveProfile: function (id, gid) {
            record('MoveProfile', [id, gid]);
            const p = profiles.find(x => x.id === id);
            if (p) {
                p.gid = gid;
                p.group = (groups.find(g => g.id === gid) || {}).name || '';
            }
            return ok();
        },
        GetProfileJSON: function (id) {
            record('GetProfileJSON', [id]);
            const p = profiles.find(x => x.id === id);
            return ok(JSON.stringify(p ? {name: p.name, addr: p.address, port: p.port, _v: 2, v: 'vmess'} : {}, null, 2));
        },
        SaveProfileJSON: function (id, json) {
            record('SaveProfileJSON', [id, json]);
            try { JSON.parse(json); } catch (e) { return Promise.reject(new Error('JSON 解析失败: ' + e.message)); }
            return ok();
        },
        ExportLink: function (id, withName) {
            record('ExportLink', [id, withName]);
            const p = profiles.find(x => x.id === id);
            return ok(p ? `vmess://eyJhZGQiOiJ4LmV4YW1wbGUuY29tIiwicG9ydCI6${p.port}LCJpZCI6ImFiYyIsIm5hbWUiOiIke2VuY29kZVRVSUNvbmUocmNvbWVudC5uYW1lfQ==` : '');
        },
        ExportConfigFile: function (id) {
            record('ExportConfigFile', [id]);
            return ok('E:\\eagle\\export\\config.json');
        },
        PreviewConfig: function (id) {
            record('PreviewConfig', [id]);
            return ok('{\n  "log": {"level": "info"},\n  "inbounds": [],\n  "outbounds": []\n}\n');
        },

        // --- import ---
        ImportLink: function (link, gid) {
            record('ImportLink', [link, gid]);
            if (!link || !link.trim()) return Promise.reject(new Error('链接为空'));
            return ok(42);
        },
        ImportLinks: function (links, gid) {
            record('ImportLinks', [links, gid]);
            return ok([41, 42]);
        },
        ReadClipboard: function () { record('ReadClipboard', []); return ok('vmess://eyJhZGQiOiJjbGlwLmV4YW1wbGUuY29tIn0='); },
        WriteClipboard: function (t) { record('WriteClipboard', [t]); state.clipboard = t; return ok(); },

        // --- core / run ---
        StartProfile: function (id) {
            record('StartProfile', [id]);
            const p = profiles.find(x => x.id === id);
            state.runningId = id;
            state.runningName = p ? p.name : '';
            setTimeout(() => emit('running:changed', id), 0);
            return ok();
        },
        StopProfile: function () {
            record('StopProfile', []);
            state.runningId = -1919;
            state.runningName = '';
            setTimeout(() => emit('running:changed', -1919), 0);
            return ok();
        },
        RestartProfile: function () { record('RestartProfile', []); return ok(); },
        StartLastProfile: function () { record('StartLastProfile', []); return ok(); },
        RunningProfile: function () { record('RunningProfile', []); return ok(state.runningId); },
        RunningProfileName: function () { record('RunningProfileName', []); return ok(state.runningName); },
        Refresh: function () { record('Refresh', []); return ok(); },
        ResetMetrics: function () { record('ResetMetrics', []); return ok(); },
        SetCurrentGroup: function (id) { record('SetCurrentGroup', [id]); settings.current_group = id; return ok(); },

        // --- modes ---
        GetSPMode: function () { record('GetSPMode', []); return ok(state.spMode); },
        SetSystemProxyMode: function (v) {
            record('SetSystemProxyMode', [v]);
            state.spMode.systemProxy = v;
            setTimeout(() => emit('spmode:changed'), 0);
            return ok();
        },
        SetVPNMode: function (v) {
            record('SetVPNMode', [v]);
            state.spMode.vpn = v;
            setTimeout(() => emit('spmode:changed'), 0);
            return ok();
        },

        // --- speed test ---
        SpeedtestGroup: function (gid, mode) {
            record('SpeedtestGroup', [gid, mode]);
            const out = profiles.filter(p => p.gid === gid).map(p => ({id: p.id, mode, latency: 30 + Math.round(Math.random() * 200), speed: 10 + Math.random() * 90, error: '', report: ''}));
            return ok(out);
        },
        SpeedtestProfiles: function (ids, mode) {
            record('SpeedtestProfiles', [ids, mode]);
            return ok(ids.map(id => ({id, mode, latency: 30 + Math.round(Math.random() * 200), speed: 10 + Math.random() * 90, error: '', report: ''})));
        },
        CancelSpeedtest: function () { record('CancelSpeedtest', []); return ok(); },

        // --- connections ---
        GetConnections: function () { record('GetConnections', []); return ok(connections); },
        CloseConnection: function (id) {
            record('CloseConnection', [id]);
            const i = connections.connections.findIndex(c => c.id === id);
            if (i >= 0) connections.connections.splice(i, 1);
            return ok();
        },

        // --- logs ---
        GetLogs: function () { record('GetLogs', []); return ok(logLines.map(l => l[0])); },
        LogCategories: function () { record('LogCategories', []); return ok(['core', 'app']); },
        GetCategoryLogs: function (cat) { record('GetCategoryLogs', [cat]); return ok(logLines.filter(l => l[1] === cat).map(l => l[0])); },
        ClearLogs: function () { record('ClearLogs', []); return ok(); },
        ClearCategoryLogs: function (cat) { record('ClearCategoryLogs', [cat]); return ok(); },
        ExportLogs: function (path) { record('ExportLogs', [path]); return ok(path || 'E:\\eagle\\logs.txt'); },
        GetLogDir: function () { record('GetLogDir', []); return ok('E:\\eagle\\logs'); },

        // --- rule sets ---
        RuleSets: function () { record('RuleSets', []); return ok(ruleSets.slice()); },
        RuleSetUpdates: function () { record('RuleSetUpdates', []); return ok({'geoip-cn': true, 'geosite-cn': false, 'geoip-private': false, 'category-ads-all': false}); },
        DownloadRuleSet: function (tag) { record('DownloadRuleSet', [tag]); return ok(); },
        DownloadMissingRuleSets: function () { record('DownloadMissingRuleSets', []); return ok(2); },
        RemoveRuleSet: function (tag) {
            record('RemoveRuleSet', [tag]);
            const i = ruleSets.findIndex(r => r.tag === tag);
            if (i >= 0) ruleSets.splice(i, 1);
            return ok();
        },
        ImportRuleSets: function (files, cat) { record('ImportRuleSets', [files, cat]); return ok(files.length); },
        PickRuleSetFiles: function () { record('PickRuleSetFiles', []); return ok([]); },

        // --- routing ---
        ListRoutings: function () { record('ListRoutings', []); return ok(Object.keys(routings)); },
        GetActiveRouting: function () { record('GetActiveRouting', []); return ok(state.activeRouting); },
        GetRouting: function (name) { record('GetRouting', [name]); return ok(routings[name] || routings['Default']); },
        SaveRouting: function (name, r) { record('SaveRouting', [name, r]); routings[name] = Object.assign({}, r); return ok(); },
        SetActiveRouting: function (name) {
            record('SetActiveRouting', [name]);
            state.activeRouting = name;
            setTimeout(() => emit('routing:changed', name), 0);
            return ok();
        },
        CreateRouting: function (name, preset) { record('CreateRouting', [name, preset]); routings[name] = JSON.parse(JSON.stringify(routings['Default'])); return ok(); },
        DeleteRouting: function (name) { record('DeleteRouting', [name]); delete routings[name]; return ok(); },
        ApplyRouting: function (name, r) { record('ApplyRouting', [name, r]); routings[name] = Object.assign({}, r); return ok(); },

        // --- DNS ---
        TestDNS: function (servers, remote) {
            record('TestDNS', [servers, remote]);
            return ok(servers.map((s, i) => ({server: s, ms: 10 + i * 7, err: ''})));
        },

        // --- settings ---
        GetSettings: function () { record('GetSettings', []); return ok(JSON.parse(JSON.stringify(settings))); },
        UpdateSettings: function (s) {
            record('UpdateSettings', [s]);
            Object.assign(settings, s);
            return ok();
        },
        GetAutostart: function () { record('GetAutostart', []); return ok(false); },
        SetAutostart: function (v) { record('SetAutostart', [v]); return ok(); },
        GetCorePath: function () { record('GetCorePath', []); return ok('E:\\project\\eagle-desk\\sing-box\\sing-box.exe'); },
        SetCorePath: function (p) { record('SetCorePath', [p]); return ok(); },
        PickCoreFile: function () { record('PickCoreFile', []); return ok(''); },
        GetDataDir: function () { record('GetDataDir', []); return ok('E:\\eagle'); },
        SetDataDir: function (p) { record('SetDataDir', [p]); return ok(); },
        OpenDataDir: function () { record('OpenDataDir', []); return ok(); },
        SystemFontScale: function () {
            record('SystemFontScale', []);
            // The machine's own text size. It is read from localStorage so a probe can
            // give the next launch a machine that is not at 100%: the app asks for it
            // once, at start-up, and a probe cannot change the answer afterwards.
            let v = 1;
            try {
                const raw = Number(localStorage.getItem('mock-sys-scale'));
                if (raw > 0) v = raw;
            } catch (e) { /* private mode keeps its silence */ }
            return ok(v);
        },
        CheckInboundPort: function (addr, port) { record('CheckInboundPort', [addr, port]); return ok({address: addr, port, free: false, ours: true, error: ''}); },
        FreeInboundPort: function () { record('FreeInboundPort', []); return ok(2081); },

        // --- hotkeys ---
        TestHotkey: function (k) { record('TestHotkey', [k]); return ok(true); },
        SuspendHotkeys: function () { record('SuspendHotkeys', []); return ok(); },
        ResumeHotkeys: function () { record('ResumeHotkeys', []); return ok(); },

        // --- update / about / window ---
        CheckUpdate: function () { record('CheckUpdate', []); return ok(null); },
        DownloadUpdate: function () { record('DownloadUpdate', []); return ok(); },
        GetAbout: function () {
            record('GetAbout', []);
            // The keys the About card reads (app_desktop.go GetAbout).
            return ok({
                version: 'v1.0.0',
                built: '2026-10-20',
                goVersion: 'go1.26.5',
                dataDir: 'config',
                corePath: 'sing-box\\sing-box.exe',
                dataDirAbs: 'C:\\Users\\me\\AppData\\Roaming\\Eagle',
                corePathAbs: 'C:\\app\\sing-box\\sing-box.exe',
                repo: 'sala-coding/EagleProxy',
            });
        },
        OpenURL: function (u) { record('OpenURL', [u]); state.lastURL = u; return ok(); },
        ShowWindow: function () { record('ShowWindow', []); return ok(); },
        HideWindow: function () { record('HideWindow', []); return ok(); },
        QuitApp: function () { record('QuitApp', []); return ok(); },
        ConsumeNotices: function () { record('ConsumeNotices', []); const n = state.notices.slice(); state.notices.length = 0; return ok(n); },
        ExitIP: function () { record('ExitIP', []); return ok(state.exitIP); },
        ExitAddress: function () { record('ExitAddress', []); return ok(state.exitIP); },
    };

    // Wrap every method so a failure the test armed reaches the caller as a
    // rejected promise. The call itself is logged by record() inside the method,
    // so the wrapper must not log it again — that is what made every call look
    // like two.
    Object.keys(app).forEach(name => {
        const fn = app[name];
        app[name] = function (...args) {
            try {
                return fn.apply(null, args);
            } catch (e) {
                return Promise.reject(e);
            }
        };
    });

    window.go = {main: {App: app}};

    // ---- the driver's handle on the harness --------------------------------
    window.__mock = {
        calls,
        state,
        groups,
        profiles,
        ruleSets,
        connections,
        settings,
        routings,
        emit,
        fail: function (name) { failures.add(name); },
        unfail: function (name) { failures.delete(name); },
        clearCalls: function () { calls.length = 0; },
        callsTo: function (name) { return calls.filter(c => c.name === name); },
        lastCall: function (name) {
            const l = calls.filter(c => c.name === name);
            return l.length ? l[l.length - 1] : null;
        },
    };

    console.info('[mock-bridge] ready');
})();

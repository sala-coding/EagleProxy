export namespace clash {
	
	export class Metadata {
	    network: string;
	    type: string;
	    sourceIP: string;
	    destinationIP: string;
	    sourcePort: string;
	    destinationPort: string;
	    host: string;
	    processPath: string;
	
	    static createFrom(source: any = {}) {
	        return new Metadata(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.network = source["network"];
	        this.type = source["type"];
	        this.sourceIP = source["sourceIP"];
	        this.destinationIP = source["destinationIP"];
	        this.sourcePort = source["sourcePort"];
	        this.destinationPort = source["destinationPort"];
	        this.host = source["host"];
	        this.processPath = source["processPath"];
	    }
	}
	export class Connection {
	    id: string;
	    upload: number;
	    download: number;
	    start: string;
	    chains: string[];
	    rule: string;
	    rulePayload: string;
	    metadata: Metadata;
	
	    static createFrom(source: any = {}) {
	        return new Connection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.upload = source["upload"];
	        this.download = source["download"];
	        this.start = source["start"];
	        this.chains = source["chains"];
	        this.rule = source["rule"];
	        this.rulePayload = source["rulePayload"];
	        this.metadata = this.convertValues(source["metadata"], Metadata);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ConnectionsResp {
	    downloadTotal: number;
	    uploadTotal: number;
	    connections: Connection[];
	
	    static createFrom(source: any = {}) {
	        return new ConnectionsResp(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.downloadTotal = source["downloadTotal"];
	        this.uploadTotal = source["uploadTotal"];
	        this.connections = this.convertValues(source["connections"], Connection);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace db {
	
	export class Group {
	    id: number;
	    front_proxy_id: number;
	    archive: boolean;
	    name: string;
	    order: number[];
	    url: string;
	    info: string;
	    lastup: number;
	    sub_interval: number;
	    manually_column_width: boolean;
	    column_width: number[];
	
	    static createFrom(source: any = {}) {
	        return new Group(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.front_proxy_id = source["front_proxy_id"];
	        this.archive = source["archive"];
	        this.name = source["name"];
	        this.order = source["order"];
	        this.url = source["url"];
	        this.info = source["info"];
	        this.lastup = source["lastup"];
	        this.sub_interval = source["sub_interval"];
	        this.manually_column_width = source["manually_column_width"];
	        this.column_width = source["column_width"];
	    }
	}

}

export namespace dnstest {
	
	export class Result {
	    server: string;
	    ms: number;
	    err?: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.server = source["server"];
	        this.ms = source["ms"];
	        this.err = source["err"];
	    }
	}

}

export namespace main {
	
	export class GroupInfo {
	    id: number;
	    name: string;
	    archive: boolean;
	    profileCount: number;
	    url: string;
	    subLastUpdate: number;
	    info: string;
	
	    static createFrom(source: any = {}) {
	        return new GroupInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.archive = source["archive"];
	        this.profileCount = source["profileCount"];
	        this.url = source["url"];
	        this.subLastUpdate = source["subLastUpdate"];
	        this.info = source["info"];
	    }
	}
	export class InboundPortStatus {
	    address: string;
	    port: number;
	    free: boolean;
	    ours: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new InboundPortStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.address = source["address"];
	        this.port = source["port"];
	        this.free = source["free"];
	        this.ours = source["ours"];
	        this.error = source["error"];
	    }
	}
	export class ProfileInfo {
	    id: number;
	    gid: number;
	    type: string;
	    name: string;
	    address: string;
	    latency: number;
	    report: string;
	    speed: number;
	    up: number;
	    down: number;
	    port: number;
	    network: string;
	    security: string;
	    tlsNote: string;
	    group: string;
	
	    static createFrom(source: any = {}) {
	        return new ProfileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.gid = source["gid"];
	        this.type = source["type"];
	        this.name = source["name"];
	        this.address = source["address"];
	        this.latency = source["latency"];
	        this.report = source["report"];
	        this.speed = source["speed"];
	        this.up = source["up"];
	        this.down = source["down"];
	        this.port = source["port"];
	        this.network = source["network"];
	        this.security = source["security"];
	        this.tlsNote = source["tlsNote"];
	        this.group = source["group"];
	    }
	}
	export class RuleSetInfo {
	    tag: string;
	    url: string;
	    needed: boolean;
	    local: boolean;
	    size: number;
	    modified: string;
	    country: string;
	
	    static createFrom(source: any = {}) {
	        return new RuleSetInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tag = source["tag"];
	        this.url = source["url"];
	        this.needed = source["needed"];
	        this.local = source["local"];
	        this.size = source["size"];
	        this.modified = source["modified"];
	        this.country = source["country"];
	    }
	}
	export class SpeedTestResult {
	    id: number;
	    mode: number;
	    latency: number;
	    speed: number;
	    error: string;
	    report: string;
	
	    static createFrom(source: any = {}) {
	        return new SpeedTestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.mode = source["mode"];
	        this.latency = source["latency"];
	        this.speed = source["speed"];
	        this.error = source["error"];
	        this.report = source["report"];
	    }
	}

}

export namespace setting {
	
	export class ExtraCore {
	    core_map: string;
	
	    static createFrom(source: any = {}) {
	        return new ExtraCore(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.core_map = source["core_map"];
	    }
	}
	export class InboundAuth {
	    user: string;
	    pass: string;
	
	    static createFrom(source: any = {}) {
	        return new InboundAuth(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.user = source["user"];
	        this.pass = source["pass"];
	    }
	}
	export class Routing {
	    direct_ip?: string;
	    direct_domain?: string;
	    proxy_ip?: string;
	    proxy_domain?: string;
	    block_ip?: string;
	    block_domain?: string;
	    def_outbound?: string;
	    custom?: string;
	    remote_dns?: string;
	    remote_dns_strategy?: string;
	    direct_dns?: string;
	    direct_dns_strategy?: string;
	    domain_strategy?: string;
	    outbound_domain_strategy?: string;
	    dns_routing: boolean;
	    sniffing_mode: number;
	    use_dns_object: boolean;
	    dns_object?: string;
	    dns_final_out?: string;
	    dns_smart_select: boolean;
	    dns_smart_minutes: number;
	    dns_smart_switch: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Routing(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.direct_ip = source["direct_ip"];
	        this.direct_domain = source["direct_domain"];
	        this.proxy_ip = source["proxy_ip"];
	        this.proxy_domain = source["proxy_domain"];
	        this.block_ip = source["block_ip"];
	        this.block_domain = source["block_domain"];
	        this.def_outbound = source["def_outbound"];
	        this.custom = source["custom"];
	        this.remote_dns = source["remote_dns"];
	        this.remote_dns_strategy = source["remote_dns_strategy"];
	        this.direct_dns = source["direct_dns"];
	        this.direct_dns_strategy = source["direct_dns_strategy"];
	        this.domain_strategy = source["domain_strategy"];
	        this.outbound_domain_strategy = source["outbound_domain_strategy"];
	        this.dns_routing = source["dns_routing"];
	        this.sniffing_mode = source["sniffing_mode"];
	        this.use_dns_object = source["use_dns_object"];
	        this.dns_object = source["dns_object"];
	        this.dns_final_out = source["dns_final_out"];
	        this.dns_smart_select = source["dns_smart_select"];
	        this.dns_smart_minutes = source["dns_smart_minutes"];
	        this.dns_smart_switch = source["dns_smart_switch"];
	    }
	}
	export class Setting {
	    extraCore?: ExtraCore;
	    inbound_auth?: InboundAuth;
	    user_agent2?: string;
	    test_url?: string;
	    test_url_dl?: string;
	    test_dl_timeout: number;
	    current_group: number;
	    inbound_address?: string;
	    inbound_socks_port: number;
	    log_level?: string;
	    mux_protocol?: string;
	    mux_concurrency: number;
	    mux_padding: boolean;
	    mux_default_on: boolean;
	    traffic_loop_interval: number;
	    test_concurrent: number;
	    theme?: string;
	    theme_schema?: string;
	    custom_inbound?: string;
	    custom_route?: string;
	    sub_use_proxy: boolean;
	    remember_id: number;
	    remember_enable: boolean;
	    language: number;
	    spmode2?: string[];
	    remember_spmode: boolean;
	    skip_cert: boolean;
	    hk_mw?: string;
	    hk_group?: string;
	    hk_route?: string;
	    hk_spmenu?: string;
	    hk_toggle_node?: string;
	    hk_egress?: string;
	    hk_toggle_tun?: string;
	    hk_next_node?: string;
	    hk_restart_node?: string;
	    hk_connections?: string;
	    hk_logs?: string;
	    hk_datadir?: string;
	    fakedns: boolean;
	    active_routing?: string;
	    egress_mode?: string;
	    mw_size?: string;
	    conn_stat: boolean;
	    vpn_impl: number;
	    vpn_mtu: number;
	    vpn_ipv6: boolean;
	    ipv6_auto: boolean;
	    vpn_hide_console: boolean;
	    vpn_strict_route: boolean;
	    vpn_bypass_process?: string;
	    vpn_bypass_cidr?: string;
	    vpn_rule_white: boolean;
	    check_include_pre: boolean;
	    sp_format?: string;
	    sp_bypass?: string;
	    sp_pac?: string;
	    sub_clear: boolean;
	    sub_insecure: boolean;
	    sub_auto_update: number;
	    log_ignore?: string[];
	    start_minimal: boolean;
	    max_log_line: number;
	    splitter_state?: string;
	    utlsFingerprint?: string;
	    core_box_clash_api: number;
	    core_box_clash_api_secret?: string;
	    core_box_underlying_dns?: string;
	    core_box_cache_file: boolean;
	    vpn_internal_tun: boolean;
	    pending_tun: boolean;
	    pending_tun_profile: number;
	    keep_tun_front: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Setting(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.extraCore = this.convertValues(source["extraCore"], ExtraCore);
	        this.inbound_auth = this.convertValues(source["inbound_auth"], InboundAuth);
	        this.user_agent2 = source["user_agent2"];
	        this.test_url = source["test_url"];
	        this.test_url_dl = source["test_url_dl"];
	        this.test_dl_timeout = source["test_dl_timeout"];
	        this.current_group = source["current_group"];
	        this.inbound_address = source["inbound_address"];
	        this.inbound_socks_port = source["inbound_socks_port"];
	        this.log_level = source["log_level"];
	        this.mux_protocol = source["mux_protocol"];
	        this.mux_concurrency = source["mux_concurrency"];
	        this.mux_padding = source["mux_padding"];
	        this.mux_default_on = source["mux_default_on"];
	        this.traffic_loop_interval = source["traffic_loop_interval"];
	        this.test_concurrent = source["test_concurrent"];
	        this.theme = source["theme"];
	        this.theme_schema = source["theme_schema"];
	        this.custom_inbound = source["custom_inbound"];
	        this.custom_route = source["custom_route"];
	        this.sub_use_proxy = source["sub_use_proxy"];
	        this.remember_id = source["remember_id"];
	        this.remember_enable = source["remember_enable"];
	        this.language = source["language"];
	        this.spmode2 = source["spmode2"];
	        this.remember_spmode = source["remember_spmode"];
	        this.skip_cert = source["skip_cert"];
	        this.hk_mw = source["hk_mw"];
	        this.hk_group = source["hk_group"];
	        this.hk_route = source["hk_route"];
	        this.hk_spmenu = source["hk_spmenu"];
	        this.hk_toggle_node = source["hk_toggle_node"];
	        this.hk_egress = source["hk_egress"];
	        this.hk_toggle_tun = source["hk_toggle_tun"];
	        this.hk_next_node = source["hk_next_node"];
	        this.hk_restart_node = source["hk_restart_node"];
	        this.hk_connections = source["hk_connections"];
	        this.hk_logs = source["hk_logs"];
	        this.hk_datadir = source["hk_datadir"];
	        this.fakedns = source["fakedns"];
	        this.active_routing = source["active_routing"];
	        this.egress_mode = source["egress_mode"];
	        this.mw_size = source["mw_size"];
	        this.conn_stat = source["conn_stat"];
	        this.vpn_impl = source["vpn_impl"];
	        this.vpn_mtu = source["vpn_mtu"];
	        this.vpn_ipv6 = source["vpn_ipv6"];
	        this.ipv6_auto = source["ipv6_auto"];
	        this.vpn_hide_console = source["vpn_hide_console"];
	        this.vpn_strict_route = source["vpn_strict_route"];
	        this.vpn_bypass_process = source["vpn_bypass_process"];
	        this.vpn_bypass_cidr = source["vpn_bypass_cidr"];
	        this.vpn_rule_white = source["vpn_rule_white"];
	        this.check_include_pre = source["check_include_pre"];
	        this.sp_format = source["sp_format"];
	        this.sp_bypass = source["sp_bypass"];
	        this.sp_pac = source["sp_pac"];
	        this.sub_clear = source["sub_clear"];
	        this.sub_insecure = source["sub_insecure"];
	        this.sub_auto_update = source["sub_auto_update"];
	        this.log_ignore = source["log_ignore"];
	        this.start_minimal = source["start_minimal"];
	        this.max_log_line = source["max_log_line"];
	        this.splitter_state = source["splitter_state"];
	        this.utlsFingerprint = source["utlsFingerprint"];
	        this.core_box_clash_api = source["core_box_clash_api"];
	        this.core_box_clash_api_secret = source["core_box_clash_api_secret"];
	        this.core_box_underlying_dns = source["core_box_underlying_dns"];
	        this.core_box_cache_file = source["core_box_cache_file"];
	        this.vpn_internal_tun = source["vpn_internal_tun"];
	        this.pending_tun = source["pending_tun"];
	        this.pending_tun_profile = source["pending_tun_profile"];
	        this.keep_tun_front = source["keep_tun_front"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace sub {
	
	export class Result {
	    gid: number;
	    added: string[];
	    deleted: string[];
	    skipped: string[];
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.gid = source["gid"];
	        this.added = source["added"];
	        this.deleted = source["deleted"];
	        this.skipped = source["skipped"];
	        this.message = source["message"];
	    }
	}

}

export namespace update {
	
	export class Asset {
	    name: string;
	    browser_download_url: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new Asset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.browser_download_url = source["browser_download_url"];
	        this.size = source["size"];
	    }
	}
	export class Release {
	    tag_name: string;
	    name: string;
	    html_url: string;
	    body: string;
	    prerelease: boolean;
	    published_at: string;
	    assets: Asset[];
	
	    static createFrom(source: any = {}) {
	        return new Release(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tag_name = source["tag_name"];
	        this.name = source["name"];
	        this.html_url = source["html_url"];
	        this.body = source["body"];
	        this.prerelease = source["prerelease"];
	        this.published_at = source["published_at"];
	        this.assets = this.convertValues(source["assets"], Asset);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}


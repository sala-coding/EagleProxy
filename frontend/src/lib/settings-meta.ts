import {codeOfLang, langFromCode, systemLang, type Lang} from "../i18n";

// The raw settings object is one flat list of every key the file holds — which is what
// the file is, and not what a reader is looking for: sixty rows in file order, with the
// inbound port above the theme and the log level three rows from the subscription
// interval. These are the subjects it actually contains, in the order they are met in
// use.
//
// Keys that already have a card of their own are deliberately absent: the TUN card owns
// vpn_internal_tun / vpn_mtu / vpn_ipv6 / ipv6_auto / vpn_strict_route / vpn_hide_console / fakedns,
// the core-and-logs card owns the directories and the external-core paths (the old
// paths card's controls, moved in whole), and the routing card owns active_routing.
// Editing them in two places is how the two come to disagree.
export const SETTING_GROUPS: {title: string; keys: string[]; note?: string}[] = [
    {
        title: 'settings.group.inbound',
        // inbound_address and inbound_socks_port belong to the port card, which is where
        // they are edited; listing them here as well would be the same two rows twice.
        // See SETTING_HIDDEN.
        keys: ['inbound_auth',
            'custom_inbound', 'sp_format', 'sp_bypass', 'sp_pac',
            'mux_protocol', 'mux_concurrency', 'mux_padding', 'mux_default_on', 'skip_cert'],
    },
    {
        title: 'settings.group.subs',
        keys: ['sub_use_proxy', 'sub_clear', 'sub_insecure', 'sub_auto_update',
            'user_agent2'],
    },
    {
        title: 'settings.group.core',
        keys: ['core_box_clash_api', 'core_box_clash_api_secret', 'core_box_underlying_dns',
            'utlsFingerprint', 'log_level', 'max_log_line', 'log_ignore'],
    },
    {
        title: 'settings.group.test',
        keys: ['test_dl_timeout', 'test_concurrent',
            'traffic_loop_interval'],
    },
    {
        title: 'settings.group.ui',
        // remember_spmode and spmode2 are the switch and the thing it will put back, so
        // they are shown together: a reader who wants a mode to stop coming back should
        // be able to see which one is written down — and clearing that list is the other
        // way to stop it.
        // theme and language are deliberately not here: the startup card draws both (the
        // palette bars and the two language pills), and a key listed in two places is one
        // setting offered twice — whichever the reader touched last would win. See
        // SETTING_HIDDEN for where the two went instead.
        //
        // The rows are drawn on that same startup card rather than on a card of this
        // group's own: see GROUP_IN_STARTUP below, which is the title this entry is
        // looked up by.
        keys: ['conn_stat', 'start_minimal', 'remember_enable',
            'remember_spmode', 'spmode2',
            'check_include_pre'],
    },
    {
        title: 'settings.group.hotkeys',
        // A card of their own rather than the tail of the one above: key names at the end
        // of a list of window preferences read as more of them. Within the card the rows
        // are grouped by what they act on — the window, the two ways traffic is taken
        // over, the node, the two panels this side opens, and the three that step
        // something on — so the cycling keys sit together instead of either side of the
        // node switches.
        //
        // The note is the only card using one: a hotkey is captured rather than typed
        // (see HotkeyInput in JsonForm), and the rules for capturing one are the same for
        // all twelve rows, so they belong above them rather than repeated under each.
        note: 'hotkeys.note',
        keys: ['hk_mw',
            'hk_spmenu', 'hk_toggle_tun',
            'hk_toggle_node', 'hk_next_node', 'hk_restart_node',
            'hk_connections', 'hk_logs',
            'hk_group', 'hk_route', 'hk_egress',
            'hk_datadir'],
    },
];

// The legends each raw card names its two frames by — the card's own title already says
// what the page holds, and "文本框"/"开关" repeated under it would name the kind of row
// rather than the subject the rows share, which is exactly what a frame exists to say.
// Translated where they are drawn (see framesOf near jsonSlides), so a language change
// reads the page like any other word on it. A frame a card does not name keeps the
// kind's own word (see `frames` in JsonForm).
export const FRAME_KEYS: Record<string, {text?: string; switch?: string}> = {
    ['settings.group.inbound']: {text: 'kind.inbound.text', switch: 'kind.inbound.switch'},
    ['settings.group.subs']: {text: 'kind.subs.text', switch: 'kind.subs.switch'},
    ['settings.group.core']: {text: 'kind.core.text'},
    ['settings.group.test']: {text: 'kind.test.text'},
    ['settings.group.hotkeys']: {text: 'kind.hotkeys.text'},
};

// The one group in that table that is not drawn as a card of its own: 界面 belongs on the
// 启动与界面 card, which is the card that asks the same question — what the window is
// like, and what it does on the way up — and a card named for both subjects with half of
// one of them standing somewhere else is what the reader was looking at. The table above
// still names the subject, so its keys are still read from the one place they are written
// down; only the card they land on changes (see jsonSlides, and where the startup card
// renders).
export const GROUP_IN_STARTUP = 'settings.group.ui';

// The keys that do not get a row in the last card. Two kinds belong here: the ones the
// app keeps for itself — a mode it was in, a size it remembers, the request that survives
// an elevated relaunch, the node it last started — and the ones that already have a
// control of their own somewhere else in this dialog (theme has the palette in the
// startup card, active_routing, egress_mode and sniffing_mode have the routing card,
// custom_route has the routing profile's own custom rules behind the routing card's advanced
// section, and the vpn_bypass_* / vpn_rule_white rows together with core_box_cache_file have
// the TUN card — the last of them because it is what keeps the FakeIP mapping). A key of the
// second kind listed in a group as well would be one setting offered twice, and whichever the
// reader touched last would be the one that stuck.
//
// Hiding a key is not dropping it: the builder still reads every one of these (see
// builder.go, and nekoray's own ConfigBuilder, which merges the same two custom-rule
// sources), so a file that already carries rules there keeps applying them. Only the row
// is gone.
//
// Anything not named here and not in a group lands in the last card instead of being
// dropped, so a key added on the Go side shows up without anyone having to remember this
// table.
export const SETTING_HIDDEN = new Set([
    'extraCore', 'pending_tun', 'pending_tun_profile', 'current_group', 'theme_schema',
    'test_url', 'test_url_dl',
    'active_routing', 'mw_size', 'splitter_state', 'remember_id',
    'theme', 'language', 'inbound_address', 'inbound_socks_port', 'custom_route', 'egress_mode',
    'vpn_internal_tun', 'vpn_impl', 'vpn_mtu', 'vpn_ipv6', 'ipv6_auto', 'vpn_strict_route', 'fakedns',
    'vpn_hide_console', 'vpn_rule_white', 'vpn_bypass_process', 'vpn_bypass_cidr',
    'core_box_cache_file', 'sniffing_mode',
]);

// The languages the interface is written in, as the settings file counts them (see
// langFromCode in i18n.ts): the numbers are what the file holds, so they are the values
// here and only the names are looked up. Each is named in its own language, and every
// dictionary spells it the same way — a language is the one label that must not be
// translated, because the reader looking for it is the reader who cannot read the rest of
// the interface. One file per language sits behind them (see ./i18n/), so a word the
// reader meets is a word of the language they chose rather than the English one.
// The flag beside each name is the country the language is spoken in, drawn from the same
// flag-icons set the node chips use (see fa/icon), one file per country: a pill wearing
// the flag says which language it is to a reader who cannot yet read the name.
export const LANGUAGES = [
    {value: '0', label: 'lang.zh', flag: 'cn'},
    {value: '1', label: 'lang.en', flag: 'us'},
    {value: '4', label: 'lang.ru', flag: 'ru'},
];

// The same flag by the language rather than by the file's number, for the one place
// that wears the language it is speaking rather than offering a choice: the mark in
// the window's own corner (see the status bar's left end), which changes with the
// language as the reader changes it. Read from LANGUAGES rather than written out, so
// a language added above is a flag the corner wears with no line changed here.
export const LANG_FLAG: Record<Lang, string> = LANGUAGES.reduce((m, l) => {
    m[langFromCode(Number(l.value))] = l.flag;
    return m;
}, {} as Record<Lang, string>);

// Which language the window wears at start-up, from what the settings file holds.
//
// A language the reader has chosen here is the one that wins, and it is remembered
// outside the settings file for two reasons: the choice is worn the moment it is
// made (see applyLanguage) whether or not 保存 is ever pressed, so writing it only
// on save would lose a choice that was acted on; and the file's own zero value
// cannot say "nobody has chosen" — Chinese is a real choice and also what a file
// that has never been written holds. The mark is therefore a plain flag beside the
// number, and it is what makes the two tellable apart.
//
// With no choice remembered, the machine answers: what the system speaks, if this
// app has a dictionary for it, and Chinese otherwise (see SystemLang, which is
// where the fallback is decided so the two paths agree on it).
export const startLanguage = (fileCode: unknown): number => {
    const chosen = localStorage.getItem('lang-chosen');
    if (chosen != null) return Number(chosen) || 0;
    // A file written by an older build has no mark — it was the only place a
    // language was ever kept — so a language in it is read as the reader's choice,
    // and only a file still holding the zero every new build starts from is read as
    // "never chosen" and handed to the machine.
    const code = Number(fileCode);
    if (Number.isFinite(code) && code !== 0) return code;
    return codeOfLang(systemLang());
};

// Set when the reader picks a language in the settings card, so the next start-up
// wears that one instead of asking the machine again. Keyed beside the number rather
// than inside it, because the number is the file's contract (see LANGUAGES) and this
// is the front end's own note that it has been given a value on purpose.
export const rememberLanguage = (code: number) => localStorage.setItem('lang-chosen', String(code));

// The one control in a card of short values that is not short: it keeps the whole row
// under the cells rather than being squeezed into one of them. A textarea folded into a
// quarter of the dialog is a column of wrapped half-lines.
export const GROUP_FULL_ROW: Record<string, string[]> = {
    'settings.group.subs': ['user_agent2'],
};

// The settings sheet's sections, in the order its nav lists them and its carousel lays
// them out: the hand-written cards first, then the raw-settings groups, whose titles
// SETTING_GROUPS names. Only the hand-written ones are listed here — they are not part
// of SETTING_GROUPS at all, and the nav needs one entry per slide either way. The two
// lists are zipped by index where the sheet renders, so the order here is the order on
// screen. The paths card is not among them any more: where the core and the data live
// belongs with the core's own settings, and its controls open the 核心与日志 card,
// above that card's JSON rows (see jsonGroups where it renders).
export const SETTING_SECTIONS = [
    'settings.group.startup',
    'settings.group.tun',
    'settings.group.routing',
    'settings.group.dns',
    'settings.group.rulesets',
];

// Where the routing card sits in that list. The rail's routing button opens the sheet
// for that card, which is now a slide to land on rather than a place to scroll to.
export const ROUTING_SLIDE = SETTING_SECTIONS.indexOf('settings.group.routing');

// How long the pointer has to hold a subscription card before the node table follows
// it, in milliseconds. This is a dwell and not a debounce: three seconds is far longer
// than a sweep down the strip takes to cross a card, so a pointer on its way somewhere
// else switches nothing at all, and the subscription that ends up current is the one
// the pointer actually rested on. The wait is only half of the rule — the card also has
// to still be under the pointer when the wait ends (see hoverSwitch) — so a hover
// abandoned part way through leaves the table exactly where it was.
export const HOVER_SWITCH_DELAY = 3000;

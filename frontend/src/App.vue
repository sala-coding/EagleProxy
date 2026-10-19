<script setup lang="ts">
// App.vue — the Vue 3 port of the React App.tsx (the whole application shell).
// The DOM structure, the classes, the copy and the behaviour are the original's;
// the logic is split into composables (src/composables) and template sections
// (src/sections), each named after the block of App.tsx it came from. The
// original comments are kept at their corresponding positions.
import {computed, onBeforeUnmount, onMounted, ref, watch, type CSSProperties} from 'vue';
import './App.css';
import * as API from "../wailsjs/go/main/App";
import {main, setting} from "../wailsjs/go/models";
import {EventsOn} from "../wailsjs/runtime/runtime";
import {t, langFromCode, setLang} from "./i18n";
import {loadTips, tip} from "./tips";
import {errText} from "./lib/shared";
import {FILTER_ICONS, nowStamp} from "./lib/logfilter";
import {hueFor, metricGroup, MODE_HAS_LATENCY, MODE_HAS_SPEED, SPEED_MODE_KEYS, PROTOCOLS} from "./lib/metrics";
import {STACK_DEPTH, STACK_STEP, STACK_TILT} from "./lib/stack";
import {clampScale, CUSTOM_VARS, LIGHT_THEMES, THEMES, THEME_REMAP, UI_SCALE_MAX, UI_SCALE_MIN, wearCustom} from "./lib/theme";
import {startLanguage} from "./lib/settings-meta";
import {SMART_PICK_MINUTES, SMART_TICK_MS, pickFastestDNS, roundChoice} from "./lib/dns";
import {warmSettingsCache} from "./components/SettingsDialog.vue";
import SettingsDialog from "./components/SettingsDialog.vue";
import Modal from "./components/Modal.vue";
import EditorDialog from "./components/EditorDialog.vue";
import ConnectionsDialog from "./components/ConnectionsDialog.vue";
import {type Dict} from "./lib/metrics";
import {type PanelExit} from "./lib/subdraw";
import {FolderFilled, AddFilled, RightFilled, PlayFilled, StopFilled, TransferHorizontalFilled,
    DashboardFilled, FlashFilled, FileImportFilled, Edit2Filled, CopyFilled, Link2Filled,
    Link3Filled, CodeFilled, FileExportFilled, QrcodeFilled, EraserFilled, Delete2Filled,
    Download3Filled} from "./lib/icons";
import {STOPPED} from "./composables/shared-consts";
import {useStatusNote} from "./composables/useStatusNote";
import {useLogs} from "./composables/useLogs";
import {useConsoleDrag} from "./composables/useConsoleDrag";
import {useSubsPanel} from "./composables/useSubsPanel";
import {useSubsDrawer} from "./composables/useSubsDrawer";
import {useReelStack} from "./composables/useReelStack";
import {useColumnWidths} from "./composables/useColumnWidths";
import {useExitIP} from "./composables/useExitIP";
import {useActions} from "./composables/useActions";
import ProfileRow from "./sections/ProfileRow.vue";
import NavRail from "./sections/NavRail.vue";
import SubsDrawer from "./sections/SubsDrawer.vue";
import LogPanel from "./sections/LogPanel.vue";
import StatusDock from "./sections/StatusDock.vue";
import ReelTable from "./components/ReelTable.vue";

const {statusNote, showStatusNote, hideStatusNote, holdStatusNote, notify} = useStatusNote();

const groupsChangedSeq = {current: 0};
const groups = ref<main.GroupInfo[]>([]);
const gid = ref<number>(-1);
const profiles = ref<main.ProfileInfo[]>([]);
// Bumped whenever the backend says a group's members were rewritten (a subscription
// update). The table's own list is re-read by that event's handler; the cross-group
// list the search box builds is not, and it is the same hazard — rows holding ids
// that no longer name anything, whose double click answers "该节点已不在列表中".
const profilesEpoch = ref(0);
const selected = ref<number>(-1);
const running = ref<number>(STOPPED);
// The same value for the event handlers, which are installed once: the id a
// core:exit carries has to be judged against the node running NOW, not against
// the one that was running when the listener was built.
const runningIdRef = ref(running.value);
watch(running, v => {
    runningIdRef.value = v;
});
// Every write to the running id goes through here, not only the commit-time sync
// above: a core:exit can arrive between a start's setRunning and the commit that
// would update the ref, and the exit guard reads that ref — it judged the new id
// against the old one, took the exit for "the current node died" and wiped the
// window back to STOPPED while the node was running.
const applyRunning = (id: number) => {
    runningIdRef.value = id;
    running.value = id;
};

const busy = ref(false);
// The same flag mirrored in a ref: the re-entry guards inside start/stop/switchTo
// are read straight from the handler, before any render can publish fresh state,
// so a double-click in the same tick is still caught.
const busyRef = ref(false);
const setBusy = (v: boolean) => {
    busyRef.value = v;
    busy.value = v;
};

const L = useLogs(notify);
const {
    logs, showLogs, logFilter, logLevel, logPaused, shownLogs, catLive, logCtx,
    logRef, onLogScroll,
    clearLogs, copyLogs, addLogLineToRoute, exportLogs,
    queueLogLine, drainLogs, flushPendingLogs,
    loadFileCats, loadAllLogs, toggleLogs,
} = L;

const C = useConsoleDrag(showLogs);
const {consoleH, consoleDragging, consoleNow, consoleRef,
    onConsoleGripDown, onConsoleGripMove, onConsoleGripUp, onConsoleGripKey, resetConsoleH} = C;

const connOpen = ref(false);
// How the connections window has been asked to leave, if it has: 'flip', the door
// swing back, when the reader dismissed it. It stays mounted until the swing has
// finished; without this the entry button could only open it, and a second click
// had nothing to say. Being displaced by the drawer is not a kind: it goes at once.
const connExit = ref<PanelExit | null>(null);
const spMode = ref<{ vpn: boolean; systemProxy: boolean }>({vpn: false, systemProxy: false});
// Which of the two switches has a press still out. TUN answers slowly — the core
// is stopped and restarted, and with the internal driver the whole app goes
// through UAC and comes back as a new process — so a second press while the
// first is unanswered would ask for another stop/start on top of the one already
// running. See toggleSP.
const spBusy = ref<'vpn' | 'systemProxy' | null>(null);
// The same value in a ref, for the reason busyRef exists: the guard is read from
// the handler before any render can publish the new state, so two presses in one
// tick both got past a state that had not been written yet — and this one waits
// on UAC, which is exactly where an impatient second press lands.
const spBusyRef = ref(false);
const setSpBusyBoth = (v: 'vpn' | 'systemProxy' | null) => {
    spBusyRef.value = v !== null;
    spBusy.value = v;
};
const theme = ref<string>('dark');
// How traffic leaves — rules, direct, global (see EGRESS_MODES) — and the status
// bar's menu for it. Held here rather than in the settings dialog because the bar
// has to show it with no dialog open, and refetched whenever the settings are
// saved (see refreshEgress).
const egress = ref('rule');
const egressOpen = ref(false);
const egressRef = ref<HTMLSpanElement | null>(null);
// The batch menu above the node table, and the button it hangs off. Declared up
// here with the other menus' state because the listener that closes it on an
// outside press lives near the top of the component, and a `const` cannot be read
// above the line that declares it.
const batchOpen = ref(false);
const batchRef = ref<HTMLSpanElement | null>(null);

const D = useSubsDrawer({connOpen, connExit, groups});
const {subsDrop, subsClosing, reelReady, openSubs, closeSubs, hardCloseSubs, subsBtnRef} = D;

const P = useSubsPanel(subsDrop);
const {panelWUser, panelW, panelResizing, panelRef, nameCols,
    onPanelGripDown, onPanelGripMove, onPanelGripUp, onPanelGripKey, resetPanelW} = P;

const R = useReelStack({groups, gid, subsDrop, panelRef});
const {stack, reelRef, stackRef, reelOn, dropReel, peekReel, trackReel} = R;

// The current subscription's nodes are already in hand, and they are read through a ref
// rather than depended on: the traffic poll rewrites `profiles` every second and a half,
// and a seed that changed its identity every poll would remount the face it seeds.
const profilesRef = ref(profiles.value);
watch(profiles, v => {
    profilesRef.value = v;
});

// Bumped only on a deliberate tab click. The tbody is keyed by this counter
// rather than by gid, because gid also changes while the pointer merely
// sweeps across the strip: keying on gid remounted the tbody on hover *and*
// again on hover-out, replaying the entrance animation twice per pass.
const listKey = ref(0);

const tableRef = ref<HTMLTableElement | null>(null);
const W = useColumnWidths({tableRef, profiles, listKey, gid});
void W;

const E = useExitIP(running);
const {exitIP, exitTick, flagMissing, infoOpen, infoRef, nodeIpRef} = E;

// The reader's own offset on the type scale, remembered the way the drawer's width is
// — localStorage, written and applied the moment it is chosen — rather than through
// the settings file: this is a preference of the window, and waiting for 保存 to see
// what a size looks like would defeat the point of picking it by eye.
// Guarded on read, because a stale or hand-edited number outside the range would
// leave the app unreadably small or large with no way back through the UI.
// The window's type scale. 0 is not a size: it is "not written down and not
// yet read from the system", and the effect below resolves it once — to the
// system's own text size, so a fresh install opens at the size the machine
// already chose, not at this app's guess of it — and from then on the value
// in localStorage is the one the reader set, which is kept as it is.
const savedScale = Number(window.localStorage.getItem('ui-scale'));
const uiScale = ref(savedScale >= UI_SCALE_MIN && savedScale <= UI_SCALE_MAX ? savedScale : 0);
// The system's answer, held so the slider's double-click "default" can mean
// the same thing this session's first run meant: the machine's own size.
const sysScale = ref(1);
watch(uiScale, scale => {
    if (scale === 0) {
        API.SystemFontScale().then(v => {
            const sys = v > 0 ? clampScale(v) : 1;
            uiScale.value = sys;
            sysScale.value = sys;
        }).catch(() => {
            uiScale.value = 1;
            sysScale.value = 1;
        });
        return;
    }
    document.documentElement.style.setProperty('--ui-scale', String(scale));
    window.localStorage.setItem('ui-scale', String(scale));
}, {immediate: true});

// The language tick: t() reads a module-level table, which Vue's reactivity cannot
// see, so a bump here (and the prop of the same name on every section) is what
// re-renders the window in the new language — the same job setLangTick did.
const langTick = ref(0);
const ctx = ref<{ x: number; y: number; id: number } | null>(null);
const ctxRef = ref<HTMLDivElement | null>(null);
// The log's menu is a second instance of the same panel and needs the same placement
// rule — it used to carry its own arithmetic, which is how it ended up over the bar.
const logCtxRef = ref<HTMLDivElement | null>(null);
// Submenus in the node menu. One panel serves all of them, so a row that
// owns one is described by data rather than by state of its own: `sub` is
// the open row's key, and the row's element is handed over when it opens so
// the panel can be docked against that row instead of the pointer.
const sub = ref<string | null>(null);
const subRowEl = ref<HTMLElement | null>(null);
const subPanelEl = ref<HTMLDivElement | null>(null);
// Closing waits for the pointer to leave both the row and the panel: the gap
// between them is crossed on every pass in and out, and a submenu that
// vanished in the gap would have to be aimed at twice.
const subTimer = ref<number | null>(null);

// dialogs
const importOpen = ref(false);
const importText = ref('');
const editor = ref<{ title: string; initial: Dict; onSave: (v: Dict) => Promise<void>; onDiscard?: () => Promise<void> } | null>(null);
// The settings dialog, and which of its cards the caller came for: the rail's gear
// asks for the form, its routing button for the routing card inside it — that card
// is the routing window now, and it is scrolled to rather than hunted for.
const settingsOpen = ref<null | 'plain' | 'routing'>(null);

// The dock covers the left end of the window, and the panels that live over the
// table — the console, the connections window, the subscriptions drawer — share
// that ground with it. When it rises they go home: not dismissed, sent back, and
// each by the exit it already owns — the console slides out the way it came, the
// two seat-sharers flip back on their hinge — so the stage clears while the dock
// is up and every panel returns intact when it is gone. Dependencies are
// deliberately just the one state: this reads what the panels ARE at the moment
// the dock opens, the way the open* functions do (see "No dependency list" below).
watch(settingsOpen, s => {
    if (!s) return;
    if (showLogs.value) showLogs.value = false;
    if (connOpen.value) connExit.value = 'flip';
    if (subsDrop.value) subsClosing.value = 'flip';
    egressOpen.value = false;
    batchOpen.value = false;
    infoOpen.value = false;
    aboutOpen.value = false;
    importOpen.value = false;
    editor.value = null;
    previewOpen.value = false;
    qrOpen.value = false;
    moveOpen.value = false;
    groupEditGid.value = null;
    gConfirm.value = null;
    ctx.value = null;
    logCtx.value = null;
});

// The box the settings panel is placed in: the node table's own, which it stands on
// rather than replacing (the table is not turned away any more — see "no page turn"
// in App.css). Measured rather than written out as four numbers, because that box
// moves: the drawer takes its left edge, the log panel its height, a resized window
// all of it.
const tableWrapRef = ref<HTMLDivElement | null>(null);
// The About panel, and the facts it shows — fetched when it is opened rather than
// held from boot: the build info never changes, but the core path it reports can.
const aboutOpen = ref(false);
const about = ref<Record<string, string> | null>(null);
// group context menu; pinned to the window's left edge, vertically centred.
// width is carried over so the popup matches the tab it was opened from.
// x/y/h describe the tab the menu belongs to: its left edge, top edge and
// height, used to dock the popup directly above it (or below, when space runs
// out) rather than at a fixed spot on the left edge.
const gConfirm = ref<number | null>(null);
// group config page — editing an existing subscription. The create case is gone
// with the drawer's plus button: a subscription starts in the search box now.
// The group whose card is unfolded for editing. The edit page lives inside
// the subscription list now — the card's own row grows to hold it — so there
// is no floating dialog and nothing to dock; a gid here is enough.
const groupEditGid = ref<number | null>(null);
const moveOpen = ref(false);
const previewOpen = ref(false);
const previewText = ref('');
const qrOpen = ref(false);
const qrDataUrl = ref('');
const scanErr = ref('');

const testing = ref(false);
// Also mirrored in a ref for the re-entry guard, same reason as busyRef.
const testingRef = ref(false);
const setTesting = (v: boolean) => {
    testingRef.value = v;
    testing.value = v;
};
// Which of the four runs is going, so the bar can name it (see SPEED_RUNNING_KEYS).
// Only read while testing is true, so it needs no reset of its own.
const testingMode = ref(0);
const testProgress = ref<{ done: number; total: number }>({done: 0, total: 0});
const traffic = ref<{ up: number; down: number; upRate: number; downRate: number; count: number }>({up: 0, down: 0, upRate: 0, downRate: 0, count: 0});

// result sorting: click 延迟/速度 once for descending (worst/fastest on top,
// matching the sort arrow), again for ascending, a third time returns to
// the natural order
const sort = ref<{ key: 'latency' | 'speed'; dir: 1 | -1 } | null>(null);

const toggleSort = (key: 'latency' | 'speed') => {
    const cur = sort.value;
    if (!cur || cur.key !== key) sort.value = {key, dir: 1};
    else if (cur.dir === 1) sort.value = {key, dir: -1};
    else sort.value = null;
};

// What the search box above the table holds. A search is a way of looking at the
// list, not a change to it: the group, the sort and the running node are left alone,
// and a row still stands for the node it names — the group column says which
// subscription that was.
const query = ref('');
const searching = computed(() => query.value.trim() !== '');
// The box's second job needs one memory: a pull should not start a second of
// itself while the first is still on the wire.
const subBusy = ref(false);

const A = useActions({
    groups, gid, profiles, selected, running, runningIdRef, applyRunning,
    busyRef, setBusy, testingRef, setTesting, testingMode, testProgress,
    sort, spMode, spBusyRef, setSpBusyBoth, egress, setEgressOpen: (v: boolean) => egressOpen.value = v,
    exitTick, gConfirm, importOpen, importText, scanErr, previewOpen, previewText,
    qrOpen, qrDataUrl, moveOpen, listKey, query, subBusy, subsDrop, connOpen, connExit,
    showLogs, batchOpen, editor, notify, holdStatusNote, loadFileCats,
    openSubs, closeSubs, hardCloseSubs,
});
const {
    loadProfiles, reload, reloadQuiet, profileLoadSeq, selectGroup, hoverSwitch, hoverTimer,
    start, stop, switchTo, resetMetrics, runBatch, stopSpeedtest, speedTest,
    updateSub, deleteGroup, toggleSP, setEgressMode,
    addProfile, editProfile, cloneProfile, deleteProfile, copyLink,
    doImport, importFromClipboard, doPreview, addSubFromSearch, moveTo,
    showQR, showUrlQR, exportConfig, scanImage, openConns,
} = A;

// The DNS card's smart pick, on a timer.
//
// It lives out here rather than in the card because the card is only on screen while the
// reader is in it, and the schedule is not the card's business: with the switch on in the
// active profile, the resolvers are re-measured and the fastest of each written into that
// profile. That is what lets the card have no button to forget — the switch is the whole
// of the arrangement.
const routeTick = ref(0);
const smartBusy = ref(false);

// What the last round measured, by address: the two tables in the DNS card are ordered
// by it (see ResolverTable). Handed down rather than read there, because the round is
// made out here — the card is only sometimes on screen, and this is not.
const dnsLatency = ref<Record<string, number>>({});

// When the last round was made, for the interval to be counted from.
const lastSmart = {current: 0};

const runSmartPick = async (force: boolean) => {
    if (smartBusy.value) return;
    smartBusy.value = true;
    try {
        const name = (await API.GetActiveRouting()) ?? '';
        if (!name) return;
        const route: any = await API.GetRouting(name);
        // The round runs for either policy: 优选 asks it to pick, 切换 asks it to look —
        // and with both off there is nothing a round could do with its answer.
        if (!route?.dns_smart_select && !route?.dns_smart_switch) return;
        // Due? The interval is the profile's own, so it is read here rather than held in
        // a timer that was armed when it was something else. A round the switch has just
        // asked for is not due (force), and one made a moment ago is not due either.
        if (!force) {
            const minutes = Number(route.dns_smart_minutes) > 0
                ? Number(route.dns_smart_minutes)
                : SMART_PICK_MINUTES;
            if (Date.now() - lastSmart.current < minutes * 60 * 1000) return;
        }
        const round = await pickFastestDNS({remote: route.remote_dns, direct: route.direct_dns});
        // Published even when neither resolver moved: the ranking is what the round
        // produced either way, and it is the usual answer. Merged, because the card's own
        // buttons measure one list at a time and those numbers are still good.
        dnsLatency.value = {...dnsLatency.value, ...round.latency};
        lastSmart.current = Date.now();
        // What to write is the switches' business and not this function's (see
        // roundChoice): 优选 takes the fastest, 切换 only refuses to sit on a slow one.
        const next: any = {...route};
        const moved: string[] = [];
        for (const side of ['remote', 'direct'] as const) {
            const won = roundChoice(side, route, round);
            if (!won) continue;
            const field = side === 'remote' ? 'remote_dns' : 'direct_dns';
            if (next[field] === won.value) continue;
            next[field] = won.value;
            moved.push(`${t(side === 'remote' ? 'routing.remoteDNS' : 'routing.directDNS')} ${won.name}`);
        }
        // The same resolvers winning again is the usual outcome of a round: nothing to
        // write, and nothing to say about it.
        if (moved.length === 0) return;
        await API.SaveRouting(name, new setting.Routing(next));
        notify(`${t(route.dns_smart_select ? 'dns.smartAuto' : 'dns.switched')} ${moved.join(' · ')}`, 'ok');
        // An open settings card is holding the profile as it was; the file under it has
        // moved (see the dialog's routeTick effect).
        routeTick.value++;
    } catch {
        // A round that could not be made says nothing: it will come round again, and the
        // card's own button is there to report the reason on demand.
    } finally {
        smartBusy.value = false;
    }
};

let smartInterval = 0;
onMounted(() => {
    smartInterval = window.setInterval(() => {
        // The scheduled round is skipped while the window is hidden — this app
        // spends most of its life in the tray, and a round is fifty-odd resolvers
        // asked three questions each plus a write to the routing file, spent on a
        // table nobody is looking at. The forced round below is not skipped: it is
        // there because a tunnel has just appeared, and its answer goes into the
        // profile rather than onto the screen.
        if (document.hidden) return;
        void runSmartPick(false);
    }, SMART_TICK_MS);
});
onBeforeUnmount(() => window.clearInterval(smartInterval));

// And once whenever a node comes up, which is the moment the foreign side becomes
// measurable at all — its resolvers are reached through the tunnel, so a round made
// before it exists can only pick the domestic one. Forced, because the schedule has
// nothing to do with it: a tunnel that has just appeared is a reason to look.
watch(running, r => {
    if (r !== STOPPED) void runSmartPick(true);
});

// The language, worn the moment it is chosen rather than on 保存 — the one setting
// whose preview is the window itself. Called from the settings card (see onLanguage)
// and from syncUi below, which is the same number read back from the file or from
// the machine itself.
const applyLanguage = (code: number) => {
    const lang = langFromCode(code);
    setLang(lang);
    // The word table is one file per language (see tips.ts), so the file that was
    // asked for is fetched here. The tick follows the fetch rather than preceding it:
    // it is what puts the words already on screen into the language that was asked
    // for, and a tick before the words arrive would render the same text twice.
    loadTips(lang).then(() => langTick.value++).catch(e => { console.error('loadTips failed:', e); });
};

const syncUi = async () => {
    const s: any = await API.GetSettings();
    applyLanguage(startLanguage(s.language));
    // Read by index rather than through a table of its own: the picker and this
    // both point at THEMES, so a palette can never be added to one and not the
    // other.
    theme.value = THEMES[s.theme_schema === '2'
        ? Number(s.theme ?? 0)
        : (THEME_REMAP[Number(s.theme ?? 0)] ?? Number(s.theme ?? 0))]?.id ?? 'dark';
};

// The custom palette rides as inline variables, not as a rule of its own in
// App.css: this one's values are built at Save from two choices, so they
// go straight onto the document. Picking any other theme takes the
// footprint off again — an inline var would otherwise outshout the picked
// palette's own rule.
watch(theme, t2 => {
    if (t2 === 'custom' && wearCustom()) return;
    // fresh entry, nothing saved yet: fall through and wear the default until
    // the reader saves a first palette.
    for (const k of CUSTOM_VARS) document.documentElement.style.removeProperty(k);
    document.documentElement.setAttribute('data-theme', t2);
    // The ground the palette stands on, as its own attribute: the light side of
    // the two is what App.css's shared light arithmetic keys on (see
    // LIGHT_THEMES), so every pale palette inherits it without restating it.
    document.documentElement.setAttribute('data-variant', LIGHT_THEMES.has(t2) ? 'light' : 'dark');
}, {immediate: true});

// The bar's outbound mode is a setting, not a property of the active routing
// profile: it decides what the configbuilder does with that profile — apply it,
// or leave its rules out — rather than which profile is in use. So it is read
// from the settings object, and refetched when the dialog saves one.
const refreshEgress = async () => {
    try {
        const s: any = await API.GetSettings();
        // "block" is a retired mode: the backend reads it as "rule" now (the block
        // lists apply in every mode), and the list here has no such entry to light up.
        const mode = s?.egress_mode === 'block' ? 'rule' : s?.egress_mode || 'rule';
        egress.value = mode;
    } catch {
        // the button keeps what it last showed: a bar icon is not worth an error
    }
};

onMounted(() => {
    void refreshEgress();
});

// The setters the settings dialog is handed. The React ones were state setters
// (Dispatch<SetStateAction<...>>), so both shapes are accepted here: a value or a
// function of the previous one.
const setDnsLatency = (v: Record<string, number> | ((prev: Record<string, number>) => Record<string, number>)) => {
    dnsLatency.value = typeof v === 'function' ? v(dnsLatency.value) : v;
};
const setUiScale = (v: number | ((prev: number) => number)) => {
    uiScale.value = typeof v === 'function' ? v(uiScale.value) : v;
};
const setTheme = (v: string | ((prev: string) => string)) => {
    theme.value = typeof v === 'function' ? v(theme.value) : v;
};

const onChangeSettings = async () => {
    // The group on screen at the moment the settings
    // settle, not the one this render closed over: the
    // save is a disk round trip and the reader may have
    // moved on while it ran.
    await reload(gid.value);
    await loadProfiles(gid.value);
    await syncUi();
    // the settings dialog can have changed the outbound
    // mode too; the bar reads it from the same file
    void refreshEgress();
};

// "node connected": edge-triggered on the STOPPED → running transition only,
// so a group switch or a profile poll that re-emits the same running id
// does not replay it, and the chip itself stays as the permanent indicator.
const prevRunning = {current: running.value};
watch(running, r => {
    const wasRunning = prevRunning.current !== STOPPED;
    prevRunning.current = r;
    if (r === STOPPED || wasRunning) return;
    showStatusNote(tip('core.connected'), 'ok', 'window');
}, {immediate: true});

// The other subscriptions' nodes, read only while the box has something in it. With
// the box empty the list is the current subscription's, as it always was; the moment
// a name is typed, "which subscription is this in" becomes the question — the reader
// does not remember, which is why they are typing.
//
// Fetched once per search rather than per keystroke: the subscriptions do not change
// while a name is being typed, and a profile list per group per letter is a lot of
// round trips for an answer that is already known.
const otherProfiles = ref<main.ProfileInfo[]>([]);
// Which subscriptions there are, as one string. The effect below depends on the
// GROUPS being the same, not on the array being the same object — and `groups` is a
// new array on every reload (a start, a stop, an import, a hotkey switch all call
// it), so depending on it directly meant a full sweep of ListProfiles across every
// other subscription each time the reader started or stopped a node, while the box
// held a name. The ids are what the sweep is about.
const groupIds = computed(() => groups.value.map(g => g.id).join(','));
watch([searching, groupIds, () => gid.value, profilesEpoch], (_, __, onCleanup) => {
    if (!searching.value) {
        if (otherProfiles.value.length > 0) otherProfiles.value = [];
        return;
    }
    let alive = true;
    void (async () => {
        const out: main.ProfileInfo[] = [];
        for (const g of groups.value) {
            if (g.id === gid.value) continue;   // already in `profiles`
            try {
                out.push(...(await API.ListProfiles(g.id) ?? []));
            } catch {
                // A subscription that will not list is one search that cannot see it,
                // which is not a reason for the whole search to come back empty.
            }
        }
        if (alive) otherProfiles.value = out;
    })();
    onCleanup(() => { alive = false; });
}, {immediate: true});

// The list everything below reads: the current subscription, or every subscription
// once the box is in use.
const searchPool = computed<main.ProfileInfo[]>(
    () => (searching.value ? [...profiles.value, ...otherProfiles.value] : profiles.value));

const sortedProfiles = computed<main.ProfileInfo[]>(() => {
    if (!sort.value) return searchPool.value;
    const {key, dir} = sort.value;
    const arr = [...searchPool.value];
    arr.sort((a, b) => {
        const ga = metricGroup(a[key]), gb = metricGroup(b[key]);
        if (ga !== gb) return ga - gb;
        if (ga !== 0) return a.id - b.id; // untested/failed rows: keep list order
        return (a[key] - b[key]) * dir || a.id - b.id;
    });
    return arr;
});

// The rows on screen: the sorted list, narrowed to what the search box matches.
// Matched on the fields a row is actually read by — the name, the address, the
// port, the group — because those are what someone types when they are looking
// for one node among hundreds; the latency and the speed are numbers they sort
// by, not strings they search for.
const shownProfiles = computed<main.ProfileInfo[]>(() => {
    const q = query.value.trim().toLowerCase();
    if (q === '') return sortedProfiles.value;
    return sortedProfiles.value.filter(p => [p.name, p.address, p.port, p.group]
        .some(v => String(v ?? '').toLowerCase().includes(q)));
});

// The reorder above is a jump: the DOM order changes and the browser paints
// the rows at their new offsets on the next frame. While a test runs that
// reads as the list being rewritten rather than as a measurement arriving, so
// each row is carried from where it stood to where it now belongs. Rows are
// keyed by profile id, which is what makes this possible at all — the element
// at one offset is the same element that turns up at another, so measuring
// every row on both sides of the reorder gives the distance to animate.
const tbodyRef = ref<HTMLTableSectionElement | null>(null);
const rowTops = new Map<number, number>();

watch([sortedProfiles, testing], (_, __, onCleanup) => {
    const tbody = tbodyRef.value;
    if (!tbody) return;
    // offsetTop, not a bounding rect: the list enters with a transform on the
    // tbody (profilesIn), and a rect would measure that slide as the rows
    // having moved. offsetTop is the position in the layout, which is the
    // thing that changes when the order does — and the thing a WAAPI
    // animation leaves alone, so a row that is still travelling is measured
    // by where it belongs rather than by where it is being drawn.
    const tops = new Map<number, number>();
    const moved: {el: HTMLTableRowElement; dy: number}[] = [];
    for (const el of Array.from(tbody.querySelectorAll<HTMLTableRowElement>('tr[data-pid]'))) {
        const id = Number(el.dataset.pid);
        const top = el.offsetTop;
        tops.set(id, top);
        // Only while the test is running. A group switch, a refresh, a
        // hand-clicked header and the drag of a row across groups all move
        // rows too, and none of them are a measurement rippling through the
        // list — animating those would make every change of view look busy.
        const prev = rowTops.get(id);
        if (testing.value && prev !== undefined) {
            const dy = prev - top;
            if (Math.abs(dy) > 1) moved.push({el, dy});
        }
    }
    rowTops.clear();
    for (const [k, v] of tops) rowTops.set(k, v);
    if (moved.length === 0) return;
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    for (const {el, dy} of moved) {
        el.classList.add('flipping');
        const anim = el.animate(
            [{transform: `translateY(${dy}px)`}, {transform: 'translateY(0)'}],
            {duration: 380, easing: 'cubic-bezier(.2, .8, .2, 1)'},
        );
        anim.onfinish = () => el.classList.remove('flipping');
        anim.oncancel = () => el.classList.remove('flipping');
    }
}, {flush: 'post'});

// dismiss context menus (profile list and log) on any outside click
watch([ctx, logCtx], (_, __, onCleanup) => {
    if (!ctx.value && !logCtx.value) return;
    const close = () => {
        ctx.value = null;
        logCtx.value = null;
    };
    // Scroll is capture-phase, so it fires for every scrolling box in the
    // page — and the two menus scroll independently. A scroll inside the
    // log console (user wheel, or the auto-scroll that new lines write)
    // says nothing about where the node menu sits, so it closes only the
    // log menu; any other scroll closes both. Without this split, a busy
    // log stream kept shutting the node's right-click menu the moment it
    // was opened.
    const onScroll = (e: Event) => {
        if (e.target instanceof Node && logRef.value?.contains(e.target as Node)) {
            logCtx.value = null;
            return;
        }
        close();
    };
    const onKey = (e: KeyboardEvent) => {
        if (e.key === 'Escape') {
            ctx.value = null;
            logCtx.value = null;
        }
    };
    window.addEventListener('click', close);
    window.addEventListener('resize', close);
    window.addEventListener('scroll', onScroll, true);
    window.addEventListener('keydown', onKey);
    onCleanup(() => {
        window.removeEventListener('click', close);
        window.removeEventListener('resize', close);
        window.removeEventListener('scroll', onScroll, true);
        window.removeEventListener('keydown', onKey);
    });
});

// Outside click and Escape put the mode list away — the same two the context menus
// answer to. mousedown rather than click, so the list is gone before the press
// lands on whatever is behind it.
watch(egressOpen, (open, _, onCleanup) => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
        if (!egressRef.value?.contains(e.target as Node)) egressOpen.value = false;
    };
    const onKey = (e: KeyboardEvent) => {
        if (e.key === 'Escape') egressOpen.value = false;
    };
    window.addEventListener('mousedown', onDown);
    window.addEventListener('keydown', onKey);
    onCleanup(() => {
        window.removeEventListener('mousedown', onDown);
        window.removeEventListener('keydown', onKey);
    });
});

// The batch menu answers the same two, for the same reason: it is a menu of
// destructive actions, and it should close when the reader looks away.
watch(batchOpen, (open, _, onCleanup) => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
        if (!batchRef.value?.contains(e.target as Node)) batchOpen.value = false;
    };
    const onKey = (e: KeyboardEvent) => {
        if (e.key === 'Escape') batchOpen.value = false;
    };
    window.addEventListener('mousedown', onDown);
    window.addEventListener('keydown', onKey);
    onCleanup(() => {
        window.removeEventListener('mousedown', onDown);
        window.removeEventListener('keydown', onKey);
    });
});

// Keep the node menu inside the window, measured after mount. It lists every
// action a row has, which is tall enough to need this: the inline placement is
// provisional, and writing the clamped offset in a layout effect means the menu
// is never painted hanging off the bottom edge.
// The floor every menu is clamped above: the status bar's own top edge, minus a
// breath. The bar is the window's floor — the exit panel and the two lists hang
// above it by exactly this rule — and a menu that reached over it would be covering
// the one strip that says what the app is doing.
const menuFloor = () => {
    const bar = document.querySelector('.statusbar');
    // Read from the bar's own top edge rather than as innerHeight minus a height: one
    // number instead of two, and the very edge the exit panel and the two lists hang
    // from, so all of them agree about where the window's floor is.
    return (bar ? bar.getBoundingClientRect().top : window.innerHeight - 30) - 4;
};

// Both menus hang off the same floor, and both are measured before they are placed:
// the node menu was the only one clamped, and the log's carried a hard-coded 80px of
// headroom instead — less than that menu is tall, so a right-click near the foot of
// the window put it over the status bar. Two menus, one rule, so the next one that
// gets a right-click cannot invent a third.
watch([ctx, logCtx], () => {
    const place = (el: HTMLDivElement | null, at: {x: number; y: number} | null) => {
        if (!at || !el) return;
        // Measured before the cap below is applied: a menu measured through its own
        // cap can only ever get shorter, a frame at a time.
        el.style.maxHeight = '';
        const floor = menuFloor();
        const top = Math.max(8, Math.min(at.y, floor - el.offsetHeight));
        el.style.top = `${top}px`;
        // The cap is written here rather than left to the stylesheet: this one is the
        // bar as it is right now, and the stylesheet's is a fixed 30px variable that a
        // compact layout or a scaled UI can leave behind.
        el.style.maxHeight = `${Math.max(120, floor - top)}px`;
        // Everything above is arithmetic on a prediction; this is the result. If the
        // menu still reaches over the bar — a margin, a scale, a font that settled
        // after the measurement — it is moved up by exactly the overlap, which is the
        // one number that cannot be argued with. Reading it back is also what makes
        // this independent of whether the cause was ever identified.
        const over = el.getBoundingClientRect().bottom - floor;
        if (over > 0) {
            el.style.top = `${Math.max(8, top - over)}px`;
        }
    };
    place(ctxRef.value, ctx.value);
    place(logCtxRef.value, logCtx.value);
}, {flush: 'post'});

// --- submenu plumbing, shared by every row that owns one ---
const cancelSubClose = () => {
    if (subTimer.value !== null) {
        window.clearTimeout(subTimer.value);
        subTimer.value = null;
    }
};

const scheduleSubClose = () => {
    cancelSubClose();
    subTimer.value = window.setTimeout(() => {
        subTimer.value = null;
        sub.value = null;
    }, 160);
};

// a close still pending when the node menu goes away must not fire into a
// fresh one
onBeforeUnmount(cancelSubClose);

const openSub = (key: string, el: HTMLElement) => {
    cancelSubClose();
    subRowEl.value = el;
    sub.value = key;
};

// Dock the panel against the row that opened it, measured after mount. The
// first paint lands on the row's own left edge, so writing the offset in a
// layout effect keeps the panel from ever being painted in the wrong place.
watch(sub, s => {
    if (!s) return;
    const row = subRowEl.value;
    const el = subPanelEl.value;
    if (!row || !el) return;
    const ir = row.getBoundingClientRect();
    const w = el.offsetWidth;
    const h = el.offsetHeight;
    // Beside the row, on whichever side has room. The node menu is clamped to
    // the window's right edge, so for a row near that edge "to the right" is
    // off-screen and the submenu opens leftwards instead.
    const left = ir.right + 4 + w <= window.innerWidth - 8
        ? ir.right + 4
        : Math.max(8, ir.left - 4 - w);
    const top = Math.max(8, Math.min(ir.top - 5, menuFloor() - h));
    el.style.left = `${left}px`;
    el.style.top = `${top}px`;
}, {flush: 'post'});

// Names are given where subscriptions are made — in the search box, from the
// link's host — so the edit form never invents one. The numbered-name scheme the
// old create form kept (see CreateGroup's history) retired with that form.

// Open a subscription from the box and get out of the way. Shared by a row's
// click and its keyboard activation, so the two cannot drift apart.
const openGroup = (id: number) => {
    // a click is a deliberate change of subscription, so this is the one
    // path that replays the list's entrance animation
    listKey.value++;
    selectGroup(id);
    closeSubs();
};

const onAbout = async () => {
    aboutOpen.value = true;
    try {
        about.value = await API.GetAbout();
    } catch {
        about.value = null;
    }
};

// The About card's update check. One at a time (a double press gets one answer
// and one toast, not two requests), and the outcome is a toast either way: "no
// update" stays quiet in the card, a newer release is named and its page opens
// so the reader can fetch it.
// The About card's bug report. Opens the repository's new-issue page with an
// environment template already filled in — the version, the Go it runs on and
// the build time are the three things a bug report is useless without, and
// they are the three the reporter always forgets to copy. The repo comes from
// the same about answer the card shows, so a fork's build reports to the fork.
const reportBug = () => {
    const a = about.value ?? {};
    const repo = a.repo || 'sala-coding/EagleProxy';
    const body = [
        '### 环境',
        `- 版本：${a.version || '未知'}`,
        `- Go 版本：${a.goVersion || '未知'}`,
        `- 构建时间：${a.built || '未知'}`,
        '',
        '### 问题描述',
        '',
        '### 复现步骤',
        '',
    ].join('\n');
    API.OpenURL(`https://github.com/${repo}/issues/new?body=${encodeURIComponent(body)}`);
};

const updateBusy = ref(false);
const checkUpdate = async () => {
    if (updateBusy.value) return;
    updateBusy.value = true;
    try {
        const rel = await API.CheckUpdate();
        if (!rel) {
            notify(tip('settings.noUpdate'));
            return;
        }
        notify(tip('update.found', rel.tag_name ?? ''));
        if (rel.html_url) API.OpenURL(rel.html_url);
    } catch (e) {
        notify(errText(e), 'err');
    } finally {
        updateBusy.value = false;
    }
};

// What each submenu in the node menu holds. Written as data so the panel
// below is a single piece of markup: a new submenu is a line here and a row
// that hands over its key.
const submenus = computed<Record<string, { items: { key: string; label: string; run: () => void }[] }>>(() => ({
    testNode: {
        items: SPEED_MODE_KEYS.map((key, mode) => ({
            key,
            label: t(key),
            // the row this menu was opened on — the right click that opened it
            // also picked that row, so this is the node under the cursor
            run: () => speedTest(mode, false, ctx.value?.id),
        })),
    },
    testGroup: {
        items: SPEED_MODE_KEYS.map((key, mode) => ({
            key,
            label: t(key),
            run: () => speedTest(mode, true),
        })),
    },
    new: {
        items: PROTOCOLS.map(p => ({
            key: p.id,
            label: p.label,
            run: () => addProfile(p.id),
        })),
    },
}));

// The behaviour every submenu row shares, so the rows in the menu differ only
// in their label and their icon. The pointer's intent opens it; a click
// toggles, which is the same gesture on a touch screen.
const subRow = (key: string) => ({
    mouseenter: (e: MouseEvent) => openSub(key, e.currentTarget as HTMLElement),
    mouseleave: scheduleSubClose,
    click: (e: MouseEvent) => {
        if (sub.value === key) sub.value = null;
        else openSub(key, e.currentTarget as HTMLElement);
    },
    keydown: (e: KeyboardEvent) => {
        if (e.key !== 'Enter' && e.key !== ' ') return;
        e.preventDefault();
        if (sub.value === key) sub.value = null;
        else openSub(key, e.currentTarget as HTMLElement);
    },
});

const isRunning = computed(() => running.value !== STOPPED);

// The row handlers ProfileRow receives. Under React they were read through a ref
// and wrapped once in useCallback because `start` is redefined on every render and
// a memo row holding one of those closures would re-render with the rest; Vue's
// props comparison needs no such armour — the handlers here simply read and write
// the refs at call time.
const selectRow = (id: number) => selected.value = id;
const startRow = (id: number) => { void start(id); };
const ctxRow = (e: MouseEvent, id: number) => {
    e.preventDefault();
    e.stopPropagation();
    selected.value = id;
    ctx.value = {x: e.clientX, y: e.clientY, id};
};

// The reload the inline group editor is handed: the tree, then the list of the
// group on screen (see editProfile's note about the hotkey group switch).
const onGroupEditReload = async () => {
    await reload();
    await loadProfiles(gid.value);
};

const hueForGid = computed(() => hueFor(gid.value));

// The two context menus' provisional position (the placement watch above writes
// the clamped value once the real height is known). The window arithmetic lives
// in the script because Vue's template expressions cannot reach `window`.
const ctxStyle = computed<CSSProperties | undefined>(() => ctx.value ? {
    // numbers in :style do not gain a px suffix the way React's did; these are
    // lengths, so each one carries its unit explicitly
    left: `${Math.min(ctx.value.x, window.innerWidth - 210)}px`,
    // provisional; the layout effect above writes the clamped
    // value once the menu's real height is known
    top: `${Math.min(ctx.value.y, window.innerHeight - 360)}px`,
} as CSSProperties : undefined);
const logCtxStyle = computed<CSSProperties | undefined>(() => logCtx.value ? {
    left: `${Math.min(logCtx.value.x, window.innerWidth - 132)}px`,
    // the top is the placement effect's to write, once the menu has
    // been measured; this is only where it starts from
    top: `${logCtx.value.y}px`,
} as CSSProperties : undefined);

// A menu item that acts on the row the menu was opened on, then closes the menu.
const closeCtxRun = (fn: (id: number) => void | Promise<void>) => {
    const id = ctx.value!.id;
    ctx.value = null;
    void fn(id);
};

// The run-state dot blinks while a proxy mode is engaged: a green switch on either
// the system proxy or TUN is the whole of the condition, because that is what being
// on means here — the same fact the two rail switches state, said again in the one
// place that stays on screen whatever else is open. The core's own state is not
// asked about: a switch cannot be green without it, and asking twice would only give
// the two readings a way to disagree.
const proxyEngaged = computed(() => spMode.value.systemProxy || spMode.value.vpn);

// The status bar's height, measured and published as --status-h so the navigation
// rail can stop exactly at its top edge.
//
// The bar's height belongs to its contents — the chips decide it — and at some
// window sizes they stand taller than the number the stylesheet declares. A rail
// clearing a guessed height does not stop at the bar, it overlaps it, and the
// overlap is a rectangle in the corner: the rail's right border crosses the bar's
// top border and its bottom edge crosses the rail's own background. Two borders
// meeting twice make a box. Measuring is also what stops the two drifting apart
// again if the bar's contents ever change.
const appRef = ref<HTMLDivElement | null>(null);
const statusbarRef = ref<HTMLDivElement | null>(null);
let statusRo: ResizeObserver | null = null;
onMounted(() => {
    const bar = statusbarRef.value;
    const app = appRef.value;
    if (!bar || !app) return;
    const publish = () => {
        app.style.setProperty('--status-h', `${Math.round(bar.getBoundingClientRect().height)}px`);
    };
    publish();
    // A window resize can wrap the chips onto a second row, so this is observed
    // rather than measured once.
    statusRo = new ResizeObserver(publish);
    statusRo.observe(bar);
});
onBeforeUnmount(() => statusRo?.disconnect());

// 模板表达式会把顶层 setup ref 解包成值，而各 section 需要拿到 ref 本体
// （由子组件写入 .value），所以经一个普通对象转发。
const fwd = {subsBtnRef, panelRef, consoleRef, logRef, statusbarRef,
    egressRef, batchRef, nodeIpRef, infoRef};

// The root style: the same three blocks the React version wrote, plus the read of
// langTick that makes a language change re-render this component (Vue's render
// tracks it here; every t(...) in this template is then re-evaluated).
const appStyle = computed<CSSProperties>(() => {
    void langTick.value;
    return {
        // The rail is a fixed strip: it is not folded away any more, so
        // --rail-w is the one width rather than a choice between two.
        '--rail-w': '40px',
        // The panels that take the left seat — the subscriptions drawer and the
        // connections window — are not layers above the content: the table
        // gives up the width instead of being covered, which is what --gutter on
        // .app adds up. Either one being out is what opens it, and only ever one
        // of them: they share the seat.
        //
        // A panel on its way out has already handed the seat back: the table
        // starts taking its width at the same moment the panel starts to leave, so
        // the two move together and arrive together — the entrance, in the other
        // direction. It used to be the other way round: the table waited for the
        // panel to finish first, which made a close two movements in a row instead
        // of one mirrored movement.
        //
        // The settings are not in this sum. They do not take the seat beside the
        // table, they take the table's own box and stand on it (see the seat),
        // so nothing is given up here — the panel is measured against the table
        // wherever the table already is, and the table does not move.
        '--panel-open': ((subsDrop.value && !subsClosing.value) || (connOpen.value && !connExit.value)) ? 1 : 0,
        // The drawer's dragged width, and only that: with no width of the reader's
        // own the variable is left out entirely, so that --panel-w on .app falls
        // back to the window's share of itself (see the rule there). Writing the
        // automatic width here instead would put the share back into JavaScript,
        // where it has to be recomputed on every resize to stay right.
        ...(panelWUser.value === null ? {} : {'--panel-w-user': `${panelWUser.value}px`}),
    } as CSSProperties;
});

onMounted(() => {
    (async () => {
        try {
            const gs = await API.ListGroups();
            groups.value = gs;
            // reopen whichever subscription was last being looked at
            const s0: any = await API.GetSettings();
            const remembered = Number(s0?.current_group ?? -1);
            const first = gs.length > 0
                ? (gs.some(x => x.id === remembered) ? remembered : gs[0].id)
                : -1;
            gid.value = first;
            profiles.value = await API.ListProfiles(first);
            const rp = await API.RunningProfile();
            applyRunning(rp);
            logs.value = ((await API.GetLogs()) ?? []).map(l => ({line: l, cat: 'core'}));
            const logDir = await API.GetLogDir();
            if (logDir) logs.value = [...logs.value, {line: `${nowStamp()} >>> 日志目录: ${logDir}`, cat: 'core'}];
            spMode.value = await API.GetSPMode() as { vpn: boolean; systemProxy: boolean };
            await syncUi();
        } catch (e) { console.error('startup bootstrap failed:', e); }
    })();
});

// The settings dock's cache is warmed here rather than at the dock's mount, and
// this is why: the dock's pages below the hand-written cards are built from the
// settings object, and an object that has not arrived yet is no cards at all —
// a first open used to show the nav standing there while the pages assembled
// behind it, one wire-trip later. Read once at startup, and the first open has
// the same head start the second one already had. A failed read costs nothing:
// the dock's own open reads again.
onMounted(() => {
    (async () => {
        try {
            const s: any = await API.GetSettings();
            warmSettingsCache(s);
        } catch { /* the dock's open effect re-reads regardless */ }
    })();
});

// The window's event wiring, installed once (the React effect ran under deps
// [notify], which never changed; here the handlers read the refs at call time, so
// there is nothing to re-register).
let logOff: (() => void)[] = [];
onMounted(() => {
    // One update for the lines that arrived since the last beat (see pendingLogs).
    // EventsOn returns a cancel func: without it every re-run (and React
    // StrictMode's double mount in dev) would register a second listener
    // and duplicate every event.
    logOff = [
        // A line's category decides which half of the stream it belongs to, and it
        // belongs to exactly one of them: the test categories live in catLogs, where
        // the file re-read puts the same text once the run ends. Landing in `logs`
        // as well was how an open panel showed every test line twice — the merge
        // below adds the two halves together, and the streamed copy and the file
        // copy carry the very same stamped text.
        EventsOn("core:log", (line: string, cat: string) => queueLogLine(line, cat)),
        // The payload is the node the process was serving, read by the backend as
        // it exited — so a core that left after a hot switch names the node the
        // reader switched to. Only that node's exit means "nothing is running":
        // the exit of a core a switch had already replaced used to arrive after
        // the new state did and wipe it back to "stopped", traffic counters and
        // all, while the node it named was running perfectly well.
        EventsOn("core:exit", (id: number) => {
            if (id !== runningIdRef.value) return;
            applyRunning(STOPPED);
            traffic.value = {up: 0, down: 0, upRate: 0, downRate: 0, count: 0};
        }),
        // starts/stops that bypass the frontend (tray menu, autostart)
        EventsOn("running:changed", async (id: number) => {
            // The ref is written here as well, not just on commit: a core:exit
            // for the replaced core can arrive in the same tick, and the ref's
            // render-time sync would still hold the old id — reading the exit
            // as "the current node died" and wiping the traffic state.
            applyRunning(id);
        }),
        // The mode is the backend's fact, not this window's: the tray's two
        // switches, a global hotkey and the remembered-mode restore all change
        // it without a press here, and the rail kept showing the last value
        // this window chose — until the next press, which then asked to undo a
        // mode that had already moved. The event carries no payload: the flags
        // are re-read, so this is the announcement of a settled state rather
        // than a claim that could arrive out of order with another.
        EventsOn("spmode:changed", () => {
            API.GetSPMode()
                .then(m => spMode.value = m as { vpn: boolean; systemProxy: boolean })
                .catch(() => { /* the next press reads the mode anyway */ });
        }),
        EventsOn("traffic", (tv: { up: number; down: number; upRate: number; downRate: number; count: number }) => traffic.value = {
            // tolerate a backend that predates the rate fields
            up: tv.up ?? 0,
            down: tv.down ?? 0,
            upRate: tv.upRate ?? 0,
            downRate: tv.downRate ?? 0,
            count: tv.count ?? 0,
        }),
        EventsOn("profile:traffic", (r: { id: number; up: number; down: number }) => {
            profiles.value = profiles.value.map(p => p.id === r.id ? {...p, up: r.up, down: r.down} : p);
        }),
        EventsOn("speedtest:result", (r: { id: number; mode: number; latency: number; speed?: number }) => {
            if (testProgress.value.total > 0) testProgress.value = {...testProgress.value, done: Math.min(testProgress.value.done + 1, testProgress.value.total)};
            profiles.value = profiles.value.map(p => {
                if (p.id !== r.id) return p;
                const next = {...p};
                // refresh only the column this mode actually measured
                if (MODE_HAS_LATENCY(r.mode)) next.latency = r.latency;
                if (MODE_HAS_SPEED(r.mode)) next.speed = r.speed ?? 0;
                return next;
            });
        }),
        EventsOn("groups:changed", async (newGid: number) => {
            const seq = ++groupsChangedSeq.current;
            // The search list is re-built off the same tick: it holds profiles from
            // every other group, and an update rewrote those as well (see
            // profilesEpoch).
            profilesEpoch.value++;
            try {
                // The list itself is re-read first: the wipe path also speaks
                // this event, and it deletes groups — the rail must reflect
                // the tree the backend now holds, which a hotkey switch
                // needs not (the group still exists there) but keeps no
                // harm from either.
                await reloadQuiet();
                if (seq !== groupsChangedSeq.current) return;
                // A negative payload means "keep the view": background walks
                // and refreshes re-read the lists but must not yank the
                // reader's group or clear their selection — the settings
                // file the -1 used to be resolved from lags a just-made
                // switch, so the old answer would drag the view back.
                const viewGid = newGid >= 0 ? newGid : gid.value;
                // The same guard the load path uses, because this writes the same
                // list: without it a slow ListProfiles for the group the backend
                // just switched to could land after one the reader had asked for
                // by hand, leaving one group's nodes under another group's
                // highlight — and nothing later would correct it. The group
                // switch rides behind the same guard: setGid used to run before
                // the ask, so a list dropped by the seq check left the highlight
                // on a group whose rows never arrived until the next reload.
                const loadSeq = ++profileLoadSeq.current;
                const list = await API.ListProfiles(viewGid);
                if (seq !== groupsChangedSeq.current) return;
                if (loadSeq !== profileLoadSeq.current) return;
                if (newGid >= 0) {
                    gid.value = newGid;
                    selected.value = -1;
                }
                profiles.value = list;
            } catch (e) { if (seq === groupsChangedSeq.current) console.error('groups:changed failed:', e); }
        }),
        EventsOn("routing:changed", (name: string) => notify(tip('route.switchedTo', name))),
        // The outbound mode can be changed from outside the window too — the hotkey
        // steps through the four — and the bar reads it from the settings file rather
        // than from its own last press. This is that file changing.
        EventsOn("egress:changed", (mode: string) => egress.value = mode),
        // The two keys that belong to the window rather than to the backend: which
        // panel is open is this side's state, so the backend only says which one was
        // asked for (see emitHotkeyToWindow) — and it brings the window up first, so
        // a press is never spent on a panel behind a hidden window.
        EventsOn("hotkey:connections", () => openConns()),
        EventsOn("hotkey:logs", () => toggleLogs()),
        // A second launch being intercepted is said through app:message now: the
        // interception can happen before this window has a backend context to emit
        // into, and the notice queue is the side that holds a message until someone
        // can hear it (see notifySecondInstance in the Go side). An event here as
        // well would be the same sentence twice.
        // the backend's own messages: which step of the mode-first flow is
        // missing, or that a mode is on and waiting for a node
        EventsOn("app:message", (text: string, isErr: boolean) =>
            notify(String(text), isErr ? 'err' : 'ok')),
    ];
    // Messages the backend raised while this page was still booting — the TUN
    // handover does exactly that — are collected here rather than pushed, so
    // none of them can slip through the gap before the listeners above
    // existed. Called after EventsOn for that reason.
    API.ConsumeNotices()
        .then(queued => (queued ?? []).forEach(n => notify(n.text, n.isErr ? 'err' : 'ok')))
        .catch(() => { /* nothing was waiting; not worth a message of its own */ });
});
onBeforeUnmount(() => {
    logOff.forEach(f => f && f());
    // This effect is installed once for the window's life, but a StrictMode
    // remount (dev) tears it down; the lines still in the batch belong to the
    // stream and are handed over rather than dropped.
    flushPendingLogs();
});

// The pending hover switch must not fire after the component is gone.
onBeforeUnmount(() => {
    if (hoverTimer.current !== null) window.clearTimeout(hoverTimer.current);
});

</script>

<template>
    <div :class="`app${panelResizing ? ' resizing' : ''}`" ref="appRef" :style="appStyle">
        <NavRail :sp-mode="spMode" :sp-busy="spBusy" :has-groups="groups.length > 0"
                 :subs-drop="subsDrop" :show-logs="showLogs" :conn-open="connOpen"
                 :proxy-engaged="proxyEngaged" :subs-btn-ref="fwd.subsBtnRef" :lang-tick="langTick"
                 @toggleSP="toggleSP" @toggle-subs="subsDrop ? closeSubs() : openSubs()"
                 @toggle-logs="toggleLogs" @open-conns="openConns" @about="onAbout"/>

        <SubsDrawer v-if="subsDrop"
                    :groups="groups" :gid="gid" :busy="busy" :sub-busy="subBusy"
                    :g-confirm="gConfirm" :group-edit-gid="groupEditGid" :name-cols="nameCols"
                    :panel-w="panelW" :panel-resizing="panelResizing" :subs-closing="subsClosing"
                    :panel-ref="fwd.panelRef" :edit-reload="onGroupEditReload" :lang-tick="langTick"
                    @grip-down="onPanelGripDown" @grip-move="onPanelGripMove" @grip-up="onPanelGripUp"
                    @grip-key="onPanelGripKey" @grip-dbl="resetPanelW"
                    @open-group="openGroup" @hover-switch="hoverSwitch" @peek-reel="peekReel"
                    @drop-reel="dropReel" @update-sub="updateSub" @delete-group="deleteGroup"
                    @showUrlQR="showUrlQR" @toggle-edit="gid2 => groupEditGid = groupEditGid === gid2 ? null : gid2"
                    @edit-done="groupEditGid = null" @edit-notify="notify"/>

        <div class="body">
            <!-- The column the node table lives in. It used to turn away about its
                bottom edge while the settings were open — the table folding back behind
                the screen to uncover the panel — then held its seat instead; now it veils
                again, because the dock stands on the bar at the far left and the table
                running right up to its edge behind it read as clutter rather than as
                depth. The veil is visibility, not display: the holder keeps its box
                (measured columns frozen, scroll position intact) and fades (see .veil
                in App.css), so the turn is a dimmer, not a page turn. -->
            <main class="content">
                <div :class="settingsOpen ? 'table-wrap veil' : 'table-wrap'"
                     ref="tableWrapRef"
                     :style="{'--tab-hue': hueForGid}"
                     @contextmenu.prevent="ctx = null">
                    <table class="profiles" ref="tableRef">
                        <thead>
                        <tr>
                            <!-- First, where the table had it before the column went away:
                                what a node is, before what it is called. Its cells wear the
                                subscription's own colour (see the td below), so the column
                                also answers "whose node is this" at a glance. -->
                            <th class="type">{{ t('col.type') }}</th>
                            <th>{{ t('col.name') }}</th>
                            <th>{{ t('col.address') }}</th>
                            <th class="port">{{ t('col.port') }}</th>
                            <th>{{ t('col.network') }}</th>
                            <th>{{ t('col.tls') }}</th>
                            <th :class="`latency sortable${sort?.key === 'latency' ? ' sorted' : ''}`"
                                @click="toggleSort('latency')">
                                {{ t('col.latency') }}
                                <span v-if="sort?.key === 'latency'" class="sort-mark">{{ sort.dir === 1 ? '▲' : '▼' }}</span>
                            </th>
                            <th :class="`speed sortable${sort?.key === 'speed' ? ' sorted' : ''}`"
                                @click="toggleSort('speed')">
                                {{ t('col.speed') }}
                                <span v-if="sort?.key === 'speed'" class="sort-mark">{{ sort.dir === 1 ? '▲' : '▼' }}</span>
                            </th>
                            <th class="ptraffic">{{ t('col.traffic') }}</th>
                            <th>{{ t('col.group') }}</th>
                        </tr>
                        </thead>
                        <!-- Keyed by listKey, bumped only on a tab click: the
                            remount is what replays the entrance animation, and a
                            hover-driven group change must not cause one — not on
                            the way in, and not on the way out either. In-place row
                            updates (traffic, speedtest) never remount, which is
                            also what lets a row be carried to its new place
                            instead of arriving with a freshly mounted one (see
                            the flip effect above). -->
                        <tbody :key="listKey" ref="tbodyRef">
                        <ProfileRow v-for="p in shownProfiles" :key="p.id" :p="p"
                                    :selected="p.id === selected"
                                    :running="p.id === running"
                                    @select="selectRow" @start="startRow" @ctx="ctxRow"/>
                        <tr v-if="searchPool.length === 0">
                            <td colspan="10" class="empty">{{ t('table.empty') }}</td>
                        </tr>
                        <!-- Two different emptinesses: a group with no nodes is the
                            group's own state, while nothing matching a search means
                            the list is there and the words are not. Naming which of
                            the two it is keeps the reader from hunting the group
                            selector for a problem that sits in the search box. -->
                        <tr v-if="searchPool.length > 0 && shownProfiles.length === 0">
                            <td colspan="10" class="empty">{{ t('search.none') }}</td>
                        </tr>
                        </tbody>
                    </table>
                </div>

                <!-- The yield, as a heap. The table slides right to let the
                    subscriptions drawer into its seat, and while the drawer is
                    out the tables of four subscriptions stand in a heap in the
                    table's own box — each hinged on its own right border and
                    turned away from the reader, the reader's own on top and the
                    three after it behind it, each showing the strip of its right
                    border that the table in front of it does not cover.

                    A place rather than a flourish: mounted with the drawer and
                    taken away with it, standing in the table's own box — the
                    table itself stands aside for as long as the heap is up (see
                    the rule under .reel.out), because the same nodes in the same
                    columns a few rows out of step read as five tables rather than
                    four, and neither of the two would line up with the other to
                    be read. The pointer means one thing here and one thing only:
                    pointing at a table — in the heap or at the drawer's card of a
                    subscription — brings that subscription's table to the front
                    (see paintStack), which is the only way to read a subscription
                    that is not the current one without changing which one is
                    current. A click still means nothing — it would land on nothing
                    rather than on a scroller the reader cannot see — so the whole
                    heap is aria-hidden because every table on it is a subscription
                    the drawer beside it already lists and acts on, and a second
                    reading of the same thing is not something to be read out. A heap
                    needs two tables to be a heap, and with one subscription the
                    table keeps its box. -->
                <div v-if="subsDrop && groups.length > 1" :class="'reel' + (subsClosing ? ' out' : '')"
                     ref="reelRef"
                     aria-hidden="true"
                     :style="{
                         // How far each table's left border goes back behind its own
                         // right one. The stylesheet applies the tilt as a negative
                         // rotateY: a positive one would swing the left border towards
                         // the reader, and the stack goes away from them. How far off
                         // the eye the stack stands (--stack-p) is the observer's, too.
                         '--stack-tilt': STACK_TILT + 'deg',
                     }">
                    <div class="reel-stack"
                         ref="stackRef"
                         @mousemove="trackReel"
                         @mouseleave="dropReel('stack')"
                         :style="{
                             // --stack-w, --stack-h and --stack-p are written by the
                             // observer rather than declared here; see the layout
                             // effect above. --stack-step and --stack-depth are the
                             // two constants that never change with the room.
                             '--stack-step': STACK_STEP + 'px',
                             '--stack-depth': STACK_DEPTH + 'px',
                         }">
                        <!-- Back to front, so the paint order alone would be right
                            even if the depth sorting were not: layer 0 is the
                            reader's own subscription and belongs on top. The
                            current subscription's nodes are already in hand, so its
                            face is handed them as a seed and never has to ask. -->
                        <ReelTable v-for="(g, ri) in [...stack].reverse()" :key="g.id"
                                   :g="g" :layer="stack.length - 1 - ri"
                                   :total="stack.length"
                                   :ready="reelReady"
                                   :seed="g.id === gid ? profilesRef : undefined"
                                   :epoch="g.id === gid ? 0 : profilesEpoch"/>
                    </div>
                </div>
            </main>
        </div>

        <LogPanel v-if="showLogs"
                  :console-h="consoleH" :console-dragging="consoleDragging" :console-now="consoleNow"
                  :log-paused="logPaused" :log-filter="logFilter" :log-level="logLevel"
                  :cat-live="catLive" :shown-logs="shownLogs"
                  :console-ref="fwd.consoleRef" :log-ref="fwd.logRef" :lang-tick="langTick"
                  @grip-down="onConsoleGripDown" @grip-move="onConsoleGripMove" @grip-up="onConsoleGripUp"
                  @grip-key="onConsoleGripKey" @grip-dbl="resetConsoleH"
                  @toggle-pause="logPaused = !logPaused"
                  @set-level="v => logLevel = v" @pick-filter="id => logFilter = id"
                  @copy="copyLogs" @clear="clearLogs" @collapse="toggleLogs"
                  @scroll="onLogScroll"
                  @body-ctx="at => logCtx = at" @line-ctx="at => logCtx = at"/>

        <StatusDock :traffic="traffic" :status-note="statusNote"
                    :testing="testing" :testing-mode="testingMode" :test-progress="testProgress"
                    :egress="egress" :egress-open="egressOpen" :batch-open="batchOpen"
                    :query="query" :sub-busy="subBusy"
                    :shown-count="shownProfiles.length" :pool-count="searchPool.length"
                    :running="running" :is-running="isRunning"
                    :exit-ip="exitIP" :flag-missing="flagMissing" :info-open="infoOpen"
                    :proxy-engaged="proxyEngaged" :settings-open="!!settingsOpen"
                    :statusbar-ref="fwd.statusbarRef" :egress-ref="fwd.egressRef" :batch-ref="fwd.batchRef"
                    :node-ip-ref="fwd.nodeIpRef" :info-ref="fwd.infoRef" :lang-tick="langTick"
                    @open-settings="settingsOpen = settingsOpen ? null : 'plain'"
                    @set-query="v => query = v" @submit-sub="addSubFromSearch(query)"
                    @toggle-batch="batchOpen = !batchOpen"
                    @run-batch="kind => runBatch(kind)"
                    @toggle-egress="egressOpen = !egressOpen"
                    @set-egress="m => setEgressMode(m)"
                    @toggle-info="infoOpen = !infoOpen"
                    @copied="notify(tip('copied'))"
                    @flag-missing="flagMissing = true"
                    @stop-test="stopSpeedtest"
                    @state-enter="showStatusNote(isRunning ? tip('core.running') : tip('core.stopped'))"
                    @state-leave="hideStatusNote"/>

        <!-- The message window: every transient toast the app has to say, in its
            own little window rather than the status bar's text slot. It is
            centred on the main window on both axes — the one place the reader
            is already looking — and carries its own title strip and close, so
            it reads as a window rather than as a toast bolted to the bar. The
            bar keeps the quiet words: hover hints and a wait in progress. -->
        <div v-if="statusNote && statusNote.mode === 'window'" :key="statusNote.seq" role="alertdialog" aria-live="polite"
             :class="`note-window${statusNote.leaving ? ' leaving' : ''}${statusNote.kind === 'err' ? ' err' : ''}`">
            <div class="note-window-head">
                <i :class="`note-window-dot${statusNote.kind === 'err' ? ' err' : ''}`"/>
                <span class="note-window-title">{{ t('status.noteTitle') }}</span>
                <button class="note-window-close"
                        :title="t('btn.close')"
                        :aria-label="t('btn.close')"
                        @click="hideStatusNote">✕</button>
            </div>
            <div class="note-window-body">{{ statusNote.text }}</div>
        </div>

        <div v-if="ctx" class="ctxmenu"
             ref="ctxRef"
             :style="ctxStyle"
             @click.stop
             @contextmenu.prevent>
            <button v-if="ctx.id === running" class="ctx-item"
                    @click="() => {
                        ctx = null;
                        stop();
                    }">
                <StopFilled class="ic"/>{{ t('btn.stop') }}
            </button>
            <button v-else-if="isRunning" class="ctx-item"
                    @click="closeCtxRun(switchTo)">
                <TransferHorizontalFilled class="ic"/>{{ t('btn.switch') }}
            </button>
            <button v-else class="ctx-item"
                    @click="closeCtxRun(start)">
                <PlayFilled class="ic"/>{{ t('btn.start') }}
            </button>
            <!-- Both test entries name a test type through a submenu, and both
                used to lean on one select in the bottom bar: which type a
                run uses is worth choosing where the run is, not in a
                control that was hidden until the pointer came up for it. -->
            <button class="ctx-item ctx-item-sub"
                    tabindex="0"
                    aria-haspopup
                    :aria-expanded="sub === 'testNode'"
                    v-on="subRow('testNode')">
                <DashboardFilled class="ic"/>{{ t('btn.testCurrent') }}
                <RightFilled class="ic ctx-sub-caret"/>
            </button>
            <button class="ctx-item ctx-item-sub"
                    tabindex="0"
                    aria-haspopup
                    :aria-expanded="sub === 'testGroup'"
                    v-on="subRow('testGroup')">
                <FlashFilled class="ic"/>{{ t('btn.testGroup') }}
                <RightFilled class="ic ctx-sub-caret"/>
            </button>
            <div class="ctx-sep"/>
            <!-- New node: the protocol list is the choice, and it was a
                separate select in the bottom bar only because the bar had
                room for a control that stands on its own. -->
            <button class="ctx-item ctx-item-sub"
                    tabindex="0"
                    aria-haspopup
                    :aria-expanded="sub === 'new'"
                    v-on="subRow('new')">
                <AddFilled class="ic"/>{{ t('btn.add') }}
                <RightFilled class="ic ctx-sub-caret"/>
            </button>
            <button class="ctx-item"
                    @click="() => {
                        ctx = null;
                        importOpen = true;
                    }">
                <FileImportFilled class="ic"/>{{ t('btn.import') }}
            </button>
            <button class="ctx-item" @click="closeCtxRun(editProfile)">
                <Edit2Filled class="ic"/>{{ t('btn.edit') }}
            </button>
            <button class="ctx-item" @click="closeCtxRun(cloneProfile)">
                <CopyFilled class="ic"/>{{ t('btn.clone') }}
            </button>
            <button class="ctx-item"
                    @click="() => {
                        ctx = null;
                        moveOpen = true;
                    }">
                <TransferHorizontalFilled class="ic"/>{{ t('btn.move') }}
            </button>
            <div class="ctx-sep"/>
            <button class="ctx-item" @click="closeCtxRun(id => copyLink(id, false))">
                <Link2Filled class="ic"/>{{ t('btn.copyLink') }}
            </button>
            <button class="ctx-item" @click="closeCtxRun(id => copyLink(id, true))">
                <Link3Filled class="ic"/>{{ t('btn.copyNekoray') }}
            </button>
            <div class="ctx-sep"/>
            <button class="ctx-item" @click="closeCtxRun(doPreview)">
                <CodeFilled class="ic"/>{{ t('btn.preview') }}
            </button>
            <button class="ctx-item" @click="closeCtxRun(exportConfig)">
                <FileExportFilled class="ic"/>{{ t('btn.export') }}
            </button>
            <button class="ctx-item" @click="closeCtxRun(showQR)">
                <QrcodeFilled class="ic"/>{{ t('btn.qr') }}
            </button>
            <div class="ctx-sep"/>
            <!-- Clearing the measurements is a whole-window action, so it
                lives here rather than on a bar that had to be summoned. -->
            <button class="ctx-item"
                    :title="t('btn.resetMetrics')"
                    @click="() => {
                        ctx = null;
                        resetMetrics();
                    }">
                <EraserFilled class="ic"/>{{ t('btn.reset') }}
            </button>
            <button class="ctx-item danger" @click="closeCtxRun(deleteProfile)">
                <Delete2Filled class="ic"/>{{ t('btn.delete') }}
            </button>
        </div>

        <!-- The open row's submenu, as a panel of its own beside that row. It is
            guarded by the menu as well as by its own key, so it cannot outlive
            the menu it belongs to. -->
        <div v-if="ctx && sub && submenus[sub]" class="ctxmenu ctxmenu-sub"
             ref="subPanelEl"
             @mouseenter="cancelSubClose"
             @mouseleave="scheduleSubClose"
             @click.stop
             @contextmenu.prevent>
            <button v-for="it in submenus[sub].items" :key="it.key"
                    class="ctx-item"
                    @click="() => {
                        sub = null;
                        ctx = null;
                        it.run();
                    }">
                {{ it.label }}
            </button>
        </div>

        <div v-if="logCtx" class="ctxmenu narrow"
             ref="logCtxRef"
             :style="logCtxStyle"
             @click.stop
             @contextmenu.prevent>
            <!-- Only when the click landed on a line: the address in it can be
                turned into a rule, and the three lists are the three things a
                rule can say about it. A click on the panel's background has no
                address in it, so it gets the two items below and nothing else. -->
            <template v-if="logCtx.line !== undefined">
                <button v-for="kind in (['direct', 'proxy', 'block'] as const)" :key="kind" class="ctx-item"
                        @click="addLogLineToRoute(logCtx.line ?? '', kind)">
                    <component :is="FILTER_ICONS[kind]" class="ic"/>
                    <span class="ctx-vsep" aria-hidden="true"/>
                    {{ t(`log.route.${kind}`) }}
                </button>
                <div class="ctx-sep"/>
            </template>
            <button class="ctx-item" @click="copyLogs">
                <CopyFilled class="ic"/>
                <span class="ctx-vsep" aria-hidden="true"/>
                {{ t('log.copy') }}
            </button>
            <!-- Saving the panel to a file. It sits with the copy: it is the same act —
                taking a copy away — with a different destination. (The clear used to be
                the third of these. It is on the panel's own title bar now, where the
                thing it undoes — the panel filling up — is what the reader is looking
                at, and where it takes one press instead of a right-click and a find.) -->
            <button class="ctx-item" @click="exportLogs">
                <Download3Filled class="ic"/>
                <span class="ctx-vsep" aria-hidden="true"/>
                {{ t('log.export') }}
            </button>
        </div>

        <Modal v-if="importOpen" :title="t('dlg.import.title')" wide @close="importOpen = false">
            <p class="hint">{{ t('dlg.import.hint') }}</p>
            <textarea class="rawjson" rows="14" v-model="importText"
                      placeholder="vmess://...&#10;trojan://..."/>
            <div v-if="scanErr" class="errline">{{ scanErr }}</div>
            <div class="modal-foot">
                <label class="btn ghost" style="cursor: pointer">
                    {{ t('btn.scanImage') }}
                    <input type="file" accept="image/*" style="display: none"
                           @change="e => {
                               const f = (e.target as HTMLInputElement).files?.[0];
                               if (f) scanImage(f);
                               (e.target as HTMLInputElement).value = '';
                           }"/>
                </label>
                <button class="btn ghost" @click="importFromClipboard">{{ t('btn.readClipboard') }}</button>
                <button class="btn ghost" @click="importOpen = false">取消</button>
                <button class="btn primary" :disabled="!importText.trim()"
                        @click="doImport">导入到「{{ groups.find(g => g.id === gid)?.name ?? '' }}」
                </button>
            </div>
        </Modal>

        <EditorDialog v-if="editor" :title="editor.title" :initial="editor.initial"
                      :on-save="editor.onSave" :on-discard="editor.onDiscard" @close="editor = null"/>

        <!-- About: the name, then the facts a bug report needs. Nothing here is
            editable — it is a card to read and to copy from, which is why the
            paths are in the monospace face rather than as truncated labels. They
            are written the way the settings field writes them, relative to the
            executable, and the full path waits in the tooltip (see relToExec in
            the backend). -->
        <Modal v-if="aboutOpen" :title="t('dlg.about.title')" @close="aboutOpen = false">
            <div class="about-head">
                <!-- The app's own mark, the same artwork the exe and the tray
                    wear (see fa/icon) — this card is stamped with what the
                    reader is running, not with the language it is in. -->
                <img class="about-logo" src="/fa/icon/eagle_64.png"
                     alt="" aria-hidden="true"/>
                <div class="about-name">Eagle</div>
            </div>
            <dl class="about-rows">
                <dt>{{ t('about.version') }}</dt>
                <dd>
                    {{ about?.version || t('about.unknown') }}
                    {{ about?.built ? ` · ${about.built}` : '' }}
                </dd>
                <dt>{{ t('about.go') }}</dt>
                <dd>{{ about?.goVersion || t('about.unknown') }}</dd>
                <dt>{{ t('about.core') }}</dt>
                <dd :title="about?.corePathAbs || ''">{{ about?.corePath || t('about.unknown') }}</dd>
                <dt>{{ t('about.dataDir') }}</dt>
                <dd :title="about?.dataDirAbs || ''">{{ about?.dataDir || t('about.unknown') }}</dd>
            </dl>
            <div class="modal-foot">
                <button class="btn ghost" @click="API.OpenDataDir().catch(e => notify(errText(e), 'err'))">
                    {{ t('about.openDataDir') }}
                </button>
                <button class="btn ghost" :disabled="updateBusy" @click="checkUpdate">
                    {{ t('about.checkUpdate') }}
                </button>
                <button class="btn ghost" @click="reportBug">
                    {{ t('about.bug') }}
                </button>
                <button class="btn ghost" @click="() => {
                    const repo = about?.repo;
                    if (repo) API.OpenURL(`https://github.com/${repo}`).catch(() => { /* the link is more decoration than a promise */ });
                }">
                    {{ t('about.repo') }}
                </button>
                <button class="btn primary" @click="aboutOpen = false">
                    {{ t('btn.close') }}
                </button>
            </div>
        </Modal>

        <SettingsDialog v-if="settingsOpen"
                        @close="settingsOpen = null"
                        @notify="notify"
                        @language="applyLanguage"
                        :route-tick="routeTick"
                        :dns-latency="dnsLatency"
                        :on-dns-latency="setDnsLatency"
                        :node-on="running !== STOPPED"
                        :focus="settingsOpen === 'routing' ? 'routing' : undefined"
                        :ui-scale="uiScale" @update:ui-scale="setUiScale"
                        :system-scale="sysScale"
                        :theme="theme" @update:theme="setTheme"
                        :tun-on="spMode.vpn"
                        :egress="egress" @update:egress="setEgressMode"
                        :on-change-settings="onChangeSettings"/>

        <Modal v-if="moveOpen" :title="t('dlg.move.title')" @close="moveOpen = false">
            <ul class="picklist">
                <li v-for="g in groups.filter(g => g.id !== gid)" :key="g.id" @click="moveTo(g.id)">
                    <span><FolderFilled class="ic"/>{{ g.name || t('groups.unnamed') }}</span>
                </li>
                <li v-if="groups.filter(g => g.id !== gid).length === 0" class="empty">{{ t('groups.none') }}</li>
            </ul>
        </Modal>

        <Modal v-if="previewOpen" :title="t('dlg.preview.title')" wide @close="previewOpen = false">
            <pre class="code">{{ previewText }}</pre>
            <div class="modal-foot">
                <button class="btn primary" @click="previewOpen = false">{{ t('btn.close') }}</button>
            </div>
        </Modal>

        <ConnectionsDialog v-if="connOpen"
                           :exiting="connExit"
                           @closing="connExit = $event"
                           @close="() => { connExit = null; connOpen = false; }"
                           @notify="notify"/>

        <Modal v-if="qrOpen" :title="t('dlg.qr.title')" @close="qrOpen = false">
            <div class="qr-wrap">
                <img :src="qrDataUrl" alt="QR"/>
            </div>
            <div class="modal-foot">
                <button class="btn primary" @click="qrOpen = false">{{ t('btn.close') }}</button>
            </div>
        </Modal>
    </div>
</template>

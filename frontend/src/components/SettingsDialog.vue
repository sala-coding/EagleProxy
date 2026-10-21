<script lang="ts">
import {THEME_REMAP} from '../lib/theme';

// --- settings ---

// The last read of the settings file, at this module's scope so it outlives the
// dock: the next open paints from it before the wire has answered (see the
// dock's own obj state, and the note there about what a second open used to
// do). Written only from a fresh read, so a file edited by hand is what the
// fields settle to; and it holds the stamped copy — theme numbers already
// rearranged — because that is what the last open's fields really showed.
// （类型即 Dict = Record<string, unknown>；导出留在模块作用域，供 App 预热。）
export let settingsCache: Record<string, unknown> | null = null;

// The stamp the last open used to apply in line: the file is lifted to the
// current layout (old theme number through the remap, the stamp written in
// with the rest) the moment it is read. Lifting is now shared — the startup
// warm-up and the dock's own open both read the same file, and both would
// otherwise carry the same private half of the remap.
export const stampLayout = (s: Record<string, unknown>): Record<string, unknown> => {
    if (s.theme_schema !== '2') {
        const raw = Number(s.theme ?? 0);
        s.theme = String(THEME_REMAP[raw] ?? raw);
        s.theme_schema = '2';
    }
    return s;
};

// App 的启动预热：读到的设置经同一个 stamp 加工后放入缓存，让下一次打开
// 设置面板在应答回来之前就能画出来（原来是对模块级 settingsCache 的直接
// 赋值；拆分后缓存归本模块，经这个入口写入，语义不变）。
export function warmSettingsCache(s: Record<string, unknown>) {
    settingsCache = stampLayout(JSON.parse(JSON.stringify(s)));
}
</script>

<script setup lang="ts">
import {computed, onBeforeUnmount, onMounted, ref, watch, type CSSProperties} from 'vue';
import * as API from "../../wailsjs/go/main/App";
import {setting} from "../../wailsjs/go/models";
import {t} from "../i18n";
import {tip} from "../tips";
import JsonForm from "../JsonForm.vue";
import Modal from "./Modal.vue";
import {applyRoute, errText} from "../lib/shared";
import {rsCategoryOf, type Dict} from "../lib/metrics";
import {clampDockHeight, DOCK_MIN_H} from "../lib/layout";
import {buildCustomPalette, readCustomPalette, scaleFromIndex, scaleIndex, THEMES, TONE_GROUNDS, TONE_HUES, UI_SCALE_CELLS, UI_SCALE_MAX, UI_SCALE_MIN, wearCustom} from "../lib/theme";
import {FRAME_KEYS, GROUP_COLS, GROUP_FULL_ROW, GROUP_IN_STARTUP, LANGUAGES, rememberLanguage, ROUTING_SLIDE, SETTING_GROUPS, SETTING_HIDDEN, SETTING_SECTIONS} from "../lib/settings-meta";
import TunCard from "./settings/TunCard.vue";
import RoutingCard from "./settings/RoutingCard.vue";
import DnsCard from "./settings/DnsCard.vue";
import RuleSetsCard from "./settings/RuleSetsCard.vue";
import SegmentedSwitch from "./SegmentedSwitch.vue";
import {FolderOpenFilled} from '../lib/icons';

// React -> Vue 迁移说明：
// - onClose/notify/onLanguage 改为事件（close/notify/language）；
//   uiScale/theme/egress 是受控值，映射为 defineModel（v-model:ui-scale 等，
//   事件名 update:uiScale / update:theme / update:egress）。
// - onChangeSettings 会被 await、onDnsLatency 是函数式更新（往 App 已有的
//   map 里合并），二者保留为函数 prop，名字不变。
// - 其余 useState -> ref，useMemo -> computed，useEffect/useLayoutEffect ->
//   watch/onMounted/onBeforeUnmount，useRef -> 模板 ref 或普通变量，
//   createPortal -> Teleport，children/extraRows -> 插槽。
const props = defineProps<{
    onChangeSettings: () => Promise<void>;
    // A count of the times the routing profile on disk has been rewritten behind this card
    // — the smart pick's timer does it (see App). Handed down rather than watched, because a
    // file has no way to tell anyone it changed.
    routeTick: number;
    // What the last round measured, and the way to publish a round made from in here: the
    // card's own button measures too, and the ranking belongs to the app rather than to
    // whichever copy of the profile is on screen.
    dnsLatency: Record<string, number>;
    // A setter rather than a plain callback: a round publishes into the map that is already
    // there, and the two tables' buttons can be pressed one after the other without the
    // second round's reading of it overwriting the first's numbers.
    onDnsLatency: (set: (prev: Record<string, number>) => Record<string, number>) => void;
    // Whether a node is up right now. The DNS card reads it to say why the foreign half of
    // its table is empty: that half is measured through the tunnel (see App.TestDNS), so
    // until something is running there is nothing for it to be measured through. Handed
    // down for the same reason tunOn is — the app owns that state.
    nodeOn: boolean;
    // Whether TUN is the mode in place right now. Only the TUN card reads it, and only to
    // say which of its rows are in force: the builder writes them for a tun front-end or
    // not at all (see BuildVPNConfig in the Go side). Handed down rather than read here
    // because the two rail switches own that state and a second copy could disagree.
    tunOn: boolean;
    // Which card the caller opened this dialog for. The rail's routing button asks for
    // the routing card by name rather than for the dialog in general, and the card is
    // scrolled to rather than left below the fold.
    focus?: 'routing';
    // The size a fresh install opens at — the system's own text scale, clamped
    // into the slider's range. The double click hands it back, so "default"
    // keeps meaning what it meant on first run: the machine's own size.
    systemScale: number;
}>();

// The interface size, which lives in the app rather than in this form (it is written
// straight to the root font and localStorage — see uiScale). Handed down like the
// routing request is, because the control belongs to this dialog and the state does
// not.
const uiScale = defineModel<number>('uiScale', {required: true});
// The theme is the other half of the same arrangement as the type scale: it lives
// in the app (it is an attribute on the document, see App.tsx) but the control
// belongs to this dialog, and the choice is written into the object being edited
// so that 保存 is what persists it.
const theme = defineModel<string>('theme', {required: true});
// The outbound mode and the way to change it. The routing card draws the same three
// choices the status bar does, and clicking a card there has to be the same act as
// picking the mode here — so the mode and its setter are handed down rather than
// kept in a second copy that could disagree with the bar.
const egress = defineModel<string>('egress', {required: true});

// onClose -> 'close'；notify -> 'notify'；onLanguage -> 'language'。
const emit = defineEmits<{
    close: [];
    notify: [t: string, k?: 'ok' | 'err'];
    language: [code: number];
}>();

// The language is the third of these arrangements: it lives in the app (a table behind
// i18n's t, see i18n.ts) rather than in the settings object this form edits, and it is
// the one setting that has to take effect while it is being set — a reader who cannot
// read the words on screen is exactly the reader choosing a language. So the choice is
// written into the object being edited for 保存 to persist, and handed up for the
// window to wear now (see applyLanguage in App).

// What the last open read out of the settings file, kept between opens at the
// module's own scope. The dock is a window of fields below the hand-written
// cards, and none of those fields exist until the object arrives: a reader who
// opens the dock twice in a run otherwise learns that the second window paints
// its cards one by one as the wire answers, which is a queue, not a window.
// The cache is only the first coat — the fresh read replaces it in the open's
// own effect — so an edited file is what this window ends up showing.
// Type is safe as a copy the last open made after its own rearranging.
const dataDir = ref('');
const corePath = ref('');
const obj = ref<Dict | null>(settingsCache ? JSON.parse(JSON.stringify(settingsCache)) : null);
const extraCores = ref<Record<string, string>>({});
const autoStart = ref(false);
// A version number bumped whenever the language changes: the three hand-written
// cards downstream (routing, DNS, rule sets) receive it as a prop so a language
// switch re-renders them and their t() words are re-spoken. Vue skips a child
// whose watched props it believes unchanged — the React version re-rendered
// everything on any state change, so it never had this hole.
const langTick = ref(0);
watch(() => obj.value?.language, () => { langTick.value++; });
// The custom-palette popover: open, and the two choices it edits — a hue
// (one stop of the wheel) and a ground. The stored palette feeds the open
// state its choices back, so reopening the popover shows what the reader
// last wore; nothing stored yet aims at the centre of the cool half.
const toneOpen = ref(false);
const toneHue = ref(readCustomPalette()
    ? Math.round(Number((readCustomPalette()!['--accent'] || '').match(/\((\d+),/)?.[1] || 210))
    : 210);
const toneGround = ref<'deep' | 'pale'>(readCustomPalette()?.['--custom-ground'] === 'pale' ? 'pale' : 'deep');
// The stored palette, read once per render for its own swatch: a worn one
// paints its disc in its own colours, an empty one keeps the blank uniform.
// （Vue 侧：localStorage 不是响应式的，以 toneOpen 为依赖在弹层开合时重读
// —— 保存调色板必然关掉弹层，与 React 每次渲染重读的可见效果一致。）
const customPal = computed(() => {
    void toneOpen.value;
    return readCustomPalette();
});
// The cell the palette panel hangs from — the custom cell of whichever bar it was
// pressed at — and the panel itself. The panel is fixed to the viewport and placed by
// hand, because both seats this card can sit in clip their overflow — a panel
// positioned as a child of the bar would be cut off at the card's edge, which is what
// the dock's own scroll would do to it. The cell is state rather than a ref because
// moving the panel to the other bar's cell is a change the placement has to answer.
const toneAnchor = ref<HTMLElement | null>(null);
const tonePop = ref<HTMLDivElement | null>(null);
// Measured before it is placed, the same rule the two context menus follow: the panel
// hangs under the cell it came out of, flips above it when the window's floor is
// closer than the panel is tall, and is kept inside either edge of the window.
watch([toneOpen, toneAnchor], (_, __, onCleanup) => {
    if (!toneOpen.value) return;
    const place = () => {
        const el = tonePop.value, cell = toneAnchor.value;
        if (!el || !cell) return;
        const r = cell.getBoundingClientRect();
        const w = el.offsetWidth, h = el.offsetHeight;
        // The floor is the status bar's own top edge — the one the exit panel and the
        // two menus hang from. Read rather than computed from --status-h, which a
        // compact layout or a scaled UI can leave behind.
        const bar = document.querySelector('.statusbar');
        const floor = (bar ? bar.getBoundingClientRect().top : window.innerHeight) - 8;
        const below = r.bottom + 6;
        const top = below + h <= floor ? below : Math.max(8, Math.min(r.top - h - 6, floor - h));
        const left = Math.max(8, Math.min(r.left, window.innerWidth - w - 8));
        el.style.top = `${Math.round(top)}px`;
        el.style.left = `${Math.round(left)}px`;
    };
    place();
    // The card scrolls and the window resizes, and both move the pill out from under a
    // panel that is fixed to the viewport — so both put the panel back.
    window.addEventListener('resize', place);
    window.addEventListener('scroll', place, true);
    onCleanup(() => {
        window.removeEventListener('resize', place);
        window.removeEventListener('scroll', place, true);
    });
    // the cell too: pressing the other bar's cell while the panel is open moves it,
    // which is a change of the same kind as opening it
}, {flush: 'post'});
// A press anywhere but the cell and its panel puts the dropdown away; the cell's own
// press is left to its handler, which closes it or moves it to the other bar (see the
// onClick above). Nothing here closes the seat itself — a press outside that is already
// the dock's and the sheet's way out.
watch(toneOpen, (open, _, onCleanup) => {
    if (!open) return;
    const onDown = (e: PointerEvent) => {
        const el = e.target as HTMLElement | null;
        if (el?.closest('.tone-pop, .theme-custom')) return;
        toneOpen.value = false;
    };
    // Escape puts the palette away as well, and it stops here: this popover had
    // no key of its own, so a reader who opened the hue picker, changed their
    // mind and pressed Escape lost the whole settings dock instead of the
    // popover. Captured, like the other layers inside this panel, so the dock's
    // own window listener never sees the press (see hotkeyClash).
    const onKey = (e: KeyboardEvent) => {
        if (e.key !== 'Escape') return;
        e.stopPropagation();
        toneOpen.value = false;
    };
    document.addEventListener('pointerdown', onDown, true);
    document.addEventListener('keydown', onKey, true);
    onCleanup(() => {
        document.removeEventListener('pointerdown', onDown, true);
        document.removeEventListener('keydown', onKey, true);
    });
});

// Saving derives the whole palette from the two choices, keeps it on this
// machine, and wears it at once — the number the settings file will get is
// the custom entry's own place in THEMES, like any other theme's.
const saveTone = () => {
    const pal = buildCustomPalette(toneHue.value, toneGround.value);
    try { window.localStorage.setItem('custom-palette', JSON.stringify(pal)); } catch { /* private mode keeps its silence: this palette lives for the run */ }
    theme.value = 'custom';
    // Already wearing this theme: nothing changes state, so the re-derived
    // set has to land by hand.
    wearCustom();
    if (obj.value) obj.value = {...obj.value, theme: String(THEMES.findIndex(x => x.id === 'custom'))};
    toneOpen.value = false;
};

// --- the theme bars ---
// Each tone row is the type scale's control with the palettes in the cells, so it is
// driven the same three ways: press and drag along the bar to the palette under the
// pointer, step through it with the arrow keys, double-click for the default. A theme
// is chosen by looking at it, which is why a press goes straight to what is under the
// pointer rather than to a neighbouring step.
// The index is the one THEMES is addressed by, because that is the number the settings
// file holds; the bars are cut out of the same list, so a cell knows both.
type ThemeCell = {th: typeof THEMES[number]; i: number};
const TONES = ['cool', 'warm'] as const;
// The bar's own nine, in the order the settings file counts
// them: every cell keeps the index into THEMES it stands
// for, and the bar knows which of them — if any — is worn.
const toneList = (tone: typeof TONES[number]): ThemeCell[] =>
    THEMES.map((th, i) => ({th, i})).filter(({th}) => th.tone === tone);
const toneAt = (tone: typeof TONES[number]): number =>
    toneList(tone).findIndex(({th}) => th.id === theme.value);

const pickTheme = (i: number) => {
    const at = Math.max(0, Math.min(THEMES.length - 1, i));
    const th = THEMES[at];
    if (theme.value === th.id) return;
    theme.value = th.id;
    // Both at once: the window changes now, and the object being edited carries the
    // number so that 保存 is what writes it to the settings file.
    if (obj.value) obj.value = {...obj.value, theme: String(at)};
};

// Which cell the pointer is over — the same arithmetic the cells are drawn with,
// rounded rather than floored for the reason the type scale gives: the nearest cell is
// the one being pointed at, and flooring would make a cell answer only over its
// right-hand edge (see onScalePointer).
//
// The cell is clamped to the bar's own two ends. A drag that leaves the bar is the
// ordinary way a pointer ends up outside it: the capture the press takes (see
// onThemeDown) keeps the moves coming after the pointer has left, and the pointer
// leaves whenever the reader drags past the end. Without the clamp the arithmetic
// reaches past the list and picks out nothing at all, and taking a field off a
// nothing is the one thing that cannot be recovered from — the whole window goes
// down with it. The type scale's own arithmetic has the same shape and is safe for
// the same reason: its value is clamped into the range (see scaleFromIndex).
const onThemePointer = (e: PointerEvent, list: ThemeCell[]) => {
    const r = (e.currentTarget as HTMLDivElement).getBoundingClientRect();
    if (r.width <= 0) return;
    const at = Math.max(0, Math.min(list.length - 1,
        Math.round((e.clientX - r.left) / r.width * (list.length - 1))));
    pickTheme(list[at].i);
};

// The keyboard half: one cell per press, Home and End for the two ends. A bar whose
// current palette is not in it — the other tone's, or the reader's own — starts from
// the end the key is walking away from rather than refusing to move.
const onThemeKey = (e: KeyboardEvent, list: ThemeCell[]) => {
    const back = e.key === 'ArrowLeft' || e.key === 'ArrowDown';
    const fwd = e.key === 'ArrowRight' || e.key === 'ArrowUp';
    if (!fwd && !back && e.key !== 'Home' && e.key !== 'End') return;
    e.preventDefault();
    if (e.key === 'Home') { pickTheme(list[0].i); return; }
    if (e.key === 'End') { pickTheme(list[list.length - 1].i); return; }
    const at = list.findIndex(({th}) => th.id === theme.value);
    const cur = at < 0 ? (back ? list.length : -1) : at;
    pickTheme(list[Math.max(0, Math.min(list.length - 1, cur + (back ? -1 : 1)))].i);
};

const onThemeDown = (e: PointerEvent, tone: typeof TONES[number]) => {
    // capture keeps the moves coming after the pointer
    // leaves the bar, which is what makes dragging
    // across it feel held rather than chased; the
    // browser may refuse, and the drag still works
    // without it
    try {
        (e.currentTarget as HTMLDivElement).setPointerCapture(e.pointerId);
    } catch { /* the drag runs without it */ }
    onThemePointer(e, toneList(tone));
};

const onThemeMove = (e: PointerEvent, tone: typeof TONES[number]) => {
    // dragging is the point of a control shaped like this, so
    // the moves are followed while the button is held
    if (e.buttons) onThemePointer(e, toneList(tone));
};

// The reader's own palette, as the bar's tenth cell rather than a button standing beside
// them: it belongs to both bars, and a cell is what says so. It wears what a cell wears
// — its own ground with its accent as the dot, and the wheel the panel offers until
// there is a palette to paint. It opens the panel instead of switching on the spot: its
// theme is not known until the two choices in it are made, and a save is what wears it.
const onCustomClick = (e: MouseEvent) => {
    // The cell it was pressed at, so the panel
    // hangs from the one the reader reached for.
    // Pressing the other bar's cell while it is
    // open moves it rather than closing it:
    // there are two ways to this panel, and only
    // the one it already hangs from closes it.
    const same = toneAnchor.value === e.currentTarget;
    toneAnchor.value = e.currentTarget as HTMLElement;
    toneOpen.value = toneOpen.value && same ? false : true;
};

// The rule sets the active profile refers to, together with what is on disk. Loaded
// here rather than read out of the settings object, because half of that answer is
// the file system rather than the settings file.
const ruleSets = ref<{tag: string; url: string; needed: boolean; local: boolean; size: number; modified: string; country: string}[]>([]);
// Which of them have a newer copy at their source than the one on disk, as the last
// check said. Checked beside every read of the list: the card is the only place a
// stale rule set is visible, so the check rides the same refresh.
const rsUpdates = ref<Record<string, boolean>>({});
const rsBusy = ref(false);
// The card is cut into bars by category — 中国 (CN), one bar each — because the add
// button belongs to a category: rules picked from a bar's own button are filed under
// that bar's country without the reader spelling it out every time. Derived from the
// list rather than stored, so a country is a bar exactly as long as something in the
// card belongs to it; with nothing on the card at all there is the one bar, nameless,
// whose button asks in the dialog as it always did.
const rsCats = computed(() => {
    if (ruleSets.value.length === 0) return [{key: '', label: '', code: ''}];
    const seen: {key: string; label: string; code: string}[] = [];
    for (const rs of ruleSets.value) {
        const cat = rsCategoryOf(rs);
        if (!seen.some(s => s.key === cat.key)) seen.push(cat);
    }
    seen.sort((a, b) => a.key === '' ? 1 : b.key === '' ? -1 : a.label.localeCompare(b.label));
    return seen;
});
// The import the reader is part-way through: the files the native picker handed back,
// and the country they have been — or are about to be — asked for. null is no import
// in progress, which is also what closes the small window.
const rsAdd = ref<string[] | null>(null);
const rsCountry = ref('');
// The inbound port's probe and what it last said. Held rather than read on every
// render, because opening a listener to answer a question is an action rather than a
// display.
const portNote = ref('');
const portOk = ref(true);
const portBusy = ref(false);

// Rule sets are the one part of routing that fails without saying so: a .srs that
// never downloads is a rule that quietly never matches, and the core reports nothing.
// Reading the list back after every action is what keeps the card honest — the
// download lands in the same directory the reader can open beside it.
const refreshRuleSets = async () => {
    try {
        ruleSets.value = await API.RuleSets();
    } catch {
        ruleSets.value = [];
    }
    // The freshness check is the network question the list itself is not: it can
    // fail on the same networks the downloads fail on, and its failure says nothing
    // about the list — the buttons stand down, the rows stay.
    try {
        rsUpdates.value = await API.RuleSetUpdates() ?? {};
    } catch {
        rsUpdates.value = {};
    }
};

const downloadRuleSet = async (tag?: string) => {
    rsBusy.value = true;
    try {
        if (tag) {
            await API.DownloadRuleSet(tag);
            emit('notify', tip('ruleset.downloaded', tag));
        } else {
            const n = await API.DownloadMissingRuleSets();
            emit('notify', n > 0 ? tip('ruleset.downloadedAll', n) : tip('ruleset.nothingMissing'));
        }
        await refreshRuleSets();
    } catch (e) {
        emit('notify', errText(e), 'err');
    } finally {
        rsBusy.value = false;
    }
};

const removeRuleSet = async (tag: string) => {
    try {
        await API.RemoveRuleSet(tag);
        emit('notify', tip('ruleset.removed', tag));
        await refreshRuleSets();
    } catch (e) {
        emit('notify', errText(e), 'err');
    }
};

// Adding rules of one's own. The picker takes several files at a time, because a
// country's rules rarely come as one file and making the round trip per file is
// making it once for each file the reader meant to add together. The country is then
// asked for once, of the whole batch: it is the one thing the reader knows about all
// of them that the file names do not say (see App.ImportRuleSets, and the fold's name
// that reads it back). Cancelling the picker is not an import and opens nothing;
// cancelling the window writes nothing.
const pickRuleSetFiles = async (defaultCountry = '') => {
    try {
        const paths = await API.PickRuleSetFiles();
        if (!paths || paths.length === 0) return;
        // The bar the press came from knows which country its rules bite in, and
        // that is the answer the country field starts with — typed once rather than
        // asked every time. The field stays editable, because the reader may hold
        // files of two countries under one hand.
        rsCountry.value = defaultCountry;
        rsAdd.value = paths;
    } catch (e) {
        emit('notify', errText(e), 'err');
    }
};

const importRuleSets = async () => {
    if (!rsAdd.value) return;
    rsBusy.value = true;
    try {
        const n = await API.ImportRuleSets(rsAdd.value, rsCountry.value.trim());
        emit('notify', tip('ruleset.imported', n));
        rsAdd.value = null;
        await refreshRuleSets();
    } catch (e) {
        emit('notify', errText(e), 'err');
    } finally {
        rsBusy.value = false;
    }
};

// The inbound port. A port held by another program shows up as "the node will not
// start", with nothing in that message pointing at the number — so the number can be
// asked about directly, and a free one picked without leaving the dialog.
const checkInboundPort = async () => {
    portBusy.value = true;
    try {
        // The pair standing in the fields, not the one in the file: what is being
        // asked about is what the reader typed, and the file catches up 650ms later
        // (see CheckInboundPort on the Go side). The answer names the pair it is
        // about, so the note can never be read as being about another number.
        const st = await API.CheckInboundPort(String(obj.value?.inbound_address ?? ''),
            Number(obj.value?.inbound_socks_port ?? 0));
        const about = t('port.about', st.address, st.port);
        portOk.value = st.free || st.ours;
        portNote.value = st.free ? t('port.available') + about
            : st.ours ? t('port.ours') + about
                : t('port.used') + (st.error || '');
    } catch (e) {
        portOk.value = false;
        portNote.value = errText(e);
    } finally {
        portBusy.value = false;
    }
};

const pickFreePort = async () => {
    portBusy.value = true;
    try {
        const p = await API.FreeInboundPort();
        if (obj.value) obj.value = {...obj.value, inbound_socks_port: p};
        portOk.value = true;
        portNote.value = `${t('port.picked')} ${p}`;
    } catch (e) {
        portOk.value = false;
        portNote.value = errText(e);
    } finally {
        portBusy.value = false;
    }
};

// A captured combination that could not be recorded, and why: another row already
// holds it, or Windows would not take it. Told apart because the reader can act on
// one of the two.
const hotkeyClash = ref<{combo: string; owner: string | null} | null>(null);

// Escape belongs to that dialog while it is up, not to the sheet behind it. Both are
// listeners on the window, and the sheet's was registered on the way in — so this one
// asks for the capture phase, which is what runs first.
watch(hotkeyClash, (clash, _, onCleanup) => {
    if (!clash) return;
    const onKey = (e: KeyboardEvent) => {
        if (e.key !== 'Escape') return;
        e.stopPropagation();
        hotkeyClash.value = null;
    };
    window.addEventListener('keydown', onKey, true);
    onCleanup(() => window.removeEventListener('keydown', onKey, true));
});

// The raw object cut into the subjects SETTING_GROUPS names, in that order, with
// everything the table does not know about collected into one last group. Built
// from the object rather than from the table alone, so a key the file does not
// have simply makes no row — instead of an empty card or a row of nothing.
const jsonGroups = computed(() => {
    if (!obj.value) return [];
    const named = new Set(SETTING_GROUPS.flatMap(g => g.keys));
    const out: {title: string; value: Dict; cols?: number; fullRow?: string[]; note?: string}[] = [];
    for (const g of SETTING_GROUPS) {
        const sub: Dict = {};
        for (const k of g.keys) if (k in obj.value) sub[k] = obj.value[k];
        if (Object.keys(sub).length > 0) {
            out.push({title: g.title, value: sub, cols: GROUP_COLS[g.title],
                      fullRow: GROUP_FULL_ROW[g.title], note: g.note});
        }
    }
    const rest: Dict = {};
    for (const k of Object.keys(obj.value)) {
        if (!named.has(k) && !SETTING_HIDDEN.has(k)) rest[k] = obj.value[k];
    }
    if (Object.keys(rest).length > 0) out.push({title: 'settings.group.misc', value: rest});
    return out;
});

// Those groups cut in two: the ones that stand as cards of their own, and the rows the
// startup card carries (see GROUP_IN_STARTUP). Kept apart because the nav is one entry
// per slide and a card's place in the carousel is its index in that list — a group
// drawn inside another card must not take a place in it, or every slide after it
// would stand one step away from where the carousel thinks it is.
const jsonSlides = computed(
    () => jsonGroups.value.filter(g => g.title !== GROUP_IN_STARTUP));
const startupRows = computed(
    () => jsonGroups.value.filter(g => g.title === GROUP_IN_STARTUP));

// The two frames of one card, named by what the card holds (see FRAME_KEYS). Read
// here rather than at the call sites so the words track the language like
// everything else on the sheet does.
const framesOf = (title: string) => {
    const keys = FRAME_KEYS[title];
    if (!keys) return undefined;
    return {
        text: keys.text ? t(keys.text) : undefined,
        switch: keys.switch ? t(keys.switch) : undefined,
    };
};

// The type-scale track is a row of equal cells, so the pointer's choice is which
// cell it is over — the same arithmetic the cells are drawn with, which is what
// keeps the lit run ending under the pointer. Rounded rather than floored: the
// nearest cell is the one being pointed at, and flooring would make a cell answer
// only over its right-hand edge.
const onScalePointer = (e: PointerEvent) => {
    const r = (e.currentTarget as HTMLDivElement).getBoundingClientRect();
    if (r.width <= 0) return;
    uiScale.value = scaleFromIndex(Math.round((e.clientX - r.left) / r.width * (UI_SCALE_CELLS - 1)));
};

// The keyboard half of the same control: one cell per press, Home and End for the
// two ends. A control drawn as cells that could only be driven by a pointer would
// be one half the readers cannot use at all.
const onScaleKey = (e: KeyboardEvent) => {
    const i = scaleIndex(uiScale.value);
    if (e.key === 'ArrowRight' || e.key === 'ArrowUp') {
        e.preventDefault();
        uiScale.value = scaleFromIndex(i + 1);
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') {
        e.preventDefault();
        uiScale.value = scaleFromIndex(i - 1);
    } else if (e.key === 'Home') {
        e.preventDefault();
        uiScale.value = UI_SCALE_MIN;
    } else if (e.key === 'End') {
        e.preventDefault();
        uiScale.value = UI_SCALE_MAX;
    }
};

const onScaleDown = (e: PointerEvent) => {
    // capture keeps the moves coming after the pointer
    // leaves the rail, which is what makes dragging across
    // it feel held rather than chased; the browser may
    // refuse, and the drag still works without it
    try {
        (e.currentTarget as HTMLDivElement).setPointerCapture(e.pointerId);
    } catch { /* the drag runs without it */ }
    onScalePointer(e);
};

const onScaleMove = (e: PointerEvent) => {
    // dragging is the point of a control shaped like this, so
    // the moves are followed while the button is held
    if (e.buttons) onScalePointer(e);
};

onMounted(() => { (async () => {
    // GetSettings alone is what the fields below the hand-written cards read
    // from, so it is the one call the dock actually waits for. The other
    // three reads (and the rule-set scan of disk, which is the slowest thing
    // here by far) start in the same breath and land whenever they land —
    // their cards are slides of their own, and a slow answer only paints
    // those slides late, not the fields the whole dock is judged by.
    const others = Promise.all([
        API.GetDataDir(),
        API.GetCorePath(),
        API.GetAutostart(),
    ]).then(([dir, core, start]) => {
        dataDir.value = dir;
        appliedDataDir.value = dir;
        corePath.value = core;
        autoStart.value = start;
    }).catch(e => { console.error('settings dock prefetch failed:', e); });
    let s: Dict;
    try {
        s = (await API.GetSettings()) as Dict;
    } catch (e) {
        // Nothing below can save anything: the watch list below is gated on the
        // load for exactly this reason (a diff against a baseline that was never
        // taken would write the file back over itself). Said out loud instead of
        // leaving a panel whose fields look editable and are not.
        emit('notify', errText(e), 'err');
        return;
    }
    // Any, and deliberately: the settings file is the backend's own shape, read
    // with plain key access throughout the dock. Stamping routes the copy through
    // a typed rim (Record<string, unknown>) only to be honest about what the remap
    // touches; letting the rim spread from here would have every field below paint
    // the file's keys unknown.
    const o: Record<string, any> = stampLayout(JSON.parse(JSON.stringify(s)));
    settingsCache = o;
    obj.value = o;
    // The watch list below runs only once the load has landed — its first run is
    // what takes the baseline — and this is the line that opens it. Without it the
    // whole panel was a form that never saved: every edit came and went with the
    // window, with nothing on screen to say so.
    loaded.value = true;
    try {
        extraCores.value = o.extraCore?.core_map ? JSON.parse(o.extraCore.core_map) : {};
    } catch {
        extraCores.value = {};
    }
    // The rule sets arrive on their own — their card is on another slide, and
    // a slow scan of disk must not hold the fields back.
    void refreshRuleSets();
    await others;
})(); });

// What 保存 used to do, without the round of applause: it writes the panel's own
// state back to its homes and lets the backend re-read the settings. Silent, and
// it never closes the panel — it is the checkout, not the way out.
const persist = async () => {
    // The data directory is not written here at all: it is committed when its
    // field is left (see commitDataDir). A debounced write of a text field the
    // reader is mid-way through typing is a store switch per pause.
    // Two side-writes, each kept apart from the settings file below: a core path the
    // system refuses, or an autostart entry a policy blocks, used to throw before the
    // write that actually carries the panel's fields — the fields kept their new
    // values while the file kept the old ones, and one error toast was the only clue.
    const problems: unknown[] = [];
    if (corePath.value.trim()) {
        try {
            await API.SetCorePath(corePath.value.trim());
        } catch (e) {
            problems.push(e);
        }
    }
    try {
        await API.SetAutostart(autoStart.value);
    } catch (e) {
        problems.push(e);
    }
    if (obj.value) {
        // What this panel changed, key by key, against what the load handed it —
        // and everything else re-read from the file at this moment. Writing the
        // whole snapshot back was how a mode changed elsewhere while the panel
        // stood open (the routing card's own call, the status bar, the tray, a
        // hotkey) got silently rolled back to its value at open, file and live
        // core left disagreeing about which mode is on. The line between "this
        // panel owns it" and "it does not" cannot be drawn by SETTING_HIDDEN:
        // that set says which rows are not drawn, and the hand-written cards own
        // half of what it lists — the port pair, the egress mode, the vpn
        // switches — so sending only the changed keys is the one rule that holds
        // for both kinds.
        const before: Dict = baselineRef ?? {};
        const changed: Dict = {};
        for (const k of Object.keys(obj.value)) {
            if (JSON.stringify(obj.value[k]) !== JSON.stringify(before[k])) changed[k] = obj.value[k];
        }
        // The theme keys can never win the diff above: stampLayout wrote the
        // schema stamp into the baseline too, and the Go side drops the empty
        // value, so 'theme_schema' stayed out of the file forever — every restart
        // then ran the old-number remap over a palette that had already been
        // renumbered, landing on a different theme than the one picked (index
        // >= 9 reshuffled every time). They are this panel's own; they always go.
        changed.theme_schema = obj.value.theme_schema;
        changed.theme = obj.value.theme;
        let fresh: Dict;
        try {
            fresh = (await API.GetSettings()) as Dict;
        } catch {
            // The file could not be re-read: the snapshot is the old
            // behaviour, which is better than losing the edit entirely.
            fresh = before;
        }
        const merged: Dict = {
            ...fresh,
            ...changed,
            extraCore: {...((obj.value.extraCore as Dict) ?? {}), core_map: JSON.stringify(extraCores.value)},
        };
        await API.UpdateSettings(new setting.Setting(merged));
        // The write landed, so it is the new baseline: the next save diffs
        // against what the file now holds rather than against the open.
        baselineRef = JSON.parse(JSON.stringify(obj.value)) as Dict;
    }
    await props.onChangeSettings();
    // The settings file was written; only a side-write is still owed. Reported, so the
    // reader knows which one, and after the write so it cannot take the write with it.
    if (problems.length) throw problems[0];
};

// The data directory's own commit, on leaving the field. The switch is heavy —
// resolve the text, create the folder, rebuild the manager on it, stop the core
// — and a keystroke-pause is not a reader saying where it goes.
const commitDataDir = async () => {
    const want = dataDir.value.trim();
    if (!want || want === appliedDataDir.value) return;
    try {
        await API.SetDataDir(want);
        appliedDataDir.value = want;
        await props.onChangeSettings();
    } catch (e) {
        emit('notify', errText(e), 'err');
        // Back to the folder the app is really using: leaving the text standing
        // would say the switch had happened.
        dataDir.value = appliedDataDir.value;
    }
};
// Read through a ref by the unmount flush below, which is registered once and
// must not close over the first render's copy of the function.
// （Vue 侧：setup 里的函数天然稳定，不存在过期闭包，直接引用即可。）

// The panel saves itself: every edit settles for .65s (a field you are typing into
// keeps moving the frame — each keystroke restarts the wait) and is then written as
// below. There is no 保存 button for a reader to forget, because the reason it
// existed was the panel holding the changes until the reader said so, and it no
// longer holds anything.
// The first run of the effect is the load itself — the panel's own state arriving
// is every key on the watch list at once — and it is marked loaded rather than
// written back; what a reader has not edited is not a change to save.
let loadedRef = false;
let busyRef = false;
// Whether the open's own load has landed. The guard below used to read
// "dataDir and corePath are not undefined", which is true on the very first
// frame — both start as '' — so the panel marked itself loaded before the load
// arrived, took the load itself for an edit, and wrote the whole file back 650ms
// after being opened. When the prefetch failed it was worse than noisy: the
// write failed on the empty data directory, the rest of that write never ran,
// and every later edit in the session failed the same way while the fields went
// on showing values that were never saved.
const loaded = ref(false);
// The data directory as the backend last had it. This field is not debounced
// like the others: typing an absolute path pauses for a keystroke, the debounce
// fires, and each pause lands as a new store — the backend resolves the
// half-typed text, creates that folder, reloads its manager on it and stops the
// core. Committed when the field is left instead (see commitDataDir), which is
// also the moment the reader has finished saying where it goes.
const appliedDataDir = ref('');
// The settings as the load handed them over: what this panel changed is the
// difference between that and the state it is about to write (see persist).
let baselineRef: Dict | null = null;
// What the debounce still owes the file. Two things read it: closing the panel inside
// the .65s (the timer dies with the panel, so requestClose writes it instead — see
// flushOnClose), and an edit that lands while a write is in flight, which the write
// takes on as soon as it has landed rather than dropping on the floor the way the old
// busy guard did.
let dirtySettings = false;
// The routing profile's own debt, kept beside this one so the exit below can see both
// (see the DNS card's debounced write, further down).
let routePending = false;
// What the file was told last, as text rather than as an object reference: the panel's
// state is a new object on every keystroke, and the load hands back a new object too,
// so identity says nothing about whether anything changed. Comparing what it says is
// what keeps a re-read (the dev mode's double mount, the reload that follows a write)
// from being taken for an edit and written back over the file.
// The watch list: what this panel owns and edits. The data directory is not on
// it — it has its own commit (commitDataDir) — because a debounce over a path
// being typed is a store switch per keystroke-pause.
const settingsSignature = () => JSON.stringify([obj.value, autoStart.value, corePath.value, extraCores.value]);
let writtenSettings: string | null = null;
const saveSettings = () => {
    if (busyRef) return;
    const writing = settingsSignature();
    if (writing === writtenSettings) {
        dirtySettings = false;
        return;
    }
    busyRef = true;
    dirtySettings = false;
    (async () => {
        try {
            await persist();
            writtenSettings = writing;
        } catch (e) {
            emit('notify', errText(e), 'err');
        } finally {
            busyRef = false;
            if (dirtySettings) saveSettings();
        }
    })();
};
// （saveSettingsRef / commitDataDirRef / writeRouteRef 这三个 React 转发 ref
// 在 Vue 里不需要：setup 函数引用是稳定的，卸载钩子直接调用即可。）
watch([obj, autoStart, corePath, extraCores, loaded], (_, __, onCleanup) => {
    if (!loaded.value) return;
    if (!loadedRef) {
        loadedRef = true;
        // The load is not an edit: remember what arrived — as the signature the
        // watch list is compared against, and as the baseline persist diffs
        // against — and let only a later difference count as something owed.
        writtenSettings = settingsSignature();
        baselineRef = JSON.parse(JSON.stringify(obj.value ?? {})) as Dict;
        return;
    }
    if (settingsSignature() === writtenSettings) return;
    dirtySettings = true;
    const timer = window.setTimeout(saveSettings, 650);
    onCleanup(() => window.clearTimeout(timer));
});
// The last way an edit could be lost: the panel taken away without its own exit ever
// running — the mark in the bar toggles it out of existence, and so does the rail. The
// debt is written on the way out whichever door was used; the refs are what keep this
// from writing a stale copy of the settings (both are refreshed every render), and the
// flags are what keep it from writing twice when requestClose already settled up.
onBeforeUnmount(() => {
    if (dirtySettings) saveSettings();
    if (routePending) {
        routePending = false;
        void writeRoute();
    }
    // And the data directory, in case the field was never left: typed at the
    // last moment, or the panel closed with the pointer still in it.
    void commitDataDir();
});

const pickCore = async () => {
    try {
        const p = await API.PickCoreFile();
        if (p) corePath.value = p;
    } catch (e) {
        emit('notify', errText(e), 'err');
    }
};

const coreKeys = computed(() =>
    Array.from(new Set(['naive', 'hysteria2', 'tuic', ...Object.keys(extraCores.value)])));

// --- routing ---
// One profile, not a list of them. The four cards below are the four things this
// card is for, and three of them are the egress mode rather than a routing profile
// at all — so there is nothing here to create, rename, enable or delete. What is
// left of the old list is the file behind it, whose name the Go side keeps in
// ActiveRouting; the reader never picks that name, so it is never shown.
const activeRoute = ref('');
const routeForm = ref<any>(null);
// Which card's settings are on screen. Deliberately not the egress mode: the block
// card is a layer rather than a direction, so it has settings but switches nothing,
// and keeping the two apart is what stops "open the block card" from meaning
// "change how traffic flows".
// Which routing card was last being read, remembered across opens the way the dock's
// height is: a reader who closes the panel on 规则集 and reopens it to change one
// resolver should not have to walk back through the cards to get there.
const routeFocus = ref((() => {
    try { return localStorage.getItem('settings-route-card') || 'rule'; } catch { return 'rule'; }
})());
watch(routeFocus, () => {
    try { localStorage.setItem('settings-route-card', routeFocus.value); } catch { /* private mode keeps its silence */ }
});
// Which slide the carousel is showing. Every slide stays mounted and the track moves
// under them, so a reader who steps away from a card and comes back finds it where
// they left it — its scroll position included, which rebuilding the slides would
// lose. Where the panel was left at all is remembered once the settings have arrived
// (see settings-slide, below navTitles).
const slide = ref(0);

// The dock's own exit. A panel that vanishes when its close button lands is a
// hole opening in the bar; the .closing class plays the slide back into the bar
// (see .settings-dock in App.css) and onClose takes the element away at the end
// of that run. The guard keeps a second click from restarting the animation.
const closing = ref(false);
let closeTimer: number | null = null;
// The settle: fires the real unmount. Cleared on remount and on unmount.
const requestClose = () => {
    if (closing.value) return;
    closing.value = true;
    // Nothing the reader changed is left behind by the exit. Both cards write on a
    // timer, and a timer is cleared when its panel goes away — so an edit made in the
    // last half second before the close would otherwise have been the one thing on the
    // panel that did not save itself. The animation's 240ms is the write's to spend.
    if (dirtySettings) saveSettings();
    if (routePending) {
        routePending = false;
        void writeRoute();
    }
    // In a ref so a fast remount (the panel reopened inside the 240ms) can
    // cancel the exit set by the previous one instead of racing it.
    window.clearTimeout(closeTimer ?? undefined);
    closeTimer = window.setTimeout(() => {
        closeTimer = null;
        emit('close');
    }, 240);
};
onBeforeUnmount(() => {
    window.clearTimeout(closeTimer ?? undefined);
});

// --- the dock's own height ---
// Null while the stylesheet's height still stands (600px, or the window's own where
// that is shorter); a drag sets a number, and the number is remembered so the panel
// comes back the size it was left at — the same arrangement as the log panel's
// height (see consoleH) and the drawer's width, down to the guarded read: a value
// left over from a taller window is clamped on the way in rather than trusted.
const dockH = ref<number | null>((() => {
    const saved = Number(window.localStorage.getItem('dock-height'));
    return Number.isFinite(saved) && saved > 0 ? clampDockHeight(saved, window.innerHeight) : null;
})());
const dockDragging = ref(false);
const dockRef = ref<HTMLElement | null>(null);
// Where the drag began, kept out of the state: it changes on every move and nothing
// on screen is drawn from it.
let dockDrag: { y: number; h: number } | null = null;
// The panel's height as the browser has actually laid it out, which is what the
// handle reports — right even when the height came from the stylesheet rather than
// from a drag (see the console's grip for the same).
const dockNow = ref(0);
watch([dockH, closing], () => {
    dockNow.value = Math.round(dockRef.value?.getBoundingClientRect().height ?? 0);
}, {flush: 'post', immediate: true});

// Dragging the panel's top edge. The pointer is captured by the handle, so every
// move and the release arrive there even once the pointer has left it — that is what
// makes the panel feel held rather than chased.
const onDockGripDown = (e: PointerEvent) => {
    const el = dockRef.value;
    if (!el) return;
    e.preventDefault();          // no text selection along the dragged edge
    try {
        (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    } catch { /* the drag runs without it */ }
    dockDrag = { y: e.clientY, h: el.getBoundingClientRect().height };
    dockDragging.value = true;
};

const onDockGripMove = (e: PointerEvent) => {
    const d = dockDrag;
    if (!d) return;
    // Up makes the panel taller: the edge follows the pointer, and the panel grows
    // away from the bar, which does not move.
    dockH.value = clampDockHeight(d.h + (d.y - e.clientY), window.innerHeight);
};

const onDockGripUp = (e: PointerEvent) => {
    if (!dockDrag) return;
    dockDrag = null;
    dockDragging.value = false;
    try {
        const el = e.currentTarget as HTMLElement;
        if (el.hasPointerCapture?.(e.pointerId)) {
            el.releasePointerCapture(e.pointerId);
        }
    } catch { /* nothing left to release */ }
    // Written on release rather than on every move: localStorage is synchronous and a
    // drag would otherwise hit it once per frame. What is stored is the state value,
    // not the measured one — the measured height is the stylesheet clamping this draw
    // to fit the window, and persisting that would quietly rewrite the reader's height
    // down to whatever the window happened to allow, for good.
    if (dockH.value) window.localStorage.setItem('dock-height', String(dockH.value));
};

// The same adjustment from the keyboard, because a separator you can only drag is a
// separator some people cannot use. Shift for the larger step, and Home to hand the
// height back to the stylesheet's own.
const onDockGripKey = (e: KeyboardEvent) => {
    const step = e.shiftKey ? 48 : 16;
    const cur = dockRef.value?.getBoundingClientRect().height ?? 0;
    let next: number | null = null;
    if (e.key === 'ArrowUp') next = cur + step;
    else if (e.key === 'ArrowDown') next = cur - step;
    else if (e.key === 'Home') next = null;
    else return;
    e.preventDefault();
    const h = next === null ? null : clampDockHeight(next, window.innerHeight);
    dockH.value = h;
    if (h === null) window.localStorage.removeItem('dock-height');
    else window.localStorage.setItem('dock-height', String(h));
};

// Escape is the dock's dismissal, as it was the sheet's: the key is the one the
// window's other panels answer to, and a reader who pushed the panel up wants the
// same key to put it back.
const onEscapeKey = (e: KeyboardEvent) => {
    if (e.key !== 'Escape') return;
    // A press that came from inside a dialog opened over this panel belongs to
    // that dialog: the Modal handles its own Escape and stops the event, and
    // this is the belt to that pair of braces — without it, a dialog that
    // somehow let the press through would take the dock down with it.
    if ((e.target as HTMLElement | null)?.closest?.('.modal-mask')) return;
    requestClose();
};

// A dock stands on the bar rather than covering the window, so it carries no mask:
// the pointer decides instead. Any press that does not land in the panel puts it
// back — the whole window outside the dock is the area a mask would have drawn.
// Captured, so the panels under it cannot swallow the press in a handler of their
// own; sub-layers the panel itself opens (its context menus sit inside it) read as
// inside; a Modal raised beside the dock — the hotkey-clash box — counts as part of
// the same conversation, so a reader closing that box does not lose the settings
// they were in the middle of editing.
const onDocDown = (e: PointerEvent) => {
    const el = e.target as HTMLElement | null;
    // .tone-pop is in the list because the palette dropdown is rendered at the
    // body rather than inside the dock (see the portal in the theme row): a press
    // inside it is a press inside this panel's business, not outside it.
    if (el && el.closest('.settings-dock, .modal-mask, .tone-pop')) return;
    requestClose();
};

const refreshRoutes = async () => {
    try {
        const name = (await API.GetActiveRouting()) ?? '';
        activeRoute.value = name;
        const form = name ? await API.GetRouting(name) : null;
        routeForm.value = form;
        // Whatever comes off disk is what is on disk, and the card that saves itself (see
        // the debounced effect further down) has to know that before it can tell a change
        // from a re-render — reading a profile is not an edit of it.
        writtenRoute = form ? {profile: name, text: JSON.stringify(form)} : null;
    } catch {
        routeForm.value = null;
    }
};

onMounted(() => {
    window.addEventListener('keydown', onEscapeKey);
    document.addEventListener('pointerdown', onDocDown, true);
    // Arrow keys step the carousel, but only when the reader is not inside a control: in
    // a text field the arrows belong to the caret and in a list to the list. The sheet is
    // a form first and a slide show second, so the form keeps the keys it was using.
    window.addEventListener('keydown', onArrowKey);
    refreshRoutes();
});
onBeforeUnmount(() => {
    window.removeEventListener('keydown', onEscapeKey);
    document.removeEventListener('pointerdown', onDocDown, true);
    window.removeEventListener('keydown', onArrowKey);
});

// The profile on disk has been rewritten under this card — by the smart pick's timer
// (see App). Read it again, or the card would go on showing the resolver that is no
// longer chosen, and would write that back over the pick on the next keystroke.
//
// Unless this card is holding an edit of its own. That write is debounced by 600ms
// (see below), so a round landing inside the window used to replace the field the
// reader was typing in with the file's older text: half-typed a resolver, pressed a
// hotkey in the same second — the pick's tick is what runs it — and the letters were
// gone with nothing said. The tick is held rather than dropped: the effect runs again
// when the form changes, and by then the reader's own text is the file's.
let handledRouteTick = 0;
watch([() => props.routeTick, routeForm], () => {
    if (props.routeTick === handledRouteTick) return;
    const onDisk = writtenRoute?.text ?? '';
    if (routeForm.value && JSON.stringify(routeForm.value) !== onDisk) return;
    handledRouteTick = props.routeTick;
    void refreshRoutes();
});

// A caller that asked for the routing card is put on its slide. There is no
// scrolling to do any more — the card has a slide of its own — and the carousel
// opens on the first one, which would leave the card the rail's routing button
// exists for ten slides away.
watch(() => props.focus, focus => {
    if (focus === 'routing') slide.value = ROUTING_SLIDE;
}, {immediate: true});

// One field of the profile being edited. Every control in the four cards goes
// through here, so a control only has to name the key it writes. The value is a string
// or a number: a list is text, and the sniffing mode is one of three numbers (see the
// constants in setting.go).
const editRoute = (key: string, value: string | number) => {
    routeForm.value = routeForm.value ? new setting.Routing({...routeForm.value, [key]: value}) : routeForm.value;
};

// The same write for the two switches, which the routing model carries as
// booleans rather than as the strings the lists and the DNS address are
// made of — a checkbox has no text to hand over.
const editRouteFlag = (key: string, value: boolean) => {
    routeForm.value = routeForm.value ? new setting.Routing({...routeForm.value, [key]: value}) : routeForm.value;
};

// One item appended to a list unless it is already there. A rule list is a set with a
// notation, and the same line twice is the one thing a reader cannot see.
const appendRoute = (key: string, value: string) => {
    const cur = routeForm.value;
    if (!cur) return;
    const lines = String(cur[key] ?? '')
        .split('\n')
        .map(s => s.trim())
        .filter(s => s !== '');
    if (lines.includes(value)) return;
    lines.push(value);
    routeForm.value = new setting.Routing({...cur, [key]: lines.join('\n')});
};

// What the profile on disk last said, and which profile said it. The form is a new
// object on every keystroke, so comparing references says nothing; comparing the text is
// what tells a change from a re-render, and the name beside it is what tells the profile
// the text belongs to — another profile's form must never be written against this one.
let writtenRoute: {profile: string; text: string} | null = null;

// Writing the routing profile, always quietly: the reader never asks for this any
// more — every edit in the card settles into it on its own (see the effect below) —
// so there is no request left to report. A toast per keystroke would be the app
// talking about itself rather than answering the reader.
//
// Putting what is on screen to work. The text comes from the same list the panel
// draws, so what lands in the file is exactly what was being read — category, search
// and order included — and ApplyRouting both writes it and, with a node running,
// rebuilds the core against it at once (the backend tells the reader; the restart
// under TUN keeps the adapter up, under the proxy it is a second's gap).
const writeRoute = async () => {
    if (activeRoute.value == null || !routeForm.value) return;
    // What is being written, taken before the await: a change made while the write is in
    // flight must not be mistaken for one that has been saved.
    const writing = JSON.stringify(routeForm.value);
    try {
        await applyRoute(activeRoute.value, routeForm.value);
        writtenRoute = {profile: activeRoute.value, text: writing};
    } catch (e) {
        emit('notify', errText(e), 'err');
    }
};
// Held in a ref for the reason requestClose is (see Modal): the effect below depends on
// the form alone, which is what changes, and this is rebuilt on every render.
// （Vue 侧：函数引用稳定，直接调用。）

// The DNS card writes itself: a row picked or a switch flipped is a decision already
// made, and a URL is made when the typing stops. It had a button, and that button was the
// one thing in the card that could be forgotten — a resolver set but not saved is a
// resolver the core never sees.
//
// Debounced, because a URL arrives one character at a time and each character would be a
// write of the whole profile; the last one is the one that matters.
//
// routePending is the same debt as dirtySettings above: a profile edited and the panel
// closed inside the .6s would otherwise lose the edit to the cleanup below, so
// requestClose writes it before the panel leaves (see flushOnClose).
watch([routeForm, activeRoute], (_, __, onCleanup) => {
    if (!routeForm.value || activeRoute.value == null) return;
    const now = JSON.stringify(routeForm.value);
    // The file as it came off disk, or a profile switched to since: there is nothing to
    // write back, and this is where the form it came from is remembered.
    if (writtenRoute?.profile !== activeRoute.value) {
        writtenRoute = {profile: activeRoute.value, text: now};
        return;
    }
    // The text rather than the object: the form is a new object on every keystroke, so
    // comparing references says nothing — and comparing what it says is also what keeps
    // a second click on the row already chosen from writing the file again.
    if (now === writtenRoute.text) return;
    routePending = true;
    const timer = window.setTimeout(() => {
        routePending = false;
        void writeRoute();
    }, 600);
    onCleanup(() => window.clearTimeout(timer));
});

// One nav entry per slide: the hand-written cards by the titles listed above, then
// the raw-settings groups in the order jsonGroups put them in. Both are i18n keys,
// so the nav is the same list of names the cards' own titles carry.
const navTitles = computed(
    () => [...SETTING_SECTIONS, ...jsonSlides.value.map(g => g.title)]);

// The remembered place, held as the i18n key the nav and the card's own title both
// carry rather than as a number: the raw-settings cards after the hand-written ones
// are whatever the settings file happens to contain, so a number would point at a
// different card the moment one of those arrived or left. It is applied once the
// settings have arrived — before that the list of cards is only half its length, and
// a key for a later card would find no place to go — and it steps aside for a card
// asked for from outside (the rail's routing button), which is an intention rather
// than a habit.
let slideRestored = false;
watch([obj, navTitles, () => props.focus], () => {
    if (slideRestored || !obj.value) return;
    slideRestored = true;
    if (props.focus) return;
    try {
        const want = navTitles.value.indexOf(localStorage.getItem('settings-slide') ?? '');
        if (want > 0) slide.value = want;
    } catch { /* private mode keeps its silence: the panel opens on the first card */ }
});
watch([slide, navTitles], () => {
    if (!slideRestored) return;
    try { localStorage.setItem('settings-slide', navTitles.value[slide.value] ?? ''); } catch { /* as above */ }
});

// The rule-set folds' open hands, remembered too: which of the categories' lists the
// reader wants on screen is a habit, not a per-visit decision. Per category rather
// than one hand for all: the card is organised by where the rules bite in, and a
// reader after the Russian list should not have to see the Chinese one open because
// they were reading it yesterday. A category the record does not name is closed.
const rsCatsOpen = ref<Record<string, boolean>>((() => {
    try { return JSON.parse(localStorage.getItem('settings-rscats') ?? '{}') ?? {}; } catch { return {}; }
})());
const toggleRsCat = (key: string) => {
    const next = {...rsCatsOpen.value, [key]: !rsCatsOpen.value[key]};
    try { localStorage.setItem('settings-rscats', JSON.stringify(next)); } catch { /* as above */ }
    rsCatsOpen.value = next;
};

const stepSlide = (d: number) => {
    slide.value = Math.max(0, Math.min(navTitles.value.length - 1, slide.value + d));
};

// Arrow keys step the carousel, but only when the reader is not inside a control: in
// a text field the arrows belong to the caret and in a list to the list. The sheet is
// a form first and a slide show second, so the form keeps the keys it was using.
const onArrowKey = (e: KeyboardEvent) => {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
    const el = e.target as HTMLElement | null;
    const tag = el?.tagName;
    if (el?.isContentEditable || tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
    stepSlide(e.key === 'ArrowRight' ? 1 : -1);
};

// A place in the carousel, by the i18n key the card's own title carries. The card is
// the slide — the sheet's children are already the cards, and a track element around
// them would push every one of them a level deeper for nothing — so each one carries
// its own position: its distance from the slide on screen, in track widths plus the
// gutter between slides. Off screen it is hidden as well as moved, because a card the
// reader cannot see is not a place to tab into; visibility flips with the move, so
// the arriving card is there to be seen while the leaving one is on its way out.
// （className/style 改为 Vue 的 class/style 键，经 v-bind 摊开。）
const slideOf = (title: string, extra = ''): {class: string; style: CSSProperties} => {
    const i = navTitles.value.indexOf(title);
    return {
        class: `set-group set-slide${extra}${i === slide.value ? ' on' : ''}`,
        style: {transform: `translateX(calc(${i - slide.value} * (100% + 24px)))`},
    };
};

// The raw-settings cards' extra class, by what they hold (see the slideOf calls).
const jsonSlideExtra = (title: string) =>
    title === 'settings.group.inbound' ? ' set-json set-inbound'
    : title === 'settings.group.core' ? ' set-json set-core'
    : title === 'settings.group.subs' ? ' set-json set-subs'
    : title === 'settings.group.test' ? ' set-json set-test'
    : title === 'settings.group.hotkeys' ? ' set-json set-hotkeys'
    : ' set-json';

// Setting a hotkey means the app stops holding one for the moment. Windows gives a
// registered combination to this process and passes nothing on, so without the pause
// the capture field would never see the press for the very combination it is showing
// — re-entering what is already there, which is the first thing anyone tries. Held in
// a useCallback because the field suspends and resumes on every focus change, and a
// new function each render would make it do so every render instead.
const onHotkeyCapture = (active: boolean) => {
    void (active ? API.SuspendHotkeys() : API.ResumeHotkeys());
};

// Whether a captured combination may be recorded, and the last word on it. Two ways
// it may not: another row already holds it — which this side can see and the field it
// was pressed in cannot — and Windows will not take it, which only trying can tell.
const acceptHotkey = async (key: string, combo: string) => {
    const rows: Dict = obj.value ?? {};
    const clash = Object.keys(rows)
        .filter(k => k !== key && k.startsWith('hk_'))
        .find(k => String(rows[k] ?? '').toLowerCase() === combo.toLowerCase());
    if (clash) {
        hotkeyClash.value = {combo, owner: clash};
        return false;
    }
    // Asked while the app's own hotkeys are suspended (see onHotkeyCapture), so none
    // of ours is in the way of the answer.
    if (!(await API.TestHotkey(combo))) {
        hotkeyClash.value = {combo, owner: null};
        return false;
    }
    return true;
};

// The 核心与日志 card's own rows — the ones its card carries that are not keys of the
// settings file: where the core and the data live is read from the app (see
// API.GetDataDir) and the paths to alternative builds are held in a map of their own.
// They are rows of that card all the same, so the card hands them to the form that
// draws it and they stand in the fields' frame with the rest (see `extraRows`), ahead
// of the JSON rows: a reader is likelier to touch a path than a log level, and a JSON
// row above these controls would bury it.
//
// The paths their own card used to hold, in the order a reader looks for them: the
// data directory (with a way into it, since the routing profiles, the rule sets and
// the logs are all in there and typing the path into an explorer is the long way
// round), the core's own path, and the alternative builds. The last ones
// keep their raw key as the label: the key is what the builder looks the path up by,
// and it is the name the reader knows from the core they are replacing.
//
// The two paths are written as the reader meant them and read as the app needs them:
// a relative one — "config", "sing-box.exe" — is taken against the folder the
// executable sits in (see resolveAppPath in the backend), so the field hands back the
// short text that was typed and the whole folder can be copied somewhere else and
// still find its own data and core.
const onDataDirKey = (e: KeyboardEvent) => {
    if (e.key === 'Enter') (e.currentTarget as HTMLInputElement).blur();
};
const openDataDir = () => { void API.OpenDataDir(); };
const setExtraCore = (k: string, e: Event) => {
    extraCores.value = {...extraCores.value, [k]: (e.target as HTMLInputElement).value};
};

// The test card's two URLs, drawn by hand rather than as generated rows: each wants
// a row of presets under it — one press writes a source into the field — and a
// generated row has no place to hang them. The chip carries the source's own host
// (the full address rides its tooltip), which is what a reader tells the sources
// apart by, and the press writes the whole URL into the field like any other row
// would.
const TEST_URL_PRESETS: Record<string, string[]> = {
    test_url: [
        'http://cp.cloudflare.com/generate_204',
        'https://www.gstatic.com/generate_204',
        'https://connectivitycheck.platform.hicloud.com/generate_204',
        'http://connect.rom.miui.com/generate_204',
        'http://wifi.vivo.com.cn/generate_204',
        'https://www.google.com/generate_204',
        'https://www.apple.com/library/test/success.html',
        'http://detectportal.firefox.com/success.txt',
    ],
    test_url_dl: [
        'http://cachefly.cachefly.net/10mb.test',
        'http://cachefly.cachefly.net/100mb.test',
        'https://speed.cloudflare.com/__down?bytes=25000000',
        'https://speed.cloudflare.com/__down?bytes=100000000',
        'http://speedtest.tele2.net/10MB.zip',
        'https://proof.ovh.net/files/100Mb.dat',
    ],
};
const testLabel = (key: 'test_url' | 'test_url_dl') =>
    key === 'test_url' ? t('cfg.test_url') : t('cfg.test_url_dl');
const hostOf = (v: string): string => { try { return new URL(v).host; } catch { return v; } };
const onTestPreset = (key: 'test_url' | 'test_url_dl', e: Event) => {
    const el = e.target as HTMLSelectElement;
    const v = el.value;
    // 受控 value="" 的等价回写：选完即回到占位项。
    el.value = '';
    if (v && obj.value) obj.value = {...obj.value, [key]: v};
};

// The language, drawn rather than generated: the file holds a
// number (see LANGUAGES) and a box of digits is no answer to
// "which language".
const pickLanguage = (l: {value: string; label: string; flag: string}) => {
    // Both at once, the arrangement the
    // palette follows: the window changes
    // now, because a language is worn
    // rather than saved, and the object
    // being edited carries the number so
    // that 保存 is what writes it to the
    // file.
    // Remembered here rather than on
    // 保存 for the same reason it is worn
    // here: the press is the choice, and a
    // next launch that asked the machine
    // again would take it back.
    rememberLanguage(Number(l.value));
    if (obj.value) obj.value = {...obj.value, language: Number(l.value)};
    emit('language', Number(l.value));
};

// The grip's aria ceiling follows the window; read at render, as the React
// original did.（window 不在模板全局白名单里，经此函数转接。）
const dockAriaMax = () => Math.round(window.innerHeight) - 54;

// JsonForm 的 onChange：把该组写回整个设置对象。
const onJsonChange = (v: Dict) => {
    if (obj.value) obj.value = {...obj.value, ...v};
};

// 路由卡的四张卡：三张模式卡同时是 egress 的选择，block 卡只翻页。
const pickRouteCard = (m: string) => {
    routeFocus.value = m;
    if (m !== 'block') egress.value = m;
};

// 双击把手高度还给样式表（原 onDoubleClick 内联逻辑）。
const resetDockHeight = () => {
    dockH.value = null;
    window.localStorage.removeItem('dock-height');
};

// 端口盒的两个输入（原 JSX 内联 onChange）。
const onAddrInput = (e: Event) => {
    const v = (e.target as HTMLInputElement).value;
    if (obj.value) obj.value = {...obj.value, inbound_address: v};
    // the last answer was about the old pair
    portNote.value = '';
};
const onPortInput = (e: Event) => {
    const el = e.target as HTMLInputElement;
    const n = Number(el.value);
    // An empty field, or a number no port can be, is not
    // a port: writing 0 (or 99999) into the file is a core
    // that cannot listen, met later as "the node will not
    // start" with nothing pointing at the number. The last
    // good one stands until a real one is typed.
    if (!Number.isInteger(n) || n < 1 || n > 65535) {
        // 受控输入的回写：非法值不落状态，框里回到上一个好值。
        el.value = String(obj.value?.inbound_socks_port ?? 2080);
        return;
    }
    if (obj.value) obj.value = {...obj.value, inbound_socks_port: n};
    if (el.value !== String(n)) el.value = String(n);
    portNote.value = '';
};
</script>

<template>
    <!-- The docked panel: pushed up from the status bar's left end, split into
        a left column of subjects and a right column of their cards. The nav
        names what is on the right and the cards slide between them — the same
        carousel the sheet used to carry, now standing up. -->
    <section :class="`settings-dock${dockDragging ? ' resizing' : ''}${closing ? ' closing' : ''}`"
             role="dialog"
             ref="dockRef"
             :style="dockH ? {height: `${dockH}px`} : undefined"
             :aria-label="t('dlg.settings.title')">
        <!-- The panel's top edge is the handle, the way the log panel's is: the dock
            stands on the bar and grows up from it, so the edge that moves is the one
            at the top. It sits inside the panel rather than straddling the border —
            the dock clips its overflow, and a strip hanging above it would be half a
            row of dead pixels (see .dock-grip). -->
        <div class="dock-grip"
             role="separator"
             aria-orientation="horizontal"
             :title="t('dlg.settings.resize')"
             :aria-label="t('dlg.settings.resize')"
             :aria-valuenow="dockNow"
             :aria-valuemin="DOCK_MIN_H"
             :aria-valuemax="dockAriaMax()"
             tabindex="0"
             @pointerdown="onDockGripDown"
             @pointermove="onDockGripMove"
             @pointerup="onDockGripUp"
             @pointercancel="onDockGripUp"
             @keydown="onDockGripKey"
             @dblclick="resetDockHeight"/>
        <!-- The header is a nameplate, not an action bar: the dock's ways out are the
            pointer (any press outside the panel) and Escape, and its changes save
            themselves — buttons here would promise a manual step that no longer
            exists. -->
        <header class="set-dock-head">
            <span class="set-dock-title">{{ t('dlg.settings.title') }}</span>
        </header>
        <div class="set-dock-body">
            <nav class="set-nav" :aria-label="t('dlg.settings.title')">
                <div class="set-nav-list">
                    <template v-for="(title, i) in navTitles" :key="title">
                        <!-- A hairline where the hand-written cards end and the
                            raw-settings groups begin: the two halves of the list
                            are two kinds of subject, and the nav is the one place
                            the reader sees both at once. -->
                        <div v-if="i === SETTING_SECTIONS.length" class="set-nav-sep" role="presentation"/>
                        <button type="button"
                                :class="`set-nav-item${i === slide ? ' active' : ''}`"
                                :aria-current="i === slide ? 'page' : undefined"
                                @click="slide = i">
                            {{ t(title) }}
                        </button>
                    </template>
                </div>
            </nav>
            <div class="set-cards">
                <!-- set-side-labels: this card keeps the label column rather than putting
                    its labels above their controls — see the rule in App.css. -->
                <div v-bind="slideOf('settings.group.startup', ' set-side-labels')">
                    <div class="set-group-title">{{ t('settings.group.startup') }}</div>
                    <!-- The looks box: the type scale and the palette wrapped in one framed
                        fieldset — two rows that answer the same question (how the window
                        looks), named once by the frame's own legend (see .look-box in
                        App.css). The rows still name themselves at their left — the
                        legend is the frame's name, and a control keeps its own — and the
                        hints ride the controls (the rail keeps its title, the theme bars
                        take theirs). The frame carries only
                        these two; the card's other rows stand in the two kind frames below
                        it, the fields' and the switches' (see `grouped`), because a switch
                        about the machine's boot is not a question about looks. -->
                    <div class="look-box">
                        <span class="look-box-title" aria-hidden="true">{{ t('settings.look') }}</span>
                        <!-- The type scale, drawn as a row of cells rather than as three stops:
                            "small / default / large" is a guess at what the reader wants, and
                            this is a setting they judge by eye. The row is the range cut into
                            equal pieces, so the number of lit cells is the size — see
                            scaleIndex / scaleFromIndex. Press and drag along it, use the arrow
                            keys inside it, or double-click to go back to 1. -->
                        <div class="jf-field" :title="t('settings.uiFontHint')">
                            <div class="ui-scale">
                                <!-- The row's own name, at its left: the frame's legend names the
                                    frame, and a control keeps its own name all the same. -->
                                <span class="ui-scale-label">{{ t('settings.uiFont') }}</span>
                                <div class="ui-scale-track"
                                     role="slider"
                                     tabindex="0"
                                     :aria-label="t('settings.uiFont')"
                                     :aria-valuemin="UI_SCALE_MIN"
                                     :aria-valuemax="UI_SCALE_MAX"
                                     :aria-valuenow="uiScale"
                                     :aria-valuetext="`${Math.round(uiScale * 100)}%`"
                                     :title="t('settings.uiFontHint')"
                                     @pointerdown="onScaleDown"
                                     @pointermove="onScaleMove"
                                     @keydown="onScaleKey"
                                     @dblclick="uiScale = systemScale">
                                    <span v-for="i in UI_SCALE_CELLS" :key="i"
                                          :class="`ui-scale-cell${i - 1 <= scaleIndex(uiScale) ? ' on' : ''}`"/>
                                </div>
                                <span class="ui-scale-read">{{ Math.round(uiScale * 100) }}%</span>
                            </div>
                        </div>
                        <!-- The palette, in the same card as the type scale because it is the
                            same kind of choice: how the window looks, judged by looking at it.
                            Drawn as the same control too — one bar to a tone, every cell painted
                            in its own palette (see .theme-swatch) — so the row is the themes
                            themselves rather than a list of their names. -->
                        <div class="jf-field" :title="t('settings.themeHint')">
                            <div class="theme-groups">
                                <div v-for="tone in TONES" :key="tone" class="theme-line">
                                    <!-- The tone's name, at the left of its bar: the way a form
                                        row names what sits beside it. It is the only place the
                                        two words are written — the bar speaks them too (see
                                        aria-label) for a reader who cannot see the colours. -->
                                    <span class="theme-tag">{{ t(`settings.tone.${tone}`) }}</span>
                                    <div class="theme-track">
                                        <!-- The nine, as the slider they are. The capsule is the
                                            frame and this is the part of it that is driven:
                                            pressed, dragged along, stepped with the arrows,
                                            double-clicked back to the first of them. -->
                                        <div class="theme-cells"
                                             role="slider"
                                             tabindex="0"
                                             :aria-label="t(`settings.tone.${tone}`)"
                                             aria-valuemin="0"
                                             :aria-valuemax="toneList(tone).length - 1"
                                             :aria-valuenow="toneAt(tone) < 0 ? undefined : toneAt(tone)"
                                             :aria-valuetext="toneAt(tone) < 0 ? undefined : t(toneList(tone)[toneAt(tone)].th.label)"
                                             :title="t('settings.themeHint')"
                                             @pointerdown="e => onThemeDown(e, tone)"
                                             @pointermove="e => onThemeMove(e, tone)"
                                             @keydown="e => onThemeKey(e, toneList(tone))"
                                             @dblclick="pickTheme(0)">
                                            <span v-for="cell in toneList(tone)" :key="cell.th.id"
                                                  :class="`theme-swatch${theme === cell.th.id ? ' active' : ''}`"
                                                  :data-swatch="cell.th.id"
                                                  :title="t(cell.th.label)"/>
                                        </div>
                                        <button type="button"
                                                :class="`theme-swatch theme-custom${theme === 'custom' ? ' active' : ''}${customPal ? ' made' : ''}`"
                                                :style="customPal ? {
                                                    '--sw-bg': customPal['--bg'],
                                                    '--sw-accent': customPal['--accent'],
                                                } as CSSProperties : undefined"
                                                :title="t('settings.customHint')"
                                                :aria-label="t('settings.theme.custom')"
                                                aria-haspopup="dialog"
                                                :aria-expanded="toneOpen"
                                                @click="onCustomClick"/>
                                    </div>
                                </div>
                            </div>
                        </div>
                        <!-- The dropdown the pill opens: twelve stops of the wheel and a
                            ground, and everything else about the palette is derived — a
                            control a reader can answer at a glance rather than a form of
                            hex codes. It is rendered at the body rather than kept in the
                            row it hangs from: the card's slide carries a transform, and a
                            fixed panel inside one is placed from the slide instead of the
                            window — and clipped by the card besides. The effect above puts
                            it under the pill (see tonePop). -->
                        <Teleport to="body">
                            <div v-if="toneOpen" class="tone-pop" ref="tonePop" role="dialog" :aria-label="t('settings.customTone')">
                                <div class="tone-pop-title">{{ t('settings.customTone') }}</div>
                                <div class="tone-hues" role="radiogroup" :aria-label="t('settings.customHue')">
                                    <button v-for="h in TONE_HUES" :key="h" type="button"
                                            :class="`tone-hue${toneHue === h ? ' on' : ''}`"
                                            :style="{'--hue': h} as CSSProperties"
                                            :aria-label="`${h}°`"
                                            :aria-pressed="toneHue === h"
                                            :title="`${h}°`"
                                            @click="toneHue = h"/>
                                </div>
                                <!-- The two grounds, as two named answers rather than two
                                    pictures of a ground: the panel shows twelve colours of
                                    the hue above, and two swatches beside them said nothing
                                    the words do not — so the words are all that is left,
                                    in the capsule this app draws a few named answers in
                                    (see SegmentedSwitch). -->
                                <SegmentedSwitch :label="t('settings.customTone')"
                                                 :options="TONE_GROUNDS"
                                                 :model-value="toneGround"
                                                 @update:model-value="v => toneGround = v as 'deep' | 'pale'"/>
                                <div class="hint">{{ t('settings.customHint') }}</div>
                                <div class="tone-pop-actions">
                                    <button class="btn" @click="saveTone">{{ t('settings.customSave') }}</button>
                                    <button class="btn" @click="toneOpen = false">{{ t('btn.cancel') }}</button>
                                </div>
                            </div>
                        </Teleport>
                    </div>
                    <!-- The 界面 rows, moved in from the card of their own they used to have
                        (see GROUP_IN_STARTUP). The card is named 启动与界面, and what these
                        ask — the language, what the window shows, what it does on the way up
                        — is that name's second half; the two were one subject read under two
                        headings. They are the same JsonForm rows the raw cards below are made
                        of, and drawn the same way: the fields in one frame, the switches in
                        another (see `grouped`), which is what every card of the panel now
                        does with the two kinds.
                        开机自启 stands in the switches' frame with them. It is a switch row of
                        this card, but it is a registration with the system rather than a key
                        of the settings file (see API.GetAutostart), so it is handed to the
                        form as a row of that kind rather than written around it (see
                        `extraRows`); its name rides the label column like the other rows'. -->
                    <JsonForm v-for="g in startupRows" :key="g.title"
                              :model-value="g.value" :cols="g.cols" :full-row="g.fullRow"
                              grouped
                              :frames="{text: t('kind.startup.text'), switch: t('kind.startup.switch')}"
                              :accept-hotkey="acceptHotkey"
                              @update:model-value="onJsonChange"
                              @hotkey-capture="onHotkeyCapture">
                        <template #extra-text>
                            <!-- The language, drawn rather than generated: the file holds a
                                number (see LANGUAGES) and a box of digits is no answer to
                                "which language". It stands first in the fields' frame, where
                                its key sat first in the group's list. -->
                            <div class="jf-field">
                                <label :title="`${t('cfg.language')} · language`">
                                    {{ t('cfg.language') }}
                                </label>
                                <!-- Seven languages, each wearing its country's flag
                                    (see LANGUAGES). One capsule of seven segments
                                    would be wider than the row, so they stand as a
                                    row of capsules that wraps — the one in force
                                    filled with the accent, which is how every
                                    other choice of a few named answers in this app
                                    says "this is the one" (see .seg-tab). -->
                                <div class="seg-tabs lang-pills" role="radiogroup"
                                     :aria-label="t('cfg.language')">
                                    <button v-for="l in LANGUAGES" :key="l.value" type="button" role="radio"
                                            :aria-checked="String(obj?.language ?? 0) === l.value"
                                            :class="`seg-tab${String(obj?.language ?? 0) === l.value ? ' active' : ''}`"
                                            :title="t(l.label)"
                                            :aria-label="t(l.label)"
                                            @click="pickLanguage(l)">
                                        <img class="lang-flag"
                                             :src="`/fa/icon/${l.flag}.svg`"
                                             alt=""/>
                                    </button>
                                </div>
                            </div>
                        </template>
                        <template #extra-switch>
                            <div class="jf-field">
                                <label>{{ t('settings.autostart') }}</label>
                                <label class="jf-check">
                                    <input type="checkbox"
                                           :aria-label="t('settings.autostart')"
                                           v-model="autoStart"/>
                                </label>
                            </div>
                        </template>
                    </JsonForm>
                    <!-- The inbound port, with a way to find a free one. The failure this
                        prevents is a confusing one: a port held by something else shows up
                        as "the node will not start", with nothing in that message pointing
                        at the number. It shares this card rather than having one of its
                        own: it is two inputs and two buttons, and a card holding nothing
                        else made the form longer than the thing it was holding.
                        The address and the port are edited here, and not in the raw card
                        further down: the same value offered in two places is two rows that
                        can disagree, and only one of them is the one the core will read.
                        Framed the way the looks box above it is, and for the same reason:
                        the pair is one subject — where the core listens — so the frame's
                        legend is its name and the rows inside carry none. The name used to
                        sit in the card's label column, beside a block it did not describe
                        (the note below the row included), which also cost the address the
                        90px that column holds. -->
                    <div class="look-box port-box">
                        <span class="look-box-title" aria-hidden="true">{{ t('settings.group.port') }}</span>
                        <!-- The probes sit inside the same row as the pair they act on:
                            one control, address and port with their two actions at the
                            right end, rather than a second row restating what the row
                            is about. Whatever the last probe said still lands below. -->
                        <div class="port-stack">
                            <div class="input-row">
                                <input class="mono" :value="String(obj?.inbound_address ?? '127.0.0.1')"
                                       placeholder="127.0.0.1"
                                       :aria-label="t('cfg.inbound_address')"
                                       :title="t('cfg.inbound_address')"
                                       @input="onAddrInput"/>
                                <input class="mono port-num" type="number" min="1" max="65535"
                                       :value="String(obj?.inbound_socks_port ?? 2080)"
                                       :aria-label="t('cfg.inbound_socks_port')"
                                       :title="t('cfg.inbound_socks_port')"
                                       @input="onPortInput"/>
                                <button class="btn" :disabled="portBusy" @click="checkInboundPort()">
                                    {{ t('port.check') }}
                                </button>
                                <button class="btn ghost" :disabled="portBusy" @click="pickFreePort()">
                                    {{ t('port.free') }}
                                </button>
                            </div>
                            <div v-if="portNote" :class="`port-note${portOk ? ' ok' : ' bad'}`">{{ portNote }}</div>
                        </div>
                    </div>
                </div>

                <!-- The TUN card. Its rows stand in the two kind frames every card of the panel
                    gathers its rows into — what the reader fills in in one, the switches in the
                    other — and its rows are hand-written rather than generated, so they wear
                    what a generated row of the same kind wears: the name in the column beside
                    the control, the control in the column that is left (see `grouped` and
                    `extraRows` in JsonForm, which is where the generated ones get theirs).
                    They used to be laid side by side in a grid instead — the number and the
                    five switches on one line, the two lists a column each under them — because
                    everything on this card is a single value, and stacked rows had the whole
                    width of a window-sized sheet to say "9000" and "false". The frames are the
                    card's shape now, and they were the reason this card was left full width
                    (see the note below about its label column). -->
                <div v-bind="slideOf('settings.group.tun', ' set-tun')">
                    <div class="set-group-title">{{ t('settings.group.tun') }}</div>
                    <TunCard v-if="obj" :obj="obj" :tun-on="tunOn" @patch="onJsonChange"/>
                    <div v-else class="hint">加载中…</div>
                </div>

                <!-- Routing. Four cards rather than a table of profiles: three of them are the
                    egress mode — the same choice the status bar carries, so clicking a card
                    and picking it there are one act — and the fourth is the block list, which
                    is a layer over all three rather than a direction of its own.
                    Each card's settings sit underneath it, edited in place. This used to be a
                    profile list whose editor was a three-tab dialog: every list was two clicks
                    away from the mode it belongs to, and the mode itself was somewhere else
                    again. -->
                <div v-bind="slideOf('settings.group.routing', ' set-routing')">
                    <div class="set-group-title">{{ t('settings.group.routing') }}</div>
                    <RoutingCard :form="routeForm" :focus="routeFocus" :egress="egress"
                                 :lang-tick="langTick"
                                 @edit="editRoute" @append="appendRoute" @card="pickRouteCard"/>
                </div>

                <!-- DNS. Lifted out of the routing card, where these were three cells of its
                    row and a tail of its advanced section. They answer a different question
                    from the lists — who resolves a name, rather than where the connection
                    goes — and the profile's DNS is written on every run whatever the mode is
                    (see buildDNS), so this is a card a reader can visit without having to
                    pick a mode first. Same profile, saved the same way. -->
                <div v-bind="slideOf('settings.group.dns')">
                    <div class="set-group-title">{{ t('settings.group.dns') }}</div>
                    <DnsCard v-if="routeForm"
                             :form="routeForm" :latency="dnsLatency" :node-on="nodeOn" :scale="uiScale"
                             :lang-tick="langTick"
                             :publish-dns-latency="onDnsLatency"
                             @edit="editRoute" @edit-flag="editRouteFlag"
                             @notify="(msg, kind) => emit('notify', msg, kind)"/>
                    <div v-else class="hint">加载中…</div>
                </div>

                <!-- Rule sets: what the routing profile refers to, and what is on disk. A card
                    of its own because this is the one part of routing whose failure is
                    silent — a .srs that never downloads is a rule that never matches, and
                    nothing in the core reports it. -->
                <div v-bind="slideOf('settings.group.rulesets')">
                    <RuleSetsCard :rule-sets="ruleSets" :rs-cats="rsCats" :rs-cats-open="rsCatsOpen"
                                  :rs-busy="rsBusy" :rs-updates="rsUpdates" :lang-tick="langTick"
                                  @toggle="toggleRsCat" @add="pickRuleSetFiles"
                                  @download="downloadRuleSet" @remove="removeRuleSet"
                                  @download-all="downloadRuleSet()" @open-dir="openDataDir"
                                  @add-cat="pickRuleSetFiles('')"/>
                </div>
                <!-- The raw settings object, split into the subjects it contains rather than
                    shown as the flat list the file happens to be. Each group is deliberately
                    the same card as everything above it — the rows are the same JsonForm
                    rows, only the territory is new — and whatever the table in SETTING_GROUPS
                    does not name lands in the last card, so a key added on the Go side can
                    never fall out of reach. -->
                <template v-for="g in jsonSlides" :key="g.title">
                    <div v-bind="slideOf(g.title, jsonSlideExtra(g.title))">
                        <div class="set-group-title">{{ t(g.title) }}</div>
                        <!-- A rule that holds for every row of the card states itself once,
                            above them. Only the hotkeys card has one. -->
                        <div v-if="g.note" class="hint">{{ t(g.note) }}</div>
                        <!-- The rows of this card that are not keys of the file are handed to
                            the form as rows of the fields' kind (see `extraRows` in JsonForm),
                            so that the card is one frame of fields rather than a few loose
                            rows above a frame of them. They are read from the app rather than
                            from the settings file: see where coreRows is built. -->
                        <JsonForm v-if="g.title === 'settings.group.core'"
                                  :model-value="g.value" :cols="g.cols" :full-row="g.fullRow"
                                  grouped
                                  :frames="framesOf(g.title)"
                                  :accept-hotkey="acceptHotkey"
                                  @update:model-value="onJsonChange"
                                  @hotkey-capture="onHotkeyCapture">
                            <template #extra-text>
                                <div class="jf-field">
                                    <label :title="t('settings.dataDir')">{{ t('settings.dataDirShort') }}</label>
                                    <div class="input-row">
                                        <input class="mono" :value="dataDir"
                                               @input="dataDir = ($event.target as HTMLInputElement).value"
                                               @blur="commitDataDir()"
                                               @keydown="onDataDirKey"/>
                                        <button class="btn" @click="openDataDir()"
                                                :title="t('about.openDataDir')">
                                            <FolderOpenFilled class="ic"/>
                                        </button>
                                    </div>
                                </div>
                                <div class="jf-field">
                                    <label :title="t('settings.corePath')">{{ t('settings.corePathShort') }}</label>
                                    <div class="input-row">
                                        <input class="mono" :value="corePath"
                                               placeholder="sing-box.exe"
                                               @input="corePath = ($event.target as HTMLInputElement).value"/>
                                        <button class="btn" @click="pickCore()" :title="t('btn.browse')">
                                            <FolderOpenFilled class="ic"/>
                                        </button>
                                    </div>
                                </div>
                                <div v-for="k in coreKeys" :key="k" class="jf-field">
                                    <label :title="t('settings.extraCores')">{{ k }}</label>
                                    <input class="mono" :value="extraCores[k] ?? ''"
                                           :placeholder="t('settings.corePlaceholder')"
                                           @input="setExtraCore(k, $event)"/>
                                </div>
                            </template>
                        </JsonForm>
                        <JsonForm v-else-if="g.title === 'settings.group.test'"
                                  :model-value="g.value" :cols="g.cols" :full-row="g.fullRow"
                                  grouped
                                  :frames="framesOf(g.title)"
                                  :accept-hotkey="acceptHotkey"
                                  @update:model-value="onJsonChange"
                                  @hotkey-capture="onHotkeyCapture">
                            <template #extra-text>
                                <!-- The test card's two URLs, drawn by hand rather than as generated
                                    rows: each wants a row of presets under it — one press writes a
                                    source into the field — and a generated row has no place to hang
                                    them. The chip carries the source's own host (the full address
                                    rides its tooltip), which is what a reader tells the sources
                                    apart by, and the press writes the whole URL into the field like
                                    any other row would. -->
                                <div v-for="key in (['test_url', 'test_url_dl'] as const)" :key="key" class="jf-field">
                                    <label :title="`${testLabel(key)} · ${key}`">{{ testLabel(key) }}</label>
                                    <div>
                                        <div class="input-row">
                                            <input class="mono" :value="(obj?.[key] as string) ?? ''"
                                                   :spellcheck="false"
                                                   @input="obj && (obj = {...obj, [key]: ($event.target as HTMLInputElement).value})"/>
                                            <!-- The presets as a dropdown rather than a run of chips: one
                                                control, one press, the whole address written in — the host
                                                is what the option names, the full URL what it carries. -->
                                            <select class="rt-urlselect" value=""
                                                    :aria-label="t('settings.testPreset')"
                                                    @change="onTestPreset(key, $event)">
                                                <option value="" disabled>{{ t('settings.testPreset') }}</option>
                                                <option v-for="v in TEST_URL_PRESETS[key]" :key="v" :value="v"
                                                        :title="v">
                                                    {{ hostOf(v) }}
                                                </option>
                                            </select>
                                        </div>
                                    </div>
                                </div>
                            </template>
                        </JsonForm>
                        <JsonForm v-else
                                  :model-value="g.value" :cols="g.cols" :full-row="g.fullRow"
                                  grouped
                                  :frames="framesOf(g.title)"
                                  :accept-hotkey="acceptHotkey"
                                  @update:model-value="onJsonChange"
                                  @hotkey-capture="onHotkeyCapture"/>
                    </div>
                </template>
                <!-- The one notice that is not a slide: the file is still being read. It hangs
                    over the carousel instead of joining it, where it would be a slide nobody
                    asked for. -->
                <div class="set-notice">
                    <div v-if="!obj" class="hint">加载中…</div>
                </div>
                <!-- A captured combination that could not be recorded, and why. Kept as the
                    pair rather than as a flag so the box can name both the keys and, when it
                    is one of ours, whose they are. -->
                <Modal v-if="hotkeyClash" :title="t('hk.clashTitle')" @close="hotkeyClash = null">
                    <p class="hint">
                        {{ hotkeyClash.owner ? t('hk.clashRow') : t('hk.clashTaken') }}<b class="hk-clash-key">{{ hotkeyClash.combo }}</b><span v-if="hotkeyClash.owner"> — {{ t('cfg.' + hotkeyClash.owner) }}</span>
                    </p>
                    <p class="hint">{{ t('hk.clashHint') }}</p>
                    <div class="modal-foot">
                        <button class="btn primary" @click="hotkeyClash = null">
                            {{ t('btn.ok') }}
                        </button>
                    </div>
                </Modal>
                <!-- The import's own window: the files the picker handed over, and the one
                    question about them the names cannot answer — where they bite. It is asked
                    once, of the whole batch, because files picked together are one country's
                    rules picked together. An empty answer is not a fourth state but a refusal
                    to say, and the fold's name then reads the tags the way it always did. -->
                <Modal v-if="rsAdd" :title="t('dlg.rsAdd.title')" @close="rsAdd = null">
                    <div class="gform">
                        <p class="hint">{{ tip('dlg.rsAdd.files', rsAdd.length) }}</p>
                        <ul class="rs-picked">
                            <li v-for="p in rsAdd" :key="p" :title="p">{{ p.split(/[\\/]/).pop() ?? p }}</li>
                        </ul>
                        <label class="gf-row">
                            <span>{{ t('dlg.rsAdd.country') }}</span>
                            <input :value="rsCountry" autofocus
                                   :placeholder="t('dlg.rsAdd.countryPh')"
                                   @input="rsCountry = ($event.target as HTMLInputElement).value"
                                   @keydown="e => { if (e.key === 'Enter') importRuleSets(); }"/>
                        </label>
                    </div>
                    <div class="modal-foot">
                        <button class="btn ghost" @click="rsAdd = null">{{ t('btn.cancel') }}</button>
                        <button class="btn primary" :disabled="rsBusy"
                                @click="importRuleSets()">{{ t('ruleset.add') }}</button>
                    </div>
                </Modal>
            </div>
        </div>
        </section>
</template>

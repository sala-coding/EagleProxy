// The app's actions, extracted from App.tsx (the "--- actions ---" block and the
// reload/loadProfiles plumbing). They run against the shared state through the deps
// object App.vue hands in — the refs are read and written at call time, which is the
// same "always current" guarantee the React version got from its ref mirrors.
import type {Ref} from 'vue';
import * as API from '../../wailsjs/go/main/App';
import {db, main, setting} from '../../wailsjs/go/models';
import {t} from '../i18n';
import {tip} from '../tips';
import {errText} from '../lib/shared';
import {hostFirstSegment} from '../lib/text';
import {MODE_HAS_SPEED, type Dict} from '../lib/metrics';
import {HOVER_SWITCH_DELAY} from '../lib/settings-meta';
import {type PanelExit} from '../lib/subdraw';
import {STOPPED} from './shared-consts';
import jsQR from 'jsqr';
import QRCode from 'qrcode';

export interface AppActionsDeps {
    groups: Ref<main.GroupInfo[]>;
    gid: Ref<number>;
    profiles: Ref<main.ProfileInfo[]>;
    selected: Ref<number>;
    running: Ref<number>;
    // The same value for the event handlers, which are installed once: the id a
    // core:exit carries has to be judged against the node running NOW, not against
    // the one that was running when the listener was built.
    runningIdRef: Ref<number>;
    applyRunning: (id: number) => void;
    busyRef: Ref<boolean>;
    setBusy: (v: boolean) => void;
    testingRef: Ref<boolean>;
    setTesting: (v: boolean) => void;
    testingMode: Ref<number>;
    testProgress: Ref<{ done: number; total: number }>;
    sort: Ref<{ key: 'latency' | 'speed'; dir: 1 | -1 } | null>;
    spMode: Ref<{ vpn: boolean; systemProxy: boolean }>;
    spBusyRef: Ref<boolean>;
    setSpBusyBoth: (v: 'vpn' | 'systemProxy' | null) => void;
    egress: Ref<string>;
    setEgressOpen: (v: boolean) => void;
    exitTick: Ref<number>;
    gConfirm: Ref<number | null>;
    importOpen: Ref<boolean>;
    importText: Ref<string>;
    scanErr: Ref<string>;
    previewOpen: Ref<boolean>;
    previewText: Ref<string>;
    qrOpen: Ref<boolean>;
    qrDataUrl: Ref<string>;
    moveOpen: Ref<boolean>;
    listKey: Ref<number>;
    query: Ref<string>;
    subBusy: Ref<boolean>;
    subsDrop: Ref<boolean>;
    connOpen: Ref<boolean>;
    connExit: Ref<PanelExit | null>;
    showLogs: Ref<boolean>;
    batchOpen: Ref<boolean>;
    editor: Ref<{ title: string; initial: Dict; onSave: (v: Dict) => Promise<void>; onDiscard?: () => Promise<void> } | null>;
    notify: (text: string, kind?: 'ok' | 'err') => void;
    holdStatusNote: (text: string, kind?: 'ok' | 'err') => void;
    loadFileCats: () => Promise<void>;
    openSubs: () => void;
    closeSubs: () => void;
    hardCloseSubs: () => void;
}

export function useActions(deps: AppActionsDeps) {
    // --- actions ---

    // The latest loadProfiles call only: see the guard inside it.
    const profileLoadSeq = {current: 0};

    const loadProfiles = async (g: number) => {
        // A sequence guard, because callers race: group switches (clicks and the
        // three-second hover) fire back-to-back, and a slower answer for the
        // earlier group would overwrite the newer one's list otherwise.
        const seq = ++profileLoadSeq.current;
        try {
            const ps = await API.ListProfiles(g);
            if (seq === profileLoadSeq.current) deps.profiles.value = ps;
        } catch (e) {
            if (seq === profileLoadSeq.current) console.error('ListProfiles failed:', e);
        }
    };

    // Ordered, because two reloads race: an event and a finally block both re-read
    // the tree (a subscription import landing while a start finishes), and the loser
    // used to be whichever answered last — the older answer, overwriting the newer
    // tree and the newer running id with it.
    const reloadSeq = {current: 0};
    const reload = async (forceGid?: number) => {
        const seq = ++reloadSeq.current;
        const gs = await API.ListGroups();
        if (seq !== reloadSeq.current) return gs;
        deps.groups.value = gs;
        const want = forceGid ?? deps.gid.value;
        if (gs.length > 0 && !gs.some(x => x.id === want)) deps.gid.value = gs[0].id;
        else deps.gid.value = want;
        const rp = await API.RunningProfile();
        if (seq !== reloadSeq.current) return gs;
        deps.applyRunning(rp);
        return gs;
    };

    // reload, but refusing to throw: for the finally blocks and the fire-and-forget
    // callers, where a rejected ListGroups would arrive as an unhandled rejection
    // with the UI already reset and nothing to show for it.
    const reloadQuiet = async (forceGid?: number) => {
        try {
            return await reload(forceGid);
        } catch (e) {
            console.error('reload failed:', e);
            return undefined;
        }
    };

    const selectGroup = async (id: number) => {
        deps.gid.value = id;
        deps.selected.value = -1;
        // The list comes first: remembering the subscription writes the settings
        // file, and making the table wait on that disk round-trip is what made a
        // hover switch feel like it lagged by a second or two.
        await loadProfiles(id);
        // remember the subscription so a restart reopens it
        try {
            await API.SetCurrentGroup(id);
        } catch {
            // persisting the view is a convenience; failing at it must not stop
            // the list from having switched
        }
    };

    // Hover-to-switch. The pointer has to hold a card for the whole delay, and still be
    // on it when the delay ends: the wait is a question — has the pointer settled here?
    // — and only a yes at the end of it makes that subscription current. A hover that is
    // abandoned part way through is dropped rather than deferred, so sweeping down the
    // strip loads nothing and the table is left exactly where it was.
    const hoverTimer: {current: number | null} = {current: null};
    // The timer fires after a render or two, so it must not read a gid from its
    // closure: a click elsewhere in the window can change the current group in the
    // meantime, and switching back to where it was would be a visible step backwards.
    //
    // The same ref serves the handlers that come back from a long wait — a
    // subscription update, a whole-group speed test, a batch that resolves every
    // node's domains, the settings settle. Minutes can pass inside those, and the gid
    // their closure was built with may by then be a subscription the reader has left
    // behind: reloading THAT group's list is how one group's nodes ended up under
    // another group's highlight, with nothing later to correct it.
    const gidRef: Ref<number> = deps.gid;

    const hoverSwitch = (id: number) => {
        if (id === deps.gid.value) return;
        if (hoverTimer.current !== null) window.clearTimeout(hoverTimer.current);
        hoverTimer.current = window.setTimeout(() => {
            hoverTimer.current = null;
            // Asked of the DOM at the end rather than cancelled by a leave handler,
            // because a card is two rows: crossing from its name down to its figures
            // raises leave and then enter for the same subscription within one task, and
            // a cancel would turn the three seconds into a wait the pointer can break by
            // moving twenty pixels. Either half of the card matches this, so the two
            // rows count as the one target they are drawn as.
            if (!document.querySelector(`tr[data-gid="${id}"]:hover`)) return;
            if (id !== gidRef.value) selectGroup(id);
        }, HOVER_SWITCH_DELAY);
    };

    // A pending switch must not fire after the dialog is gone.
    // (App.vue tears the timer down with the component; see onBeforeUnmount there.)

    const start = async (id: number) => {
        if (id < 0) return deps.notify(tip('core.needsNode'), 'err');
        if (deps.busyRef.value) return;
        // 节点先行：a double-click on a row starts the node, plainly if no mode is
        // on — the core listens on its local port and the TUN or system-proxy
        // switch on top of it is a separate, deliberate press. The backend no
        // longer refuses a bare start, and the switches refuse to turn on until
        // a node like this one is running.
        try {
            deps.setBusy(true);
            await API.StartProfile(id);
            deps.applyRunning(id);
            deps.notify(tip('core.started'));
        } catch (e) {
            deps.notify(tip('core.startFailed', errText(e)), 'err');
            // And the list this row came from is re-read. A start that failed because
            // the node is gone (the backend says so in as many words: a subscription
            // update replaced it) leaves a row on screen that no longer exists, and
            // the next press on it would fail exactly the same way. Re-reading here is
            // what takes the stale row away, at the moment the reader finds out about
            // it rather than on some later event.
            await loadProfiles(gidRef.value);
        } finally {
            deps.setBusy(false);
            // A failed refresh must not escape the finally block: callers here
            // await nothing, so the rejection would go unhandled.
            await reloadQuiet();
        }
    };

    const stop = async () => {
        if (deps.busyRef.value) return;
        try {
            deps.setBusy(true);
            await API.StopProfile();
            deps.applyRunning(STOPPED);
            deps.notify(tip('core.stopDone'));
        } catch (e) {
            deps.notify(tip('core.stopFailed', errText(e)), 'err');
        } finally {
            deps.setBusy(false);
            await reloadQuiet();
        }
    };

    // switchTo moves to another node in one click — and asks for it with a single call.
    //
    // It used to stop the running node first, which is what made a switch cost a core
    // restart: by the time the start arrived, the core was already down and there was
    // nothing left to move. The backend decides now. It switches in place when the node
    // is already in the running config (the whole point of the selector it carries, see
    // proxyGroup), and stops the old core only when it genuinely has to.
    const switchTo = async (id: number) => {
        if (id < 0) return deps.notify(tip('core.needsNode'), 'err');
        if (deps.busyRef.value) return;
        const name = deps.profiles.value.find(p => p.id === id)?.name ?? `#${id}`;
        try {
            deps.setBusy(true);
            await API.StartProfile(id);
            deps.applyRunning(id);
            // A hot switch leaves the id where it is — same core, same config — so the
            // effect above would not run on its own. This is what asks for a new reading.
            deps.exitTick.value++;
            deps.notify(tip('core.switchedTo', name));
        } catch (e) {
            deps.notify(tip('core.switchFailed', errText(e)), 'err');
        } finally {
            deps.setBusy(false);
            await reloadQuiet();
        }
    };

    // Clears every profile's latency, speed and traffic counters. The list is
    // reloaded from the backend afterwards so the cleared columns actually show.
    const resetMetrics = async () => {
        // Same re-entry contract as start/stop: a clear landing mid-start must
        // not flip the busy flag (and free the start's guard) underneath it.
        if (deps.busyRef.value) return;
        try {
            deps.setBusy(true);
            await API.ResetMetrics();
            const gs = await reload();
            await loadProfiles(gs.some(x => x.id === deps.gid.value) ? deps.gid.value : (gs[0]?.id ?? deps.gid.value));
            deps.notify(tip('metrics.cleared'));
        } catch (e) {
            deps.notify(errText(e), 'err');
        } finally {
            deps.setBusy(false);
        }
    };

    const runBatch = async (kind: 'dedupe' | 'failed' | 'resolve') => {
        deps.batchOpen.value = false;
        try {
            const n = kind === 'dedupe' ? await API.DeduplicateGroup(deps.gid.value)
                : kind === 'failed' ? await API.RemoveFailedProfiles(deps.gid.value)
                    : await API.ResolveGroupDomains(deps.gid.value);
            // The batch acts on the group on screen when it is pressed; the list that
            // is refreshed afterwards belongs to whatever group is on screen then,
            // since resolving a group's domains is a long walk.
            await loadProfiles(gidRef.value);
            if (n === 0) {
                deps.notify(tip('batch.none'));
                return;
            }
            deps.notify(tip(kind === 'dedupe' ? 'batch.deduped'
                : kind === 'failed' ? 'batch.removed' : 'batch.resolved', n));
        } catch (e) {
            deps.notify(errText(e), 'err');
        }
    };

    // Ends the run in progress. A group test is one node at a time and can be
    // minutes; the reader may have seen enough after the first few, and the nodes
    // already measured keep their numbers (see CancelSpeedtest on the Go side).
    const stopSpeedtest = async () => {
        try {
            await API.CancelSpeedtest();
            deps.notify(tip('metrics.stopped'));
        } catch (e) {
            deps.notify(errText(e), 'err');
        }
    };

    const speedTest = async (mode: number, wholeGroup: boolean, id?: number) => {
        // One run at a time: a second trigger while one is going must not clear its
        // progress, transplant its counters or end the "testing" indicator early.
        if (deps.testingRef.value) return;
        try {
            deps.setTesting(true);
            deps.testingMode.value = mode;
            deps.testProgress.value = {done: 0, total: wholeGroup ? deps.profiles.value.length : 1};
            // Sorted from the moment the run starts, not only when it ends. Every
            // result lands on its own row as it arrives (see the speedtest:result
            // handler), and re-sorting on each one is what makes the run a list
            // visibly rearranging itself rather than a block of numbers appearing
            // at the end. The direction is the one the finished run would pick,
            // for the same reason it picks it there: a low latency and a high
            // speed are both "better", so it follows the column the mode fills.
            // Nodes still untested keep the place the metricGroup rule gives them
            // — behind everything that measured something — so the list grows its
            // good end upwards instead of shuffling placeholders around.
            const speedMode = MODE_HAS_SPEED(mode);
            deps.sort.value = {key: speedMode ? 'speed' : 'latency', dir: speedMode ? -1 : 1};
            // "Test current" means the node the mouse picked: the row whose menu
            // this is, or — for any caller that names no node — the row that is
            // selected. It deliberately ignores the node that happens to be
            // running: that one is chosen by the running state, not by the
            // pointer, so a test would land on a node the user was never looking
            // at. The selection is the only fallback, and with nothing picked it
            // says so rather than guessing. Named out here because the reset below
            // is about this same row.
            const target = id ?? deps.selected.value;
            // Both measured columns are cleared before the run starts, not merely
            // written over by it. A run answers "how do these nodes do now", and
            // the previous run's numbers both hide which rows have been answered
            // yet — knowing what is still outstanding is half of what watching a
            // run is for — and hold places in the sort that the arriving results
            // then have to fight their way past instead of rising from the bottom.
            // The column this mode does not fill comes back when the run ends:
            // loadProfiles reads what the backend holds, and clearing here only
            // ever cleared the rows on screen.
            // Only the rows this run will test, though: "test current" must not
            // wipe the rest of the list.
            deps.profiles.value = deps.profiles.value.map(p => {
                if (!wholeGroup && p.id !== target) return p;
                return {...p, latency: 0, speed: 0};
            });
            let res: main.SpeedTestResult[];
            if (wholeGroup) {
                res = await API.SpeedtestGroup(deps.gid.value, mode);
            } else {
                if (target < 0) {
                    deps.notify(tip('core.needsNode'), 'err');
                    return;
                }
                res = await API.SpeedtestProfiles([target], mode);
            }
            // A whole-group test takes minutes; the group on screen is the one to
            // refresh, not the one this handler was built with.
            await loadProfiles(gidRef.value);
            if (res.length === 0) {
                deps.notify(tip('metrics.groupEmpty'), 'err');
                return;
            }
            // Connectivity is reported where it belongs: the latency column already
            // marks every unreachable node as "unavailable". Repeating the raw
            // error in a toast only duplicates that, truncates awkwardly, and
            // covers the very rows the user is trying to compare — so a finished
            // run says only that. An empty result is still an error, because that
            // means there was nothing to test: a mistake, not a node condition.
            deps.notify(tip('metrics.speedDone'));
            // The order the run has been building up stays as it is; this is only
            // here for the reader who re-sorted the list by hand while it ran, so
            // that the results are still laid out with the usable end of the
            // measured column on top when it finishes.
            deps.sort.value = {key: speedMode ? 'speed' : 'latency', dir: speedMode ? -1 : 1};
            // The run wrote a new file behind one of the test categories, and the
            // panel reads that file rather than watching it: re-read, so an open
            // log panel shows this run instead of the one before it.
            deps.loadFileCats();
        } catch (e) {
            deps.notify(errText(e), 'err');
        } finally {
            deps.setTesting(false);
            deps.testProgress.value = {done: 0, total: 0};
        }
    };

    const updateSub = async (targetGid: number) => {
        // The same reentry gate start/stop use: a press during a start (or a
        // second press here) would otherwise run concurrently, and whichever
        // finished first released the gate the other was still waiting on.
        if (deps.busyRef.value) return;
        try {
            deps.setBusy(true);
            const res: any = await API.UpdateSubscription(targetGid);
            await reload();
            // The group being looked at NOW: the fetch above is a network wait, and
            // the reader may well have moved to another subscription while it ran.
            await loadProfiles(gidRef.value);
            deps.notify(tip('sub.updated'));
            // What did not come across, and why. A node in a protocol this build does not
            // speak is not an update failure — its absence is the only trace of it, and a
            // count that comes out short with no explanation is what turns that into a
            // mystery worth asking about.
            const skipped: string[] = res?.skipped ?? [];
            if (skipped.length > 0) {
                const shown = skipped.slice(0, 3).join('、');
                const more = skipped.length > 3 ? ` 等 ${skipped.length} 个` : '';
                deps.notify(t('sub.skipped', skipped.length, shown + more), 'err');
            }
        } catch (e) {
            deps.notify(errText(e), 'err');
        } finally {
            deps.setBusy(false);
        }
    };

    // Two-step confirmation on the row's own delete button: the first click arms
    // it, the second one within four seconds removes the subscription. Same pattern
    // as the node list's rows.
    //
    // One timer at a time, and the handle kept: the timer was never stored, so a
    // second press on another row left the first one running, and the one that
    // fired first cleared a confirmation the reader had just been given. The
    // handle is handed out so the window can cancel it on the way out (see
    // App.vue's onBeforeUnmount).
    const gConfirmTimer: {current: number | null} = {current: null};
    const deleteGroup = async (targetGid: number) => {
        if (deps.gConfirm.value !== targetGid) {
            deps.gConfirm.value = targetGid;
            if (gConfirmTimer.current !== null) window.clearTimeout(gConfirmTimer.current);
            gConfirmTimer.current = window.setTimeout(() => {
                gConfirmTimer.current = null;
                if (deps.gConfirm.value === targetGid) deps.gConfirm.value = null;
            }, 4000);
            return;
        }
        if (gConfirmTimer.current !== null) {
            window.clearTimeout(gConfirmTimer.current);
            gConfirmTimer.current = null;
        }
        deps.gConfirm.value = null;
        try {
            await API.DeleteGroup(targetGid);
            const gs = await reload();
            // deleting the active group switches to another one
            const want = gs.some(x => x.id === deps.gid.value) ? deps.gid.value : (gs.length > 0 ? gs[0].id : -1);
            if (want < 0) {
                // ListProfiles(-1) means "every group" to the backend. With no
                // group left there is nothing to show, and an all-groups list
                // under a "no group" tab is a table of orphans.
                deps.gid.value = -1;
                deps.profiles.value = [];
            } else {
                await loadProfiles(want);
            }
            deps.notify(tip('group.deleted'));
        } catch (e) {
            deps.notify(errText(e), 'err');
        }
    };

    // System proxy and TUN both take over the machine's network path, so running
    // them together is not a meaningful combination — traffic would be captured
    // twice. Switching one on clears the other inside the backend, as part of the
    // same action, so nothing is decided about it here.
    //
    // The switch paints itself before the call is answered. Turning TUN on is not a
    // quick write: the running core is stopped, the mode is applied, and with the
    // internal TUN driver the app hands itself to UAC and comes back as a new
    // process — seconds in which a button that still looked off read as a press that
    // had been ignored, so the reader pressed it again, and again, and every press
    // asked for another stop/start. Three things follow from that: the optimistic
    // state below is what the backend is about to say anyway (the two clear each
    // other, so the one switched on is the only one on), the call corrects it once
    // it answers, and a press arriving while the first is still out is dropped
    // rather than queued — the reader asked for the mode, not for two of them.
    const toggleSP = async (kind: 'vpn' | 'systemProxy', on: boolean) => {
        if (deps.spBusyRef.value) return;
        // 节点先行：a mode with nothing behind it captures nothing, so the
        // switches answer the missing step instead of calling the backend —
        // which refuses with the same words for the paths that bypass this
        // handler (the tray, the hotkeys).
        if (on && deps.running.value === STOPPED) {
            deps.notify(t('sp.needNode'), 'err');
            return;
        }
        deps.setSpBusyBoth(kind);
        // And the wait is said in words, not only in the icon's breathing: switching
        // TUN can take seconds, and someone who pressed the switch deserves to read
        // what the app is doing rather than infer it from an animation. The message
        // is held rather than timed — see holdStatusNote — and the outcome replaces
        // it when it arrives.
        deps.holdStatusNote(tip(on ? 'sp.turningOn' : 'sp.turningOff',
            t(kind === 'vpn' ? 'sp.tun' : 'sp.systemProxy')));
        deps.spMode.value = on
            ? {vpn: kind === 'vpn', systemProxy: kind === 'systemProxy'}
            : (kind === 'vpn' ? {...deps.spMode.value, vpn: false} : {...deps.spMode.value, systemProxy: false});
        try {
            if (kind === 'vpn') {
                await API.SetVPNMode(on);
            } else {
                await API.SetSystemProxyMode(on);
            }
            deps.spMode.value = await API.GetSPMode() as { vpn: boolean; systemProxy: boolean };
            // Switching a mode ON is announced by the backend: only it knows
            // whether a node was restarted under the new mode or is still to be
            // picked, and for TUN the app may be restarting this very moment, so
            // the message has to come from the side that survives that. Switching
            // one OFF is local knowledge and is reported here.
            if (!on) deps.notify(kind === 'vpn' ? tip('sp.tunOff') : tip('sp.systemOff'));
        } catch (e) {
            deps.notify(errText(e), 'err');
            // The optimistic state was a guess and the backend has just refused the
            // call, so the guess is taken back rather than left standing. The read
            // is guarded: this catch is already inside an onClick handler with no
            // catch of its own, and a rejection here would escape as an unhandled
            // one — with the optimistic switch still showing the wrong state.
            await API.GetSPMode()
                .then(m => deps.spMode.value = m as { vpn: boolean; systemProxy: boolean })
                .catch(() => { /* the next press re-reads the mode anyway */ });
        } finally {
            deps.setSpBusyBoth(null);
        }
    };

    // The bar's outbound mode is a setting, not a property of the active routing
    // profile: it decides what the configbuilder does with that profile — apply it,
    // or leave its rules out — rather than which profile is in use. So it is read
    // from the settings object, and refetched when the dialog saves one.
    // (refreshEgress itself stays in App.vue, beside the state it feeds.)
    const setEgressMode = async (mode: string) => {
        deps.setEgressOpen(false);
        if (mode === deps.egress.value) return;
        // The same re-entry guard the start / stop / switch handlers keep, and for the
        // same reason: this stops and restarts the core. Without it a second press —
        // or a start the reader asked for while this one was still in flight — ran a
        // second stop/start pair, and whichever finished first took the flag down,
        // releasing a guard the other was still holding.
        if (deps.busyRef.value) {
            // Said rather than dropped: the card has already lit the new mode
            // (see pickRouteCard), and a press that silently did nothing read
            // as a broken card.
            deps.notify(t('egress.busy'), 'err');
            return;
        }
        deps.setBusy(true);
        try {
            const s: any = await API.GetSettings();
            await API.UpdateSettings(new setting.Setting({...s, egress_mode: mode}));
            deps.egress.value = mode;
            // The node running now, not the one this render closed over: the settings
            // write above is a disk round trip, and a switch made while it was in
            // flight is the one to restart.
            const cur = deps.runningIdRef.value;
            if (cur === STOPPED) {
                deps.notify(tip('egress.saved', t(`egress.${mode}`)));
                return;
            }
            await API.StopProfile();
            await API.StartProfile(cur);
            deps.notify(tip('egress.savedRunning', t(`egress.${mode}`)));
        } catch (e) {
            deps.notify(errText(e), 'err');
        } finally {
            // Quiet, so a failed re-read cannot leave the guard standing: this is the
            // only path that clears it.
            await reloadQuiet();
            deps.setBusy(false);
        }
    };

    // The protocol arrives with the call: it used to come from a select in the
    // bottom bar, and is now picked from the node menu's own submenu, where the
    // choice and the action it belongs to are finally in the same place.
    // 新增 = 先建一个空节点再打开编辑器（创建要拿到 id，编辑窗口才知道编的是谁）。
    // 读者若不保存，刚建的节点就被收回——"新增"取消后不留下一具空壳。
    const addProfile = async (proto: string) => {
        try {
            const id = await API.CreateProfile(proto, deps.gid.value);
            await reload();
            await loadProfiles(deps.gid.value);
            deps.selected.value = id;
            await editProfile(id, async () => {
                await deleteProfile(id);
            });
        } catch (e) {
            deps.notify(tip('profile.createFailed', errText(e)), 'err');
        }
    };

    // The entity JSON carries runtime bookkeeping beside what the reader edits:
    // the identity the store files the node under (id/gid), the measurements
    // (yc/speed/report), and the traffic counters. None of it is configuration —
    // a node that never ran even drew its empty traffic counter as an empty
    // frame — so the form shows the rest, and every one of these is merged back
    // untouched at save, from the read this dialog opened with (an edit must not
    // reset the counters it hid).
    const EDITOR_HIDDEN_KEYS = ['id', 'gid', 'yc', 'report', 'speed', 'traffic'];

    const editProfile = async (id: number, onDiscard?: () => Promise<void>) => {
        try {
            const json = await API.GetProfileJSON(id);
            const full = JSON.parse(json);
            const initial: Dict = {...full};
            for (const k of EDITOR_HIDDEN_KEYS) delete initial[k];
            deps.editor.value = {
                title: `${t('btn.edit')} #${id}`,
                initial,
                onSave: async (v) => {
                    const merged: Dict = {...v};
                    for (const k of EDITOR_HIDDEN_KEYS) if (k in full) merged[k] = full[k];
                    await API.SaveProfileJSON(id, JSON.stringify(merged));
                    // gidRef, not the closure gid: the dialog can stand open
                    // across a hotkey group switch, and saving into the stale
                    // group's list would leave the new group's rows overwritten.
                    await loadProfiles(gidRef.value);
                    deps.notify(tip('profile.saved'));
                },
                onDiscard,
            };
        } catch (e) {
            deps.notify(errText(e), 'err');
        }
    };

    const cloneProfile = async (id: number) => {
        try {
            const nid = await API.CloneProfile(id);
            await reload();
            await loadProfiles(deps.gid.value);
            deps.selected.value = nid;
            deps.notify(tip('profile.cloned'));
        } catch (e) {
            deps.notify(errText(e), 'err');
        }
    };

    const deleteProfile = async (id: number) => {
        try {
            await API.DeleteProfile(id);
            deps.selected.value = -1;
            await reload();
            await loadProfiles(deps.gid.value);
            deps.notify(tip('profile.deleted'));
        } catch (e) {
            deps.notify(errText(e), 'err');
        }
    };

    const copyLink = async (id: number, nekoray: boolean) => {
        try {
            const link = await API.ExportLink(id, nekoray);
            await API.WriteClipboard(link);
            deps.notify(tip('profile.linkCopied'));
        } catch (e) {
            deps.notify(errText(e), 'err');
        }
    };

    const doImport = async () => {
        try {
            const ids = await API.ImportLinks(deps.importText.value, deps.gid.value);
            deps.importOpen.value = false;
            deps.importText.value = '';
            await reload();
            await loadProfiles(deps.gid.value);
            deps.notify(tip('import.done', ids.length));
        } catch (e) {
            // A partial import arrives as ids + error: the rows that made it are
            // real and the list must show them, while the text stays for the
            // lines the reader may want to fix and re-run.
            deps.notify(errText(e), 'err');
            await reload().catch(() => { /* the error already said its piece */ });
            await loadProfiles(deps.gid.value).catch(() => { /* the error already said its piece */ });
        }
    };

    const importFromClipboard = async () => {
        deps.importText.value = await API.ReadClipboard();
    };

    const doPreview = async (id?: number) => {
        const target = id ?? deps.selected.value;
        if (target < 0) return deps.notify(tip('core.needsNode'), 'err');
        try {
            deps.previewText.value = await API.PreviewConfig(target);
            deps.previewOpen.value = true;
        } catch (e) {
            deps.notify(tip('config.previewFailed', errText(e)), 'err');
        }
    };

    // A subscription now starts here: the link pasted into the search box and
    // Enter-ed. The form the drawer's plus button used to raise is gone with the
    // button — the box IS the form: host from the link becomes the name, the rest
    // of the fields take their defaults, and the pull runs the moment the record
    // exists. Any half-made record comes back out on the way down (UpdateGroup is
    // the step that can fail on a bad link), so a retry does not leave a husk
    // behind. The pull itself is `updateSub`'s own work — success opens the drawer
    // with the new entry on it; a failed pull leaves the record (its refresh
    // button can reach it again) and says why.
    const addSubFromSearch = async (raw: string) => {
        const url = raw.trim();
        try {
            deps.subBusy.value = true;
            let host = '';
            try { host = new URL(url).hostname; } catch { /* named by fallback */ }
            const name = hostFirstSegment(url) || host || t('groups.unnamed');
            const newGid = await API.CreateGroup(name);
            try {
                await API.UpdateGroup(new db.Group({
                    id: newGid, name, url,
                    front_proxy_id: -1, archive: false,
                    sub_interval: 0,
                }));
                await reload();
            } catch (e) {
                try { await API.DeleteGroup(newGid); } catch { /* the first failure is the story */ }
                throw e;
            }
            await updateSub(newGid);
            deps.query.value = '';
            deps.openSubs();
        } catch (e) {
            deps.notify(errText(e), 'err');
        } finally {
            deps.subBusy.value = false;
        }
    };

    const moveTo = async (targetGid: number, id?: number) => {
        const target = id ?? deps.selected.value;
        try {
            await API.MoveProfile(target, targetGid);
            deps.moveOpen.value = false;
            deps.selected.value = -1;
            await reload();
            await loadProfiles(deps.gid.value);
            deps.notify(tip('core.moved'));
        } catch (e) {
            deps.notify(errText(e), 'err');
        }
    };

    const showQR = async (id: number) => {
        try {
            const link = await API.ExportLink(id, false);
            deps.qrDataUrl.value = await QRCode.toDataURL(link, {width: 320, margin: 1});
            deps.qrOpen.value = true;
        } catch (e) {
            deps.notify(errText(e), 'err');
        }
    };

    // The same dialog for a subscription's own address. A node's link has to be exported
    // by the backend; a subscription is a URL that is already on the row, so this one is
    // drawn from what the reader is looking at and needs no round trip at all. Takes the
    // value rather than the id because that is all it needs, and an empty one is simply
    // not drawn (the button does not appear without an address to encode).
    const showUrlQR = async (url?: string) => {
        if (!url) return;
        try {
            deps.qrDataUrl.value = await QRCode.toDataURL(url, {width: 320, margin: 1});
            deps.qrOpen.value = true;
        } catch (e) {
            deps.notify(errText(e), 'err');
        }
    };

    const exportConfig = async (id: number) => {
        try {
            const path = await API.ExportConfigFile(id);
            if (path) deps.notify(tip('config.exported', path));
        } catch (e) {
            deps.notify(errText(e), 'err');
        }
    };

    const scanImage = (file: File) => {
        const img = new Image();
        const url = URL.createObjectURL(file);
        // Both outcomes release the blob and both say something. A file the engine
        // cannot decode (a HEIC, a truncated PNG) used to take neither path: the
        // button looked as if it had not been pressed, and the object URL stayed alive
        // for the life of the window.
        img.onerror = () => {
            URL.revokeObjectURL(url);
            deps.scanErr.value = t('qr.decodeFailed');
        };
        img.onload = () => {
            const canvas = document.createElement('canvas');
            canvas.width = img.width;
            canvas.height = img.height;
            const ctx = canvas.getContext('2d');
            if (ctx) {
                ctx.drawImage(img, 0, 0);
                const data = ctx.getImageData(0, 0, img.width, img.height);
                const code = jsQR(data.data, data.width, data.height);
                if (code && code.data) {
                    deps.importText.value = deps.importText.value ? deps.importText.value + '\n' + code.data : code.data;
                    deps.scanErr.value = '';
                } else {
                    deps.scanErr.value = t('qr.notFound');
                }
            }
            URL.revokeObjectURL(url);
        };
        img.src = url;
    };

    // The connections window and the subscriptions drawer share one seat: the same
    // left edge, the same width, the same hinge. So opening one closes the other —
    // two panels in one slot would be stacked exactly on top of each other, and the
    // gutter would be holding room for both while showing one. Shared by the two
    // entry points, the status bar and the rail, for the same reason toggleLogs is.
    const openConns = () => {
        // The same button that opens it puts it away. An exit already under way is
        // impatience rather than a second request: cut it short instead of waiting
        // out the turn.
        if (deps.connExit.value) {
            deps.connExit.value = null;
            deps.connOpen.value = false;
            return;
        }
        if (deps.connOpen.value) {
            deps.connExit.value = 'flip';
            return;
        }
        // The drawer is in this seat too, and it goes at once, with no exit of its own —
        // the same reasoning as the other way round in openSubs: a displacement is not a
        // dismissal, and animating one put a second motion on screen for a panel the
        // reader never asked to close.
        if (deps.subsDrop.value) deps.hardCloseSubs();
        deps.connOpen.value = true;
    };

    return {loadProfiles, reload, reloadQuiet, profileLoadSeq, selectGroup, hoverSwitch, hoverTimer,
        start, stop, switchTo, resetMetrics, runBatch, stopSpeedtest, speedTest,
        updateSub, deleteGroup, gConfirmTimer, toggleSP, setEgressMode,
        addProfile, editProfile, cloneProfile, deleteProfile, copyLink,
        doImport, importFromClipboard, doPreview, addSubFromSearch, moveTo,
        showQR, showUrlQR, exportConfig, scanImage, openConns};
}

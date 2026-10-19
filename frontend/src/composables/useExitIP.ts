// The exit-address corner of the status bar and the exit-details panel,
// extracted from App.tsx (the exitIP block).
import {ref, watch, type Ref} from 'vue';
import * as API from '../../wailsjs/go/main/App';
import {STOPPED} from './shared-consts';

export function useExitIP(running: Ref<number>) {
    // The address the world sees this machine as while a node is up (see ExitIP on the
    // Go side), with the country it is registered in. Not the node's own address: that
    // one only says which server the traffic was handed to, and this corner of the bar
    // is about what the traffic looks like coming out the other side.
    const exitIP = ref<Record<string, string> | null>(null);
    // The same answer readable without waiting for a render: the polling timer below
    // compares against it to tell "the address moved" from "the same address asked
    // again", and the first answer after a start has nothing else to compare against.
    // (Vue: the mirror is kept, synced from the state by the watcher below.)
    const exitIPRef = {current: null as Record<string, string> | null};
    watch(exitIP, v => {
        exitIPRef.current = v;
    });
    // Bumped when something asks for a fresh reading even though the running node did not
    // change — a hot switch keeps the same core and the same config, so the id stays put
    // while the node behind it does not. The effect below listens for it.
    const exitTick = ref(0);
    // Set when the flag file for that country is not on disk after all — a code the set
    // does not carry, or a copy that did not finish. Reset with each answer.
    const flagMissing = ref(false);
    // The exit-details panel, opened by pressing the address itself. The data is the
    // same answer the corner already holds (see exitIP) — the panel only lays it out
    // with the labels on.
    const infoOpen = ref(false);
    const infoRef = ref<HTMLDivElement | null>(null);
    // The button that opens the panel. It stands in the status bar rather than inside
    // the panel, so the outside-press closer below has to be told about it by name:
    // judging on the panel alone, a press on the button counted as an outside press,
    // closed the panel on mousedown, and the click that followed opened it again —
    // the panel could be opened but never closed by the control that opens it.
    const nodeIpRef = ref<HTMLButtonElement | null>(null);

    // The exit address, read when the running node changes and not on a timer: it belongs
    // to the node that is up, and it does not move while that node stays up.
    //
    // Retried a few times, because the honest answer in the first seconds after a start
    // is nothing at all — the core is up before its tunnel carries anything — and that
    // would otherwise leave the corner empty until the node was restarted. The flag is
    // what stops a slow answer for the previous node from landing after the next one has
    // already been asked about.
    watch([running, exitTick], (_, __, onCleanup) => {
        if (running.value === STOPPED) {
            exitIP.value = null;
            // And the panel that shows it goes with the address: left open behind a
            // torn-down corner, it was still "open" as far as its own state went, and
            // it reappeared by itself the moment the next node answered.
            infoOpen.value = false;
            return;
        }
        let alive = true;
        let tries = 0;
        let timer: number | undefined;
        const ask = async () => {
            tries++;
            try {
                const r = await API.ExitIP();
                if (!alive) return;
                if (r && r.ip) {
                    exitIP.value = r;
                    // the previous node's missing flag says nothing about this one
                    flagMissing.value = false;
                    return;
                }
            } catch { /* the retry below is what a failure gets */ }
            // Six tries over twelve seconds rather than four over six: the answer is
            // wanted right after a start or a switch, which is exactly when the tunnel is
            // least likely to carry it — the core is up before its outbound is.
            if (alive && tries < 6) timer = window.setTimeout(ask, 2000);
        };
        void ask();
        onCleanup(() => {
            alive = false;
            if (timer) window.clearTimeout(timer);
        });
    }, {immediate: true});

    // The same corner, on a timer, so it keeps up with a node that moves underneath it.
    // The effect above runs only when the running id changes, and the address can change
    // without it: a hot switch goes through the core's own API and leaves the id where it
    // is, a group hands the traffic to another member, a provider fails over. None of those
    // restart the core, so the corner would go on naming the old address until something
    // unrelated happened to refresh it.
    //
    // ExitAddress rather than ExitIP: this runs every couple of seconds, and the full
    // reading costs three more requests — an ASN, a registrar, a purity score — for a
    // corner that draws none of them. When the address does move, the cheap answer is shown
    // at once and exitTick is bumped, which is what asks the full one and fills the panel
    // in behind it.
    //
    // Nothing is asked while the window is hidden: the tray is where this app spends most
    // of its life, and a round trip through the tunnel every two seconds for a corner
    // nobody is looking at spends the reader's bandwidth and somebody's quota on nothing.
    // Coming back into view asks immediately, so what is first seen is current.
    watch(running, (_, __, onCleanup) => {
        if (running.value === STOPPED) return;
        let alive = true;
        // The address last seen by this loop, so a change can be told from the same answer
        // arriving again. Local to the effect: a new node starts a new comparison.
        const tick = async () => {
            if (document.hidden) return;
            try {
                const r = await API.ExitAddress();
                if (!alive || !r || !r.ip) return;
                // Compared against what the corner is showing, not against the last
                // answer this loop saw: the first answer after a start is exactly the
                // one with nowhere else to come from, and the old guard — which needed a
                // previous answer of its own — dropped it. Everything read about the
                // old address is about the old address, so the cheap answer stands on
                // its own until the full reading lands on top of it.
                if (exitIPRef.current?.ip === r.ip) return;
                exitIP.value = r;
                // The flag that failed to load belonged to the previous address, and a
                // country that has just changed says nothing about it. Left set, the rack
                // icon stood in for a flag that exists, until some later full reading
                // happened to succeed.
                flagMissing.value = false;
                exitTick.value++;
            } catch { /* a poll that fails is not news: the corner keeps what it has */ }
        };
        const timer = window.setInterval(tick, 2000);
        const onVisible = () => {
            if (!document.hidden) void tick();
        };
        document.addEventListener('visibilitychange', onVisible);
        onCleanup(() => {
            alive = false;
            window.clearInterval(timer);
            document.removeEventListener('visibilitychange', onVisible);
        });
    }, {immediate: true});

    // The exit panel answers to the same two as every other list: a press outside it, or
    // Escape. mousedown rather than click, so it is gone before the press lands on
    // whatever is behind it.
    watch(infoOpen, (open, _, onCleanup) => {
        if (!open) return;
        const onDown = (e: MouseEvent) => {
            const target = e.target as Node;
            if (infoRef.value?.contains(target)) return;
            if (nodeIpRef.value?.contains(target)) return;
            infoOpen.value = false;
        };
        const onKey = (e: KeyboardEvent) => {
            if (e.key === 'Escape') infoOpen.value = false;
        };
        window.addEventListener('mousedown', onDown);
        window.addEventListener('keydown', onKey);
        onCleanup(() => {
            window.removeEventListener('mousedown', onDown);
            window.removeEventListener('keydown', onKey);
        });
    });

    return {exitIP, exitTick, flagMissing, infoOpen, infoRef, nodeIpRef};
}

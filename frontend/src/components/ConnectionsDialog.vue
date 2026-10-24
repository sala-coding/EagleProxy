<script setup lang="ts">
import {computed, onBeforeUnmount, onMounted, ref} from 'vue';
import * as API from "../../wailsjs/go/main/App";
import {t} from "../i18n";
import {errText} from "../lib/shared";
import {fmtBytes} from "../lib/format";
import {type PanelExit} from "../lib/subdraw";
import Modal from "./Modal.vue";
import {CloseCircleFilled} from '../lib/icons';

// onClose -> 'close'; notify -> 'notify'; onClosing -> 'closing'.
// exiting is the rail and status-bar buttons asking for the same thing the × does:
// the panel leaves by the motion it arrived by. They cannot call onClose themselves,
// because that unmounts it outright and the swing away would never be seen.
withDefaults(defineProps<{
    exiting?: PanelExit | null;
}>(), {exiting: null});

const emit = defineEmits<{
    close: [];
    notify: [t: string, k?: 'ok' | 'err'];
    closing: [kind: PanelExit];
}>();

const data = ref<any>(null);
// Escape used to call onClose outright, which skipped the exit animation altogether —
// the one close path that had no motion at all. It asks for the same flip the x does.
const escExit = ref<PanelExit | null>(null);
const err = ref('');
// The startup card's "连接统计" (conn_stat) is what decides whether this panel
// is figures as well as a list: off, the per-connection ↑/↓ and the footer's
// upload/download totals stand down and only the destinations remain. True
// whenever the settings cannot be read, so the panel never turns blank by
// accident.
const showStat = ref(true);

// One poll in flight at a time. The interval does not wait for the previous load, and
// a Clash API with a long connection table answers slower than 1.5s: the polls queued
// up, their answers arrived out of order, and the list went backwards — rows for
// connections closed a second earlier reappearing under the newest snapshot.
let busy = false;

const load = async () => {
    if (busy) return;
    busy = true;
    try {
        data.value = await API.GetConnections();
        err.value = '';
    } catch (e) {
        err.value = errText(e);
        // And the last good snapshot goes with the failure. A core that has
        // stopped, or a Clash API that no longer answers, has no connections —
        // the list still on screen was the previous poll's, showing rows for
        // connections that are gone, each with a close button that can only
        // fail, under a footer whose totals are as old as the rows. The error
        // line is the whole truth of that state.
        data.value = null;
    }
    // The figures switch is read with every poll rather than once at mount: the
    // startup card can turn it while this panel is open, and a panel that goes on
    // drawing numbers after the reader turned them off (or hides them after the
    // reader turned them on) is the panel disagreeing with the setting it names.
    // Its failure is not the list's: what the last answer said stands.
    try {
        const s: any = await API.GetSettings();
        showStat.value = !!s?.conn_stat;
    } catch { /* keep the last answer */ }
    busy = false;
};

let interval = 0;
// Escape puts it away. It cannot be dismissed by being clicked away from any more:
// its mask is transparent and passes clicks through to the table, which is the
// point — a panel you consult while you work should not eat the click you meant
// for the node list. So the key and the × are what close it. Bound once: the body
// touches nothing from the props, and re-binding the listener on every render
// (this panel re-renders each second with fresh data) is churn.
const onKey = (e: KeyboardEvent) => {
    if (e.key !== 'Escape') return;
    // The topmost panel answers first, and only it. The settings dock is also
    // listening on the window for Escape, and it was registered before this
    // one existed — so the capture phase is what lets this panel take the
    // press, which is the one the reader meant (it is the panel they are
    // looking at). One Escape closes this, the next closes the dock.
    e.stopPropagation();
    escExit.value = 'flip';
};

onMounted(() => {
    load();
    interval = window.setInterval(load, 1500);
    window.addEventListener('keydown', onKey, true);
});
onBeforeUnmount(() => {
    window.clearInterval(interval);
    window.removeEventListener('keydown', onKey, true);
});

const closeConn = async (id: string) => {
    try {
        await API.CloseConnection(id);
        await load();
    } catch (e) {
        emit('notify', errText(e), 'err');
    }
};

const conns = computed<any[]>(() => data.value?.connections ?? []);

// The render cap. A busy node can hold thousands of live connections, and the
// list re-lays all of them out every poll — that, not the fetch, is what makes
// the panel expensive to keep open. The footer's count still names the real
// total; the tail becomes one summary line instead of thousands of rows.
const CONN_RENDER_CAP = 500;
const shownConns = computed<any[]>(() => conns.value.slice(0, CONN_RENDER_CAP));
const hiddenConns = computed(() => Math.max(0, conns.value.length - shownConns.value.length));

const targetOf = (c: any) => `${c.metadata?.host || c.metadata?.destinationIP}:${c.metadata?.destinationPort}`;
const chainOf = (c: any) => Array.isArray(c.chains) ? c.chains.join(' ← ') : '';
</script>

<template>
    <Modal :title="t('dlg.conn.title')" @close="emit('close')" flip
           :exit="exiting ?? escExit ?? null" @closing="emit('closing', $event)"
           extra-class="conn-modal" :body-scroll="false">
        <template #bar>
            <div class="conn-foot">
                <div class="conn-total dim">
                    <template v-if="showStat">{{ t('traffic.upDown', fmtBytes(data?.uploadTotal ?? 0), fmtBytes(data?.downloadTotal ?? 0)) }} · </template>{{ t('conn.total', conns.length) }}
                </div>
                <button class="btn" @click="load">{{ t('btn.refresh') }}</button>
            </div>
        </template>
        <div v-if="err" class="errline">{{ err }}</div>
        <!-- One entry per connection, on two lines: the destination above, the
             account figures below. This used to be a six-column table with fixed
             widths — 300px for the target plus 500px of fixed columns — which is
             why the dialog had to be 860px wide, and it is now the same width as
             the subscriptions drawer. A table cannot be narrow and keep its
             columns; stacking the figures under the address can, and it is the
             arrangement the subscription cards already use.

             It also removes the reason the header was a second table: the columns
             were the thing that had to stay aligned between the header and the
             body, and there are no columns left to misalign. -->
        <div class="conn-list">
            <div class="conn-row" v-for="c in shownConns" :key="c.id">
                <div class="conn-row-head">
                    <span class="addr" :title="targetOf(c)">{{ targetOf(c) }}</span>
                    <button class="btn tiny danger" :title="t('btn.close')"
                            @click="closeConn(c.id)">
                        <CloseCircleFilled class="ic"/>{{ t('btn.close') }}
                    </button>
                </div>
                <div class="conn-row-figures">
                    <span>{{ c.metadata?.network }}/{{ c.metadata?.type }}</span>
                    <template v-if="showStat">
                      <span class="dim fig-bytes">↑ {{ fmtBytes(c.upload) }}</span>
                      <span class="dim fig-bytes">↓ {{ fmtBytes(c.download) }}</span>
                    </template>
                    <!-- The chain keeps its full value in the tooltip: a
                         multi-hop chain is the one field here with no
                         bound on its length, so it is the one that gives
                         up its width rather than its text. -->
                    <span v-if="chainOf(c)" class="dim" :title="chainOf(c)">{{ chainOf(c) }}</span>
                </div>
            </div>
            <div v-if="conns.length === 0" class="conn-empty">{{ t('conn.empty') }}</div>
            <div v-else-if="hiddenConns > 0" class="conn-empty dim">{{ t('conn.more', hiddenConns) }}</div>
        </div>
    </Modal>
</template>

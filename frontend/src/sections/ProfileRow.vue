<script setup lang="ts">
// One row of the profiles table. Under React this row was wrapped in memo()
// (see the note below); Vue's granular updates give the same outcome — a row
// re-renders only when one of its own props changes, so the traffic poll's
// array rewrites touch the running row alone.
import {main} from '../../wailsjs/go/models';
import {hueFor, latencyClass, latencyText, isLowLatency, speedCell, trafficCell} from '../lib/metrics';
import {fmtBytes} from '../lib/format';
import NodeName from '../components/NodeName.vue';
import {ThumbUpFilled} from '../lib/icons';

const props = defineProps<{
    p: main.ProfileInfo;
    selected: boolean;
    running: boolean;
}>();

const emit = defineEmits<{
    select: [id: number];
    start: [id: number];
    ctx: [e: MouseEvent, id: number];
}>();

// One row of the profiles table, memoised. The traffic poll rewrites the profiles
// array every tick (and a speedtest run does the same at a burst), and the inline
// row map made every one of those ticks re-render the whole table — a few hundred
// rows of vdom and DOM work per second for the one row whose counters actually
// moved, which is where the WebView2 process's standing CPU went. Behind
// React.memo a tick reconciles the running row alone: every other row receives
// unchanged prop references and is skipped. The handlers take the id rather than
// closing over the row, so they stay reference-stable across renders.
const rowClass = () => [props.selected ? 'selected' : '', props.running ? 'running' : ''].join(' ');
</script>

<template>
    <tr :data-pid="p.id"
        :class="rowClass()"
        @click="emit('select', p.id)"
        @dblclick="emit('start', p.id)"
        @contextmenu="emit('ctx', $event, p.id)">
        <!-- The protocol, in a column of its own — in the colour of the
            subscription the node came from: the same --tab-hue the drawer's
            card carries (see hueFor), set per row so a node and the card it
            lives in come out one colour in both places. -->
        <td class="type" :style="{'--tab-hue': hueFor(p.gid)}">
            {{ p.type }}
        </td>
        <td class="name" :title="p.name"><NodeName :name="p.name"/></td>
        <td class="addr" :title="p.address">{{ p.address }}</td>
        <td class="port">{{ p.port }}</td>
        <td class="net">{{ p.network || 'tcp' }}</td>
        <td class="tls" :title="p.tlsNote || ''">{{ p.security || '-' }}</td>
        <td :class="`latency ${latencyClass(p.latency)}`" :title="p.report || ''">
            {{ latencyText(p.latency) }}
            <ThumbUpFilled v-if="isLowLatency(p.latency)" class="ic lat-ok"/>
        </td>
        <td class="speed" :title="p.report || ''">{{ speedCell(p.speed) }}</td>
        <td class="ptraffic"
            :title="p.up || p.down ? `上传 ${fmtBytes(p.up)} · 下载 ${fmtBytes(p.down)}` : ''">
            {{ trafficCell(p.up, p.down) }}
        </td>
        <td class="pgroup">{{ p.group || '-' }}</td>
    </tr>
</template>

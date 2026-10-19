<script setup lang="ts">
import {main} from "../../wailsjs/go/models";
import {fmtBytes} from "../lib/format";
import {latencyClass, latencyText, speedCell, trafficCell} from "../lib/metrics";
import NodeName from "./NodeName.vue";

// One row of a card, memoised. The current group's card is fed fresh rows by
// seed, and the traffic poll hands that seed a new identity every tick — so the
// card re-renders every second. Without the memo that meant re-rendering every
// row's ten cells for the one row whose counters moved; with it, a tick
// reconciles the running row alone and the other rows' prop references are
// unchanged. (Under Vue the standalone SFC plays the memo's part: the row only
// re-renders when its own `p` prop changes.)
defineProps<{p: main.ProfileInfo}>();
</script>

<template>
    <div class="reel-row">
        <span class="reel-ty">{{ p.type }}</span>
        <span class="reel-nm"><NodeName :name="p.name"/></span>
        <span class="reel-ad">{{ p.address }}</span>
        <span class="reel-pt">{{ p.port }}</span>
        <span class="reel-nw">{{ p.network || 'tcp' }}</span>
        <span class="reel-se">{{ p.security || '-' }}</span>
        <span :class="'reel-lt ' + latencyClass(p.latency)" :title="p.report || ''">
            {{ latencyText(p.latency) }}
        </span>
        <span class="reel-sp" :title="p.report || ''">{{ speedCell(p.speed) }}</span>
        <span class="reel-tr"
              :title="p.up || p.down ? `上传 ${fmtBytes(p.up)} · 下载 ${fmtBytes(p.down)}` : ''">
            {{ trafficCell(p.up, p.down) }}
        </span>
        <span class="reel-gp">{{ p.group || '-' }}</span>
    </div>
</template>

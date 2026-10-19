<script setup lang="ts">
import {computed} from 'vue';
import {t} from '../../i18n';
import {type Dict} from '../../lib/metrics';

// The TUN card's body (the two kind frames); the slide div and the card's own
// title stay with SettingsDialog, which owns the carousel.
//
// React -> Vue mapping: obj/setObj 是受控状态，子组件不改它，只把补丁经
// 'patch' 事件交回（setObj({...obj, key: v}) 的等价写法）；tunOn 仍是 prop。
const props = defineProps<{
    obj: Dict;
    // Whether TUN is the mode in place right now. Only the TUN card reads it, and only to
    // say which of its rows are in force: the builder writes them for a tun front-end or
    // not at all (see BuildVPNConfig in the Go side). Handed down rather than read here
    // because the two rail switches own that state and a second copy could disagree.
    tunOn: boolean;
}>();

const emit = defineEmits<{patch: [p: Dict]}>();

// The rows of the TUN card the builder writes only for a tun front-end — the MTU,
// four of its switches and the two lists below them (see BuildVPNConfig and
// buildRoute in the Go side). With TUN not in place they are settings for something
// that is not running, so they go quiet. Quiet rather than disabled: filling them in
// before switching TUN on is the usual order of use.
//
// 隐藏核心控制台 is not among them. It is about the core's console window, not about
// the tun, and nothing in the builder reads it — it is a plain flag for the launcher.
const tunIdle = computed(() => props.tunOn ? '' : ' tun-idle');

// The MTU the field shows, which is also the MTU the core is built with: 1280 is
// IPv6's floor and the Go side applies the same one (see configbuilder.TunMTU), so a
// stored 1000 displayed with IPv6 on would name a number nothing uses. Both readings
// are taken from the same pair of keys here, so the field and the checkbox can never
// come to disagree about it.
const vpnMtu = computed(() => Math.max(Number(props.obj.vpn_mtu ?? 9000) || 9000, props.obj.vpn_ipv6 ? 1280 : 1000));

// The cache file is written for store_fakeip, so with FakeDNS off there is nothing in
// it to keep. Two conditions share one class: .tun-idle says "written only where
// conditions hold that do not at the moment", and both of them come to the same thing.
const cacheIdle = computed(() => props.tunOn && !!props.obj.fakedns ? '' : ' tun-idle');

const onMtuInput = (e: Event) => {
    const el = e.target as HTMLInputElement;
    // NekoRay clamps to 1000..10000 when the VPN dialog is
    // accepted; doing it here keeps an unusable value from
    // ever reaching the core, which rejects it at startup.
    // 1280 is the second floor, and it belongs to IPv6: an
    // interface carrying an IPv6 address cannot be given a
    // smaller link MTU, and the core refuses to build the
    // adapter when the two disagree — which reads as "TUN
    // 网卡启动失败" rather than as a number to change. So the
    // floor follows the checkbox below (see vpnMtu).
    const n = Number(el.value);
    const mtu = Number.isFinite(n)
        ? Math.min(10000, Math.max(props.obj.vpn_ipv6 ? 1280 : 1000, Math.round(n)))
        : 9000;
    emit('patch', {vpn_mtu: mtu});
    // 受控输入的回写：状态被钳到别处时，框里也跟着回到那个值。
    if (el.value !== String(mtu)) el.value = String(mtu);
};

const onIpv6Change = (e: Event) => {
    // The MTU floor goes up with this switch: a value below
    // 1280 cannot carry an IPv6 address, and the core fails
    // to build the adapter rather than saying which of the
    // two to change (see vpnMtu). Raised as it is switched
    // on, so the field keeps showing what will be used.
    const ipv6 = (e.target as HTMLInputElement).checked;
    const mtu = Number(props.obj.vpn_mtu ?? 9000) || 9000;
    emit('patch', {vpn_ipv6: ipv6, vpn_mtu: ipv6 ? Math.max(mtu, 1280) : mtu});
};

const flag = (key: string) => (e: Event) =>
    emit('patch', {[key]: (e.target as HTMLInputElement).checked});
</script>

<template>
    <div class="kind-box kind-text">
        <span class="look-box-title">{{ t('kind.tun.text') }}</span>
        <div :class="`jf-field${tunIdle}`">
            <label>{{ t('cfg.vpn_mtu') }}</label>
            <input type="number" :min="obj.vpn_ipv6 ? 1280 : 1000" max="10000"
                   :value="vpnMtu" @input="onMtuInput"/>
        </div>
        <!-- The two lists, a row each: they are the tall controls on the
            card and they hold one entry per line, so the control column
            is the most either of them ever needs — and what a list means
            depends on the switch it belongs to, which stands in the
            switches' frame below (see vpn_rule_white). -->
        <div :class="`jf-field jf-stack${tunIdle}`">
            <label>{{ t('cfg.vpn_bypass_process') }}</label>
            <textarea rows="4" :spellcheck="false"
                      :value="String(obj.vpn_bypass_process ?? '')"
                      @input="emit('patch', {vpn_bypass_process: ($event.target as HTMLTextAreaElement).value})"/>
        </div>
        <div :class="`jf-field jf-stack${tunIdle}`">
            <label>{{ t('cfg.vpn_bypass_cidr') }}</label>
            <textarea rows="4" :spellcheck="false"
                      :value="String(obj.vpn_bypass_cidr ?? '')"
                      @input="emit('patch', {vpn_bypass_cidr: ($event.target as HTMLTextAreaElement).value})"/>
        </div>
    </div>
    <div class="kind-box kind-switch">
        <span class="look-box-title">{{ t('kind.tun.switch') }}</span>
        <!-- The address-family question, asked once for everything it
            covers: whether the tun adapter is given a v6 address,
            whether answers carry AAAA, and which resolver stands when
            the reader's own is an IPv6 literal. Off means nothing is
            measured and nothing is substituted — the rows below and
            the routing card's strategy fields are written into the
            config exactly as they stand (see builder.ipv6Usable).
            Not a tun-idle row: it governs the DNS and resolver half
            under the system proxy too, where nothing is dimmed. -->
        <div class="jf-field">
            <label :title="t('hint.ipv6_auto')">{{ t('cfg.ipv6_auto') }}</label>
            <label class="jf-check">
                <input type="checkbox" :checked="obj.ipv6_auto !== false"
                       @change="flag('ipv6_auto')($event)"/>
            </label>
        </div>
        <div :class="`jf-field${tunIdle}`">
            <label :title="t('hint.vpn_ipv6')">{{ t('cfg.vpn_ipv6') }}</label>
            <label class="jf-check">
                <input type="checkbox" :checked="!!obj.vpn_ipv6" @change="onIpv6Change"/>
            </label>
        </div>
        <div :class="`jf-field${tunIdle}`">
            <label>{{ t('cfg.vpn_strict_route') }}</label>
            <label class="jf-check">
                <input type="checkbox" :checked="!!obj.vpn_strict_route" @change="flag('vpn_strict_route')($event)"/>
            </label>
        </div>
        <div :class="`jf-field${tunIdle}`">
            <label>{{ t('cfg.fakedns') }}</label>
            <label class="jf-check">
                <input type="checkbox" :checked="!!obj.fakedns" @change="flag('fakedns')($event)"/>
            </label>
        </div>
        <!-- Beside the switch it is about rather than with the other
            core_box_* keys in the core card: what it holds is the
            FakeIP mapping, and a reader who turns FakeDNS on is the one
            who wants it. The dependency is the reason it dims on two
            conditions rather than one. -->
        <div :class="`jf-field${cacheIdle}`">
            <label :title="t('hint.core_box_cache_file')">{{ t('cfg.core_box_cache_file') }}</label>
            <label class="jf-check">
                <input type="checkbox" :checked="!!obj.core_box_cache_file" @change="flag('core_box_cache_file')($event)"/>
            </label>
        </div>
        <!-- Moved in from the one-row card it used to have: it is a
            VPN-family flag like the three above, and a card holding a
            single checkbox was a heading and a border around one line.
            It is not one of the dimmed rows: nothing in the builder
            reads it for the tun. -->
        <div class="jf-field">
            <label>{{ t('cfg.vpn_hide_console') }}</label>
            <label class="jf-check">
                <input type="checkbox" :checked="!!obj.vpn_hide_console" @change="flag('vpn_hide_console')($event)"/>
            </label>
        </div>
        <!-- Moved in from a card of its own: this answers the same
            question as the flags above — which processes and which
            addresses the TUN split lets through — so it sits with them
            rather than under a heading of their own. It is what gives
            the two lists their direction: read as a list of what to
            bypass when it is off, and as the only thing to proxy when
            it is on, where everything else goes direct (see
            buildRoute). -->
        <div :class="`jf-field${tunIdle}`">
            <label>{{ t('cfg.vpn_bypass_white') }}</label>
            <label class="jf-check">
                <input type="checkbox" :checked="!!obj.vpn_rule_white" @change="flag('vpn_rule_white')($event)"/>
            </label>
        </div>
    </div>
</template>

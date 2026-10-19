<script setup lang="ts">
// The navigation rail, extracted from App.tsx (the <nav class="appnav"> block).
import {type Ref} from 'vue';
import {t} from '../i18n';
import {EarthFilled, ShieldFilled, Layout4Filled, TerminalBoxFilled, PluginFilled, InformationFilled} from '../lib/icons';

defineProps<{
    spMode: { vpn: boolean; systemProxy: boolean };
    spBusy: 'vpn' | 'systemProxy' | null;
    hasGroups: boolean;
    subsDrop: boolean;
    showLogs: boolean;
    connOpen: boolean;
    proxyEngaged: boolean;
    // The rail's subscriptions entry: resident while there is a subscription to
    // open, null in a rail with nothing behind the icon (see the conditional).
    // The ref object itself is handed over so App's Tab listener can reach it.
    subsBtnRef: Ref<HTMLButtonElement | null>;
    // Bumped on a language change so the words here are re-read (see App.vue).
    langTick: number;
}>();

const emit = defineEmits<{
    toggleSP: [kind: 'vpn' | 'systemProxy', on: boolean];
    toggleSubs: [];
    toggleLogs: [];
    openConns: [];
    about: [];
}>();
</script>

<template>
    <!-- Navigation rail: the app's sections as one column of icon buttons
        down the window's left edge — the two system switches first, the
        subscriptions entry after, panels after them, preferences last. It is
        painted over the layout rather than laid out with it, so the strip
        stays put while the table scrolls; the rest of the window is offset by
        --rail-w, which is set here and read by .app and by the readout that
        centres itself on the content area rather than on the viewport. -->
    <nav class="appnav" :aria-label="t('nav.title')">
        <!-- The rail's groups, and the rules between them: the two system
            switches lead, the subscriptions entry follows when there is a
            list to open, the two panels under those, preferences past the
            next gap. The entry is resident only on a list that has
            something in it: with no subscription there is no list to open,
            and a button that opens nothing is a promise with no room behind
            it — adding a subscription is the search box's job (a link
            pasted there and Enter-ed), so the icon owes its place to the
            list it opens. -->
        <!-- The two system switches, in a capsule of their own — now first on
            the rail, swapped with the subscriptions entry below. They are state
            rather than a section, so they light in the app's green-for-live
            rather than in the accent the section buttons use — the reading the
            status bar's chips already have, and the reason the lit one
            breathes. The capsule is what says the two of them are one thing: a
            pair of modes, mutually exclusive. -->
        <div class="sp-capsule">
            <button :class="`appnav-btn switch${spMode.systemProxy ? ' active' : ''}${spBusy === 'systemProxy' ? ' busy' : ''}`"
                    :title="t('sp.systemProxy')"
                    :aria-label="t('sp.systemProxy')"
                    :aria-pressed="spMode.systemProxy"
                    :aria-busy="spBusy === 'systemProxy'"
                    @click="$emit('toggleSP', 'systemProxy', !spMode.systemProxy)">
                <!-- The earth rather than the wireframe globe: at 15px the graticule
                    drew a grid inside a circle and read as a window, while the
                    continents are recognisable at a glance — this button is one of
                    the two the whole window is switched by. -->
                <EarthFilled class="ic"/>
            </button>
            <button :class="`appnav-btn switch${spMode.vpn ? ' active' : ''}${spBusy === 'vpn' ? ' busy' : ''}`"
                    :title="t('sp.tun')"
                    :aria-label="t('sp.tun')"
                    :aria-pressed="spMode.vpn"
                    :aria-busy="spBusy === 'vpn'"
                    @click="$emit('toggleSP', 'vpn', !spMode.vpn)">
                <ShieldFilled class="ic"/>
            </button>
        </div>
        <span class="appnav-divider"/>
        <template v-if="hasGroups">
            <!-- A grid of everything the app can show: pressing it drops
                the subscriptions box rather than switching a view, which
                is why its lit state follows the box and not the table.
                Tab is the switch here as well as the way in: while this
                button holds the focus, one Tab opens the box and the next
                puts it away, so both ends of the gesture are the same key.
                The default is prevented only then — left to itself, Tab
                would carry the focus off to the next rail button and the
                second press would never land. Shift+Tab is deliberately
                not intercepted: the positive direction is the toggle, and
                with no way back out the focus would be held by a button
                that never lets go. Tab anywhere else in the app is the
                ordinary traversal it always was. -->
            <button :class="`appnav-btn${subsDrop ? ' active' : ''}`"
                    :ref="subsBtnRef"
                    :title="t('nav.subs')"
                    :aria-label="t('nav.subs')"
                    :aria-pressed="subsDrop"
                    @click="$emit('toggleSubs')"
                    @keydown="e => {
                        if (e.key !== 'Tab' || e.shiftKey) return;
                        e.preventDefault();
                        $emit('toggleSubs');
                    }">
                <Layout4Filled class="ic"/>
            </button>
            <!-- A rule goes between groups and nowhere else — one between
                every pair of icons would draw a ladder rather than an
                order, and two of them in a row draw a thicker line. See
                .appnav-divider. -->
            <span class="appnav-divider"/>
        </template>
        <!-- The two panels: log and connections open a piece of the window, which is
            neither how traffic leaves nor what the subscriptions list holds — so
            they are their own group, standing apart from what is above them by
            the rule the subscriptions entry carries, and by their own when that
            entry is absent. The connections entry is the one that also says
            something on its own: it wears the app's green-for-live while a proxy
            mode is engaged, the same reading the run-state lamp gives the chip. -->
        <button :class="`appnav-btn${showLogs ? ' active' : ''}`"
                :title="t('log.title')"
                :aria-label="t('log.title')"
                :aria-pressed="showLogs"
                @click="$emit('toggleLogs')">
            <TerminalBoxFilled class="ic"/>
        </button>
        <button :class="`appnav-btn${connOpen ? ' active' : ''}${proxyEngaged ? ' engaged' : ''}`"
                :title="t('btn.connections')"
                :aria-label="t('btn.connections')"
                :aria-pressed="connOpen"
                @click="$emit('openConns')">
            <PluginFilled class="ic"/>
        </button>
        <!-- eats the middle of the column: sections above, preferences below -->
        <span class="appnav-gap"/>
        <span class="appnav-divider"/>
        <!-- Where this build is from, and where it keeps its things — the two
            questions a reader asks before reporting anything. It sits above
            the gear because it is the less used of the two, and a rail reads
            from the top down. -->
        <button class="appnav-btn"
                :title="t('btn.about')"
                :aria-label="t('btn.about')"
                @click="$emit('about')">
            <InformationFilled class="ic"/>
        </button>
    </nav>
</template>

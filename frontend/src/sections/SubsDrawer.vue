<script setup lang="ts">
// The subscriptions drawer, extracted from App.tsx (the subsDrop JSX block):
// the drawer panel with its grip, the subscription cards as two-row table
// entries, and the inline group editor row.
import {computed, type CSSProperties, type Ref} from 'vue';
import {main} from '../../wailsjs/go/models';
import {t} from '../i18n';
import {hueFor} from '../lib/metrics';
import {parseSubUsage, fitName} from '../lib/format';
import {EXPIRY_WARN_DAYS, fmtQuota, fmtTime, SUBS_PANEL_CLASS} from '../lib/subdraw';
import {PANEL_W_MAX, PANEL_W_MIN} from '../lib/layout';
import {flagISOOf, nameWithoutFlag} from '../components/NodeName.vue';
import Flag from '../components/Flag.vue';
import GroupEditorDialog from '../components/GroupEditorDialog.vue';
import {BookmarksFilled, Edit2Filled, Refresh1Filled, CloseCircleFilled, Delete2Filled, QrcodeFilled} from '../lib/icons';

const props = defineProps<{
    groups: main.GroupInfo[];
    gid: number;
    busy: boolean;
    subBusy: boolean;
    gConfirm: number | null;
    groupEditGid: number | null;
    nameCols: number;
    panelW: number;
    panelResizing: boolean;
    subsClosing: 'flip' | null;
    // The drawer's root element, owned by App.vue (the stack's paint effect and
    // the name-column measurement both read it).
    panelRef: Ref<HTMLDivElement | null>;
    // Handed straight through to the inline group editor, which awaits it.
    editReload: () => Promise<void>;
    // Bumped on a language change so the words here are re-read (see App.vue).
    langTick: number;
}>();

const emit = defineEmits<{
    gripDown: [e: PointerEvent];
    gripMove: [e: PointerEvent];
    gripUp: [e: PointerEvent];
    gripKey: [e: KeyboardEvent];
    gripDbl: [];
    openGroup: [id: number];
    hoverSwitch: [id: number];
    peekReel: [id: number];
    dropReel: [from: 'drawer' | 'stack'];
    updateSub: [id: number];
    deleteGroup: [id: number];
    showUrlQR: [url: string];
    toggleEdit: [id: number];
    editDone: [];
    editNotify: [text: string, kind?: 'ok' | 'err'];
}>();

// One subscription is two table rows: the name row the reader acts on, and a detail
// row under it carrying the address and the account figures the panel itself
// reported. The detail belongs to the card above it, so both rows carry the same
// zebra class and the same hover state — a subscription is one thing, not two.
// (The per-card derivations the React version computed inline in its map are
// worked out here, one decorated row per subscription.)
const rows = computed(() => props.groups.map((g, gi) => {
    // The drawer's own words ("未命名", the allowance figures' caption) are
    // spoken by this computed, so the language change's tick is read here —
    // the same idiom RoutingCard's warnings use for theirs (see langTick in
    // SettingsDialog). Without it the tooltip kept the language it opened in.
    void props.langTick;
    const usage = parseSubUsage(g.info);
    // Whether the panel said anything about the
    // account's allowance. Gates the single
    // divider: an unlimited plan with no expiry has
    // no figure to print, and a pipe in front of
    // nothing would be a mark with no meaning.
    const hasQuota = !!usage
        && (usage.total > 0 || usage.expire > 0);
    // The allowance left, as a fraction. The bar is
    // this number and nothing else: it shortens as
    // the subscription is used, which is the one
    // thing a reader scanning the list is looking
    // for. Clamped, because a panel that
    // over-reports the used side would otherwise
    // draw a bar wider than its own track.
    const quotaPct = usage && usage.total > 0
        ? Math.max(0, Math.min(100, usage.left / usage.total * 100))
        : 0;
    // The bar's colour, which is not a token and cannot be one:
    // it walks from green to red as the traffic is spent, and no
    // single palette entry does that. 130 is the green the theme
    // already uses and 0 is red, so the hue is the remaining
    // fraction of the arc between them — linear, because
    // "gradually" is the whole point of the change. Only the hue is
    // worked out here; saturation and lightness stay in App.css,
    // where they can differ between the themes.
    const quotaHue = Math.round(130 * quotaPct / 100);
    // The name cut or padded to the name column, so the
    // allowance bars down the column of cards all start at
    // the same place — and to the column's measured width
    // rather than a fixed one, so what the drawer can hold
    // is what the name gets.
    // The flag rides ahead of the fitted text: it is
    // stripped from the string fitName counts, so the
    // column arithmetic never sees the emoji, and the
    // drawn flag takes its place at the card's front.
    const flagISO = flagISOOf(g.name || '');
    const rawDrawerName = g.name || t('groups.unnamed');
    const strippedName = flagISO
        ? (nameWithoutFlag(g.name || '') || rawDrawerName)
        : rawDrawerName;
    const nameFit = fitName(strippedName, props.nameCols);
    // The card's tooltip. The allowance's exact figures
    // live here rather than on the bar, because the bar
    // is what the pointer has to be over to see them —
    // and being over the card is exactly what takes the
    // bar away and puts the actions in its place.
    const cardTitle = [
        g.name || t('groups.unnamed'),
        `${g.profileCount}`,
        usage && usage.total > 0
            ? `${t('sub.left')} ${fmtQuota(usage.left)} / ${fmtQuota(usage.total)}`
            : '',
    ].filter(Boolean).join(' · ');
    const alt = gi % 2 === 1;
    // The same seconds-or-milliseconds guess fmtTime makes, and it
    // has to be the same guess: a panel reporting milliseconds would
    // otherwise print the right date and never mark it as past.
    const expireMs = usage && usage.expire > 0
        ? (usage.expire > 1e11 ? usage.expire : usage.expire * 1000)
        : 0;
    const expired = !!expireMs && expireMs < Date.now();
    // The warning window opens a week out, and past the date it
    // stays open: the mark is one state, not two, because a
    // subscription that has just lapsed is the more urgent of
    // the two and must not go quieter exactly then.
    const expiryWarn = !!expireMs
        && (expired
            || expireMs - Date.now() <= EXPIRY_WARN_DAYS * 86400000);
    return {g, gi, usage, hasQuota, quotaPct, quotaHue, flagISO, nameFit, cardTitle, alt, expireMs, expired, expiryWarn};
}));

const hueStyle = (id: number) => ({'--tab-hue': hueFor(id)} as CSSProperties);

// No listKey bump: the list swaps
// silently, and the entrance animation
// stays the click's alone.
const cardEnter = (id: number) => {
    emit('hoverSwitch', id);
    emit('peekReel', id);
};

// the row's own action buttons sit inside
// it, and Enter or Space on one of them
// is the button's keystroke, not the
// row's — without this the same keypress
// both ran the action and switched
// subscription.
const onRowKey = (e: KeyboardEvent, id: number) => {
    if (e.key !== 'Enter' && e.key !== ' ') return;
    if (e.target instanceof HTMLElement
        && e.target.closest('.row-acts')) return;
    e.preventDefault();
    emit('openGroup', id);
};
const swallowCtx = (e: MouseEvent) => {
    // The actions are on the row now, so a
    // right click has nothing left to open.
    e.preventDefault();
    e.stopPropagation();
};
</script>

<template>
    <!-- The subscriptions drawer, pushed out from under the navigation rail
        by its grid button while the table gives up the width beside it.
        There is no catcher under it any more: a full-window layer that
        exists only to turn "clicked elsewhere" into "close" also blocks
        everything behind it, and the table beside this one is there to be
        used. It closes from the rail button, from Escape, and from picking
        a card. -->
    <div :class="SUBS_PANEL_CLASS(subsClosing)" :ref="panelRef" role="dialog" :aria-label="t('nav.subs')">
        <!-- The drawer's right edge is the handle. It sits over the
            border rather than under it, so the line the pointer aims
            at is the line it grabs. -->
        <div class="subdrop-grip"
             role="separator"
             aria-orientation="vertical"
             :title="t('panel.resize')"
             :aria-label="t('panel.resize')"
             :aria-valuenow="Math.round(panelW)"
             :aria-valuemin="PANEL_W_MIN"
             :aria-valuemax="PANEL_W_MAX"
             tabindex="0"
             @pointerdown="emit('gripDown', $event)"
             @pointermove="emit('gripMove', $event)"
             @pointerup="emit('gripUp', $event)"
             @pointercancel="emit('gripUp', $event)"
             @keydown="emit('gripKey', $event)"
             @dblclick="emit('gripDbl')"/>
        <div class="subdrop-scroll">
            <!-- The subscription list, drawn as the table it borrows its
                look from: a sticky caption row over zebra rows, each
                one a type tag in the subscription's own hue — the
                count riding inside the tag, to the right of the
                glyph, because the tag is the row's badge and the
                number belongs with the badge rather than in a column
                of its own. The name is bracketed right against the
                tag's border, and its three actions sit inline after
                it: they used to need a right click to surface, which
                hid a row's whole vocabulary behind a gesture nothing
                on the row advertised. Click to switch (and close),
                hover to sweep through them. -->
            <table class="subdrop-table">
                <thead>
                    <tr>
                        <!-- The list's own name, and it sits at the row's left
                            edge rather than in the cell beside it: the column
                            this pocket belongs to is a count of nodes with no
                            heading over it, and a caption indented past an
                            empty pocket reads as the label of a third column
                            that is not there. -->
                        <th class="cap-name">{{ t('groups.title') }}</th>
                        <!-- The caption row carries the controls that belong to
                            the list rather than to the app: re-reading it,
                            re-fetching every subscription in it, and creating
                            another one are all acts on this list, and the top bar
                            is the wrong address for them — it is a strip of global
                            controls, and these mean anything only while the list is
                            in front of you. Held at the right of the caption so they
                            sit above the allowance bars and the row actions they
                            act on, lighter action first. -->
                        <th class="cap-act">
                            <span class="cap-acts">
                                <!-- Refreshing the list and re-fetching every
                                    subscription are background timers now (the
                                    settings card carries their intervals) — the
                                    acts are no buttons. An act that no longer
                                    starts on the list has no button on it. -->
                            </span>
                        </th>
                    </tr>
                </thead>
                <tbody>
                    <template v-for="d in rows" :key="d.g.id">
                        <!-- only here so the pending hover switch can ask, when its delay
                            runs out, whether this card is still the one under the pointer -->
                        <tr :class="`sub-card${d.g.id === gid ? ' active' : ''}${d.alt ? ' alt' : ''}`"
                            tabindex="0"
                            :style="hueStyle(d.g.id)"
                            :title="d.cardTitle"
                            :data-gid="d.g.id"
                            @mouseenter="cardEnter(d.g.id)"
                            @mouseleave="emit('dropReel', 'drawer')"
                            @click="emit('openGroup', d.g.id)"
                            @keydown="onRowKey($event, d.g.id)"
                            @contextmenu="swallowCtx">
                            <td>
                                <span class="type-tag">
                                    <BookmarksFilled class="ic"/>
                                    <span class="tag-count">{{ d.g.profileCount }}</span>
                                </span>
                            </td>
                            <td class="name">
                                <!-- The name in a fixed column, with the
                                    filler kept in its own hidden span:
                                    spaces hold the column's width and are
                                    not part of the name. The full name is
                                    in the tooltip, since a name longer than
                                    the column is cut short here. The name
                                    shows as the panel gave it — no wrapper
                                    brackets of the app's own. -->
                                <span class="sub-label" dir="auto"
                                      :title="d.g.name || t('groups.unnamed')"><Flag v-if="d.flagISO" :iso="d.flagISO"/>{{ d.nameFit.text }}<span aria-hidden="true">{{ d.nameFit.pad }}</span></span>
                                <!-- The hairline between the name and the
                                    slot: the same weight as the rule between
                                    the card's two columns, and full height, so
                                    it divides the row rather than marking a
                                    point in it. Decorative: it says nothing a
                                    reader needs. -->
                                <span class="sub-sep" aria-hidden="true"/>
                                <!-- One slot at the end of the name, read two
                                    ways. At rest it is the allowance: that is
                                    the figure a column of cards is scanned
                                    for, and it is worth more than three
                                    buttons nobody is aiming at. Under the
                                    pointer — or under the keyboard — it
                                    becomes the actions, which is the only
                                    moment they can be used. The two are
                                    stacked in the same grid cell so the slot
                                    takes the wider of them and nothing
                                    shifts as they trade places. -->
                                <span class="row-tail">
                                    <span v-if="d.usage && d.usage.total > 0" class="sub-quota"
                                          role="img"
                                          :aria-label="`${t('sub.left')} ${fmtQuota(d.usage.left)} / ${fmtQuota(d.usage.total)}`">
                                        <!-- The bar is a picture of a number, so
                                            the number goes in the label: without
                                            it a screen reader gets an empty span.
                                            No title, though — a tooltip on the
                                            bar is a tooltip nobody can reach, since
                                            the pointer being on the card is exactly
                                            what takes the bar away. The figures are
                                            on the card's own tooltip instead. -->
                                        <span class="sub-quota-fill"
                                              :style="{width: d.quotaPct + '%', '--quota-hue': d.quotaHue}"/>
                                    </span>
                                    <!-- The row's whole vocabulary. Colours
                                        carry the meaning here as they did in
                                        the menu: blue edits, green
                                        refreshes, red destroys. -->
                                    <span class="row-acts">
                                        <button class="sub-act edit"
                                                :title="t('btn.edit')"
                                                :aria-label="t('btn.edit')"
                                                @click.stop="emit('toggleEdit', d.g.id)">
                                            <!-- Toggle the card's own editor: the
                                                row grows to hold it under this
                                                group, and a second press folds it. -->
                                            <Edit2Filled class="ic"/>
                                        </button>
                                        <button v-if="d.g.url" class="sub-act refresh"
                                                :title="t('btn.updateSub')"
                                                :aria-label="t('btn.updateSub')"
                                                :disabled="busy || subBusy"
                                                @click.stop="emit('updateSub', d.g.id)">
                                            <Refresh1Filled class="ic"/>
                                        </button>
                                        <button :class="`sub-act danger${gConfirm === d.g.id ? ' solid' : ''}`"
                                                :title="gConfirm === d.g.id ? t('btn.confirmDelete') : t('btn.delete')"
                                                :aria-label="t('btn.delete')"
                                                @click.stop="emit('deleteGroup', d.g.id)">
                                            <CloseCircleFilled v-if="gConfirm === d.g.id" class="ic"/>
                                            <Delete2Filled v-else class="ic"/>
                                        </button>
                                        <!-- After the destroy button, as asked. It is the one
                                            action in this row that shows something rather
                                            than changing it — the subscription's address,
                                            in the form a phone can read off the screen — so
                                            it wears none of the three verbs' colours and
                                            keeps the base one. The hairline that separates
                                            a pair of actions comes with it: it is drawn from
                                            the button rather than declared per pair.
                                            Only when there is an address to encode: a group
                                            put together by hand has nothing to show. -->
                                        <button v-if="d.g.url" class="sub-act"
                                                :title="t('btn.qr')"
                                                :aria-label="t('btn.qr')"
                                                @click.stop="emit('showUrlQR', d.g.url)">
                                            <QrcodeFilled class="ic"/>
                                        </button>
                                    </span>
                                </span>
                            </td>
                        </tr>
                        <!-- The hue travels with the card, not with the
                            name row: the selected stripe is drawn on both
                            halves, and a half that had to fall back to the
                            default hue would come out a different colour
                            from the half above it. -->
                        <!-- The lower half of the card is the card: the
                            switch is what entering it means, and it counts
                            here too — reaching a card from below crosses
                            its figures before it reaches its name. The
                            current subscription falls straight out again in
                            hoverSwitch, so moving about inside one card
                            costs nothing. -->
                        <tr :class="`sub-detail${d.alt ? ' alt' : ''}${d.g.id === gid ? ' active' : ''}`"
                            :style="hueStyle(d.g.id)"
                            :data-gid="d.g.id"
                            @mouseenter="cardEnter(d.g.id)"
                            @mouseleave="emit('dropReel', 'drawer')"
                            @click="emit('openGroup', d.g.id)"
                            @contextmenu="swallowCtx">
                            <!-- No address on the card. It was the one line on the
                                row that is neither about the account nor about
                                what the subscription holds, it is long enough to
                                be the row's whole width, and nothing here acts on
                                it — the edit panel has the field for the times it
                                really is needed. -->
                            <td colspan="2">
                                <div class="sub-meta">
                                    <span class="sub-meta-item">
                                        {{ t('sub.lastUpdate') }} {{ fmtTime(d.g.subLastUpdate) }}
                                    </span>
                                    <!-- The one divider on the line. The update
                                        time is a fact about this
                                        subscription's own bookkeeping; the
                                        figures after it are the account
                                        behind it, and only that change of
                                        kind is worth a mark. -->
                                    <span v-if="d.hasQuota" class="sub-meta-sep">|</span>
                                    <!-- No "used" figure, deliberately. What is
                                        left of the allowance is the number a
                                        subscription is judged by, and it is
                                        the only one of the two that can act
                                        as a warning: a plan with 40 hours
                                        left is a different situation from one
                                        that has transferred 40 hours, and
                                        only the first is actionable. The used
                                        total is still counted — it is how the
                                        remainder is worked out — it just is
                                        not printed. -->
                                    <!-- The allowance bar is not here: it
                                        sits in the slot at the end of the
                                        name row above, where it can be read
                                        down a column and where it gives way
                                        to the actions. -->
                                    <span v-if="d.usage && d.usage.expire > 0"
                                          :class="`sub-meta-item expiry${d.expiryWarn ? ' warn' : ''}`">
                                        <!-- The one figure on the card with a
                                            deadline, and the only one that gets
                                            the warning mark: past it the node list
                                            it feeds stops refreshing. -->
                                        {{ t('sub.expire') }} {{ fmtTime(d.usage.expire) }}
                                    </span>
                                </div>
                            </td>
                        </tr>
                        <tr v-if="groupEditGid === d.g.id"
                            :class="`sub-edit${d.alt ? ' alt' : ''}`"
                            :style="hueStyle(d.g.id)">
                            <!-- The edit page under the group's own name: the
                                third row of the same card, which the card's
                                height simply grows to hold. It keeps the
                                card's hue so the three rows read as one. -->
                            <td colspan="2">
                                <GroupEditorDialog :gid="d.g.id"
                                                   :reload="editReload"
                                                   @done="emit('editDone')"
                                                   @notify="(text: string, kind?: 'ok' | 'err') => emit('editNotify', text, kind)"/>
                            </td>
                        </tr>
                    </template>
                </tbody>
            </table>
            <div v-if="groups.length === 0" class="subdrop-empty">{{ t('groups.none') }}</div>
        </div>
    </div>
</template>

<script setup lang="ts">
// The log panel, extracted from App.tsx (the showLogs JSX block): the head
// strip (pause / level / copy / clear / collapse), the filter column beside
// the log, the legend, and the lines themselves.
import {computed, type Ref} from 'vue';
import {t} from '../i18n';
import {CONSOLE_MAX_SHARE, CONSOLE_MIN_H} from '../lib/layout';
import {LOG_CATEGORIES, LOG_FILTERS, LOG_LEVELS, lineLevelOf, sectionText, splitStamp, type ShownLine} from '../lib/logfilter';
import {CopyFilled, Delete3Filled, LayoutBottomCloseFilled, PauseFilled, PlayFilled} from '../lib/icons';

// The aria-valuemax of the grip: the window's own answer, read per render
// rather than once at mount. The panel is not remounted when the window is
// resized, so a sampled value went stale — a reader with a screen reader was
// told a maximum that no longer bounded anything.
const maxConsoleH = () => Math.round(window.innerHeight * CONSOLE_MAX_SHARE);

const props = defineProps<{
    consoleH: number | null;
    consoleDragging: boolean;
    consoleNow: number;
    logPaused: boolean;
    logFilter: string;
    logLevel: string;
    catLive: Record<string, boolean>;
    shownLogs: ShownLine[];
    // The panel element and the scroll box, owned by App.vue's composables
    // (the drag reads the panel, the follow logic reads the box).
    consoleRef: Ref<HTMLElement | null>;
    logRef: Ref<HTMLDivElement | null>;
    // Bumped on a language change so the words here are re-read (see App.vue).
    langTick: number;
}>();

const emit = defineEmits<{
    gripDown: [e: PointerEvent];
    gripMove: [e: PointerEvent];
    gripUp: [e: PointerEvent];
    gripKey: [e: KeyboardEvent];
    gripDbl: [];
    togglePause: [];
    setLevel: [v: string];
    pickFilter: [id: string];
    copy: [];
    clear: [];
    collapse: [];
    scroll: [];
    bodyCtx: [at: { x: number; y: number }];
    lineCtx: [at: { x: number; y: number; line: string }];
}>();

// A run's heading is a divider between runs, not one
// category's output: no colour rule, dimmed, and set
// apart by a hairline instead of by punctuation.
// Severity lifts a line out of its category: an error
// is an error in every stream, so where the line says
// so the alarm colour goes on instead of the category's.
// Warnings sit between the two. The level is the
// filter's own answer for this very line (lineLevelOf),
// so what wears the colour and what the dropdown files
// under 错误 are one set — which they were not while
// the two were separate rules.
const views = computed(() => props.shownLogs.map(l => {
    const [stamp, msg] = splitStamp(l.line);
    const head = sectionText(msg);
    const lvl = lineLevelOf(l.line);
    const bad = head === null && lvl === 'error';
    const warn = head === null && lvl === 'warn';
    const cls = `logline${head === null ? ` cat-${l.cat}` : ' section'}${bad ? ' err' : warn ? ' warn' : ''}`;
    return {l, stamp, head, msg, cls};
}));
</script>

<template>
    <section :class="`console${consoleDragging ? ' resizing' : ''}`"
             :ref="consoleRef"
             :style="consoleH ? {height: consoleH + 'px'} : undefined">
        <!-- The panel's top edge is the handle. It sits over the border
            rather than under it, so the line the pointer aims at is the
            line it grabs. -->
        <div class="console-grip"
             role="separator"
             aria-orientation="horizontal"
             :title="t('log.resize')"
             :aria-label="t('log.resize')"
             :aria-valuenow="consoleNow"
             :aria-valuemin="CONSOLE_MIN_H"
             :aria-valuemax="maxConsoleH()"
             tabindex="0"
             @pointerdown="emit('gripDown', $event)"
             @pointermove="emit('gripMove', $event)"
             @pointerup="emit('gripUp', $event)"
             @pointercancel="emit('gripUp', $event)"
             @keydown="emit('gripKey', $event)"
             @dblclick="emit('gripDbl')"/>
        <div class="console-head">
            <!-- The head is the panel's own strip and nothing else: what it
                is, and the one way out of it. Everything about the lines —
                which of them are showing, and what their colours mean — is
                down in the log itself, where the lines are. -->
            <!-- Holding the stream still, at the head's left end where the
                reading starts. The glyph is the state rather than the
                action: a pause bar while the panel is following, a play
                triangle while it is not, so what is on screen says which
                it is doing without a tooltip. -->
            <button :class="`btn tiny ghost icon-only log-pause${logPaused ? ' on' : ''}`"
                    :title="t(logPaused ? 'log.resume' : 'log.pause')"
                    :aria-label="t(logPaused ? 'log.resume' : 'log.pause')"
                    :aria-pressed="logPaused"
                    @click="emit('togglePause')">
                <PlayFilled v-if="logPaused" class="ic"/>
                <PauseFilled v-else class="ic"/>
            </button>
            <span class="console-title">{{ t('log.title') }}</span>
            <!-- How loud a line has to be to be shown, beside the title because
                it is a property of the whole panel rather than of a line.
                One dropdown instead of the four icons: its options name each
                level in words, the core's own keywords stay as the option
                values, which is what a line is matched against (see
                LOG_LEVELS and LINE_LEVEL). The chosen one wears the colour
                its lines will wear below. -->
            <select class="log-level" :value="logLevel"
                    :data-lvl="logLevel"
                    :title="t('log.levelHint')"
                    :aria-label="t('log.levelHint')"
                    @change="emit('setLevel', ($event.target as HTMLSelectElement).value)">
                <option v-for="l in LOG_LEVELS" :key="l.id" :value="l.id">{{ t(l.label) }}</option>
            </select>
            <!-- Copying the whole panel, the head's own button for what the
                right-click menu offers on the text: the act is about the panel
                rather than about a line, and reaching for it over the menu
                asked for three presses and a context. The clipboard writer
                itself is copyLogs, shared with the menu item. -->
            <button class="btn tiny ghost icon-only log-copy"
                    :title="t('log.copy')"
                    :aria-label="t('log.copy')"
                    @click="emit('copy')">
                <CopyFilled class="ic"/>
            </button>
            <!-- Clearing the panel. This was an item of the log's own right-click
                menu — opened on the text, three items down — and the act is about
                the whole panel rather than about a line, so the panel's own strip
                is where its control belongs. It stands before the collapse rather
                than after it: the far right is the collapse's place, being the one
                control that is not about the log's content. -->
            <button class="btn tiny ghost icon-only log-clear"
                    :title="t('log.clearAll')"
                    :aria-label="t('log.clearAll')"
                    @click="emit('clear')">
                <Delete3Filled class="ic"/>
            </button>
            <!-- Collapse sits at the far right: it is the last thing you
                want and the only control that is not about the log's
                content, so the margin-left:auto parks it there however
                wide the rest of the strip gets. -->
            <button class="btn tiny ghost icon-only console-collapse"
                    :title="t('log.collapse')"
                    :aria-label="t('log.collapse')"
                    @click="emit('collapse')">
                <LayoutBottomCloseFilled class="ic"/>
            </button>
        </div>
        <!-- The filter and the log it filters, side by side: the choices are
            a column rather than a menu, so which one is in effect and what
            else there is to choose are both readable without opening
            anything, and the panel cannot cover the lines it is filtering. -->
        <div class="console-main">
            <div class="log-picker" role="radiogroup"
                 :aria-label="t('log.filter')">
                <button v-for="f in LOG_FILTERS" :key="f.id"
                        role="radio"
                        :aria-checked="logFilter === f.id"
                        :class="`log-filter-item${logFilter === f.id ? ' on' : ''}`"
                        @click="$emit('pickFilter', f.id)">
                    <!-- A word each, and nothing else: the three are a choice
                        between what to show. The icon above each one said
                        what the word did, at the cost of a column of the
                        panel's narrow width; the tick beside the chosen one
                        said what its colour says, at the cost of another.
                        The colour is the one mark left — it is the mark the
                        eye reads without being taught, and in a column this
                        narrow it is the mark that costs nothing. -->
                    <span>{{ t(f.label) }}</span>
                </button>
            </div>
            <!-- The key to the colours and the lines it explains, in one
                column: the key sits at the log's top right — the eye
                arrives at the lines' left edge first, so the explanation
                waits on the far side of them — and stays put while the
                lines scroll under it. -->
            <div class="console-log">
                <div class="log-legend">
                    <span v-for="c in LOG_CATEGORIES" :key="c.id" :class="`log-key cat-${c.id}${catLive[c.id] ? ' live' : ''}`">
                        <i class="dot"/>{{ t(c.label) }}
                    </span>
                </div>
                <div class="console-body" :ref="logRef"
                     @scroll="emit('scroll')"
                     @contextmenu.prevent="emit('bodyCtx', {x: $event.clientX, y: $event.clientY})">
                    <div v-for="v in views" :key="v.l.key" :class="v.cls"
                         @contextmenu.prevent.stop="emit('lineCtx', {x: $event.clientX, y: $event.clientY, line: v.l.line})">
                        <!-- The line carries its own menu, over the panel's —
                           which stays as the background's. Stopping the
                           bubble is what keeps the panel from replacing this
                           menu with the one that has no line in it. -->
                        <!-- Always rendered, empty or not: the fixed-width
                            stamp column keeps every message starting on
                            the same x, whether or not its line has a time. -->
                        <span class="stamp">{{ v.stamp }}</span>
                        <span class="msg">{{ v.head ?? v.msg }}</span>
                    </div>
                    <div v-if="shownLogs.length === 0" class="logline dim">
                        {{ logFilter === 'all' ? t('log.empty') : t('log.noMatch') }}
                    </div>
                </div>
            </div>
        </div>
    </section>
</template>

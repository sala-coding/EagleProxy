<script setup lang="ts">
// Generic JSON object editor. Renders an input per field whose kind is derived
// from the value type, recursing into nested objects. This lets every protocol
// and settings field be edited without hardcoding per-field forms.
//
// React -> Vue mapping of the props: value/onChange 是受控状态，映射为
// v-model（defineModel）；extraRows 的两个 ReactNode 改为 'extra-text' 与
// 'extra-switch' 具名插槽；onHotkeyCapture 改为 'hotkey-capture' 事件；
// acceptHotkey 的返回值会被 await，故保留为函数 prop。
// 纯函数辅助（fieldLabel/fieldHint/LONG_KEYS 等）平移到了 ./lib/jsonform.ts，
// 行渲染拆分为 ./JsonFormRow.vue。
import {computed, useSlots} from "vue";
import {t} from "./i18n";
import {type Json} from "./lib/jsonform";
import KindFrame from "./KindFrame.vue";
import JsonFormRow from "./JsonFormRow.vue";

const props = withDefaults(defineProps<{
    depth?: number;
    // How many fields share a line. 1 is a row each, which is what every group wants
    // by default; a group of short values in a full-width card wants 2 or 3, or each
    // control is stretched across the window to hold two digits. Read by CSS through
    // data-cols so the grid lives with the rest of the form's geometry.
    cols?: number;
    // The keys that keep a row of their own whatever cols says: the tall control in a
    // card of short ones. A column is the width of the shortest value beside it, and a
    // textarea or a list squeezed into one is a column of wrapped lines. Full width
    // rather than ignored, so nothing in the card is lost to the grid.
    fullRow?: string[];
    // The rows drawn as two frames — the fields in one, the switches in another, each
    // named on its own border by the kind it holds (see .kind-box): the fields where a
    // value the reader fills in belongs, the switches where the switches are. What every
    // settings card asks of the forms it renders: a card holding both kinds is two
    // questions read as one list, and a reader after the switch they half-remember should
    // not have to read twelve labels to find it. A card holding one kind gets the one
    // frame. The rows keep their own names in the card's label column — the frame's
    // legend says what kind they are, not what they are.
    grouped?: boolean;
    // The legends of the two drawn frames, when the card names them by their subject
    // rather than by their kind: the card's own title already says what the page holds,
    // and "文本框"/"开关" twice more under it says nothing any page above does not.
    // Whatever the caller does not name falls back to the kind's own word.
    frames?: {text?: string; switch?: string};
    // Asked before a captured combination is recorded, and the answer is the caller's:
    // it holds the other rows — a combination already set on one of them is a collision
    // this form cannot see — and it is the side that can say so in a dialog. False leaves
    // the field as it was.
    acceptHotkey?: (key: string, combo: string) => Promise<boolean>;
}>(), {depth: 0, cols: 1});

// Called when a hotkey field starts and stops listening, for the one thing a capture
// needs from outside this form: the app's own global hotkeys have to be out of the
// way while one is being set (see SuspendHotkeys). Handed in rather than called from
// here so this file keeps knowing nothing about the backend.
const emit = defineEmits<{hotkeyCapture: [active: boolean]}>();

const value = defineModel<Record<string, Json>>({required: true});
const slots = useSlots();

const rows = computed<[string, Json][]>(() => Object.entries(value.value ?? {}));
// A boolean is a switch and stands in the switches' frame; everything else — a text
// field, a number, a list of lines, a nested block of them — is a value the reader
// fills in, and stands in the fields' frame. A nested block goes in whole: the
// switches inside it are part of the value it holds rather than rows of this form, and
// taking them out of it would take them away from the block that names them (see
// .jf-nested).
const fields = computed(() => rows.value.filter(([, v]) => typeof v !== 'boolean'));
const switches = computed(() => rows.value.filter(([, v]) => typeof v === 'boolean'));

const set = (k: string, v: Json) => { value.value = {...value.value, [k]: v}; };
const onSet = (k: string, v: Json) => set(k, v);
const onHotkeyCapture = (active: boolean) => emit('hotkeyCapture', active);
</script>

<template>
    <!-- data-cols is the grid in which the rows share a line. The frames are the card's
         own blocks, one to a line, so the grid is left off when they are what is drawn. -->
    <div class="jsonform" :data-depth="depth"
         :data-cols="!grouped && cols > 1 ? cols : undefined">
        <template v-if="grouped">
            <KindFrame v-if="fields.length > 0 || slots['extra-text']" kind="text"
                       :label="frames?.text ?? t('settings.kind.text')">
                <slot name="extra-text"/>
                <JsonFormRow v-for="[k, v] in fields" :key="k" :k="k" :v="v"
                             :depth="depth" :full-row="fullRow" :accept-hotkey="acceptHotkey"
                             @set="onSet" @hotkey-capture="onHotkeyCapture"/>
            </KindFrame>
            <KindFrame v-if="switches.length > 0 || slots['extra-switch']" kind="switch"
                       :label="frames?.switch ?? t('settings.kind.switch')">
                <slot name="extra-switch"/>
                <JsonFormRow v-for="[k, v] in switches" :key="k" :k="k" :v="v"
                             :depth="depth" :full-row="fullRow" :accept-hotkey="acceptHotkey"
                             @set="onSet" @hotkey-capture="onHotkeyCapture"/>
            </KindFrame>
        </template>
        <template v-else>
            <JsonFormRow v-for="[k, v] in rows" :key="k" :k="k" :v="v"
                         :depth="depth" :full-row="fullRow" :accept-hotkey="acceptHotkey"
                         @set="onSet" @hotkey-capture="onHotkeyCapture"/>
        </template>
    </div>
</template>

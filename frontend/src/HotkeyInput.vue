<script setup lang="ts">
import {ref, watch} from 'vue';
import {t} from "./i18n";
import {captureName, heldModifiers} from "./lib/jsonform";

// A key combination, recorded by pressing it. The stored spelling ("Ctrl+Alt+X") is the
// backend's, and typing it by hand is the kind of thing that looks right and is not —
// so the field cannot be typed into at all: it is either showing a combination or
// waiting for one.
const props = defineProps<{
    hotkeyKey: string;
    // acceptHotkey is asked before a combination is recorded and its answer is
    // awaited, so it stays a callable prop rather than an emit: an emit cannot
    // hand the boolean back.
    acceptHotkey?: (key: string, combo: string) => Promise<boolean>;
}>();

// value/onChange 是受控状态，映射为 v-model（defineModel）；onCapture -> 'capture'。
const emit = defineEmits<{capture: [active: boolean]}>();
const value = defineModel<string>({required: true});

const listening = ref(false);
// What the control is saying back: the modifiers being held, so far, or why the last
// press was refused.
const status = ref('');

// While this field listens, the app must not be holding any hotkey: Windows delivers
// a registered combination to this process and passes nothing on, so the field would
// never see the press for the very combination it is showing.
//
// The cleanup is what pairs the two up, and it runs on unmount as well as on losing
// focus – a sheet closed mid-capture would otherwise leave the reader with no hotkeys
// at all and no sign of why. Under Vue the emit is stable, so the old callback-ref
// dance is not needed.
watch(listening, (on, _, onCleanup) => {
    if (!on) return;
    emit('capture', true);
    onCleanup(() => emit('capture', false));
});

const press = async (e: KeyboardEvent) => {
    if (!listening.value) return;
    // The field's own while it listens. Tab would move on to the next row, Space
    // would scroll the dialog, Enter would do whatever the row does. The browser's
    // own shortcuts — Ctrl+W, F5 — cannot be taken back here for the same reason they
    // cannot be registered as global hotkeys.
    e.preventDefault();
    // And it does not travel on, either. The settings sheet dismisses itself on
    // Escape from a listener on the window (see Modal), so without this the press
    // that cancels a capture would close the dialog — and take every unsaved edit in
    // it. Stopping it here, at the field that is listening, leaves that dismissal
    // alone everywhere else.
    e.stopPropagation();
    if (e.key === 'Backspace' || e.key === 'Delete') {
        // Give up the combination rather than try to record it: there is no such key
        // in the vocabulary, and a field that can only be filled is one that cannot
        // be emptied.
        value.value = '';
        listening.value = false;
        status.value = '';
        return;
    }
    const name = captureName(e.key);
    const mods = heldModifiers(e);
    if (!name) {
        // A modifier on its own: say what is held and wait for the key it is for.
        status.value = mods ? mods + '+…' : '…';
        return;
    }
    // Escape gives up while no modifier is held — which is what it means in every
    // other capture field — and is a key like any other once one is.
    if (name === 'Esc' && !mods) {
        listening.value = false;
        status.value = '';
        return;
    }
    if (!mods && !/^F([1-9]|1[0-9]|2[0-4])$/.test(name)) {
        // A key on its own is registered on its own, which takes it from every other
        // program on the machine: a bare Tab would stop moving the caret anywhere, and
        // the reader who set it would have no way back to this dialog. Function keys
        // are the exception, because that is the one shape people mean by a bare key.
        status.value = t('hk.needMod');
        return;
    }
    const combo = mods ? mods + '+' + name : name;
    // Asked before it is recorded, and asked while this field is still listening — the
    // order matters: listening is what holds the app's own hotkeys suspended, and the
    // combination the reader is most likely to press is the one already in this row.
    // Let go first and that press would be answered by our own registration, as a
    // conflict with nobody.
    if (props.acceptHotkey && !(await props.acceptHotkey(props.hotkeyKey, combo))) {
        listening.value = false;
        status.value = '';
        return;
    }
    value.value = combo;
    // Listening ends either way: whether the combination is taken is the answer to a
    // question, not a reason to stand here waiting for another press.
    listening.value = false;
    status.value = '';
};
</script>

<template>
    <input :class="`hk-input${listening ? ' listening' : ''}`"
           readonly
           :value="listening ? (status || t('hk.press')) : value"
           :placeholder="t('hk.unset')"
           @focus="listening = true"
           @blur="listening = false; status = ''"
           @keydown="press"/>
</template>

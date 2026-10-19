<script setup lang="ts">
import {ref} from 'vue';
import {t} from "../i18n";
import {CloseFilled, SaveFilled} from '../lib/icons';
import JsonForm from "../JsonForm.vue";
import Modal from "./Modal.vue";
import {type Dict} from "../lib/metrics";
import {errText} from "../lib/shared";

const props = defineProps<{
    title: string;
    initial: Dict;
    // onSave is awaited — its failure has to land in the error line and keep the
    // dialog open — so it stays a callable prop rather than an emit: an emit
    // cannot hand the promise back to this two-step save.
    onSave: (v: Dict) => Promise<void>;
    // Walking away without saving. Handed in by the add flow, which creates the
    // record before this dialog opens: a cancel takes the record back, so
    // "新增" leaves nothing behind when the reader does not save.
    onDiscard?: () => Promise<void>;
}>();

const emit = defineEmits<{close: []}>();

const obj = ref<Dict>(props.initial);
const err = ref('');
const saving = ref(false);

const save = async () => {
    // One press, one save: a double click on the button must not write the
    // same profile twice (the second write races the dialog already closing).
    if (saving.value) return;
    saving.value = true;
    try {
        err.value = '';
        await props.onSave(obj.value);
        emit('close');
    } catch (e) {
        err.value = errText(e);
        saving.value = false;
    }
};

const cancel = async () => {
    await props.onDiscard?.();
    emit('close');
};
</script>

<template>
    <Modal :title="title" @close="cancel()" wide>
        <div class="editor-body">
            <JsonForm v-model="obj"/>
        </div>
        <div v-if="err" class="errline">{{ err }}</div>
        <div class="modal-foot">
            <button class="btn ghost" @click="cancel()"><CloseFilled class="ic"/>{{ t('btn.cancel') }}</button>
            <button class="btn primary" :disabled="saving" @click="save"><SaveFilled class="ic"/>{{ t('btn.save') }}</button>
        </div>
    </Modal>
</template>

<script setup lang="ts">
import {ref, watch} from "vue";
import * as API from "../../wailsjs/go/main/App";
import {db, main} from "../../wailsjs/go/models";
import {t} from "../i18n";
import {tip} from "../tips";
import {errText} from "../lib/shared";
import {CheckFilled, CloseFilled, LoadingFilled} from "../lib/icons";

const props = defineProps<{
    gid: number;
    // reload is awaited between the save and the "saved" toast, so it stays a
    // callable prop rather than an emit: an emit cannot hand the promise back.
    reload: () => Promise<void>;
}>();

// onDone -> 'done' (collapse the editing row); notify -> 'notify'.
const emit = defineEmits<{
    done: [];
    notify: [t: string, k?: "ok" | "err"];
}>();

const g = ref<any>(null);
const profiles = ref<main.ProfileInfo[]>([]);
const err = ref("");
const busy = ref(false);

watch(() => props.gid, (gid, _, onCleanup) => {
    // The answer may come back after the row has collapsed or its gid
    // swapped: a late setG would paste a dead group over the open form.
    let alive = true;
    onCleanup(() => { alive = false; });
    (async () => {
        try {
            const grp: any = await API.GetGroup(gid);
            if (!alive) return;
            g.value = grp;
            const ps = await API.ListProfiles(gid);
            if (!alive) return;
            profiles.value = ps;
        } catch (e) {
            if (!alive) return;
            emit("notify", errText(e), "err");
            emit("done");
        }
    })();
}, {immediate: true});

const set = (k: string, v: unknown) => { g.value = {...g.value, [k]: v}; };

const save = async () => {
    try {
        err.value = "";
        busy.value = true;
        await API.UpdateGroup(new db.Group(g.value));
        await props.reload();
        emit("notify", tip("group.saved"));
        emit("done");
    } catch (e) {
        err.value = errText(e);
    } finally {
        busy.value = false;
    }
};
</script>

<template>
    <!-- The group editor, asked to live inside the subscription card: the list row
         grows, the form unfolds under the group's own name, and saving folds it back.
         The interactive shape (the fields, the load, the two-step save) is the one the
         old held dialog used; only the walls around it are gone. -->
    <div class="sub-edit-body" v-if="g" @click.stop>
        <div class="gform">
            <label class="gf-row">
                <span>{{ t("groups.name") }}</span>
                <input :value="g.name ?? ''" :placeholder="t('groups.unnamed')"
                       @input="set('name', ($event.target as HTMLInputElement).value)"/>
            </label>
            <label class="gf-row">
                <span>{{ t("col.subUrl") }}</span>
                <input :value="g.url ?? ''" placeholder="https://example.com/sub"
                       @input="set('url', ($event.target as HTMLInputElement).value)"/>
            </label>
            <label class="gf-row">
                <span>{{ t("groups.frontProxy") }}</span>
                <select :value="g.front_proxy_id ?? -1"
                        @change="set('front_proxy_id', Number(($event.target as HTMLSelectElement).value))">
                    <option :value="-1">{{ t("groups.noFrontProxy") }}</option>
                    <option v-for="p in profiles" :key="p.id" :value="p.id">{{ p.name || p.address }}</option>
                </select>
            </label>
            <label class="gf-row">
                <span>{{ t("groups.interval") }}</span>
                <input type="number" min="0" step="5" :value="g.sub_interval ?? 0"
                       @input="set('sub_interval', Number(($event.target as HTMLInputElement).value))"/>
            </label>
        </div>
        <div class="errline" v-if="err">{{ err }}</div>
        <div class="sub-edit-foot">
            <button class="btn ghost" @click="emit('done')">
                <CloseFilled class="ic"/>{{ t("btn.cancel") }}
            </button>
            <button class="btn primary" :disabled="busy" @click="save">
                <LoadingFilled v-if="busy" class="ic spin"/><CheckFilled v-else class="ic"/>{{ t("btn.save") }}
            </button>
        </div>
    </div>
</template>

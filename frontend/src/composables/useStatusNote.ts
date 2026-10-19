// The status bar's one text slot and the floating message window, extracted
// verbatim from App.tsx (the statusNote block). One instance lives in App.vue;
// notify is handed down to the dialogs as a callable prop, the way the React
// version handed the same closure down.
import {onBeforeUnmount, ref} from 'vue';
import {NOTE_MS, NOTE_RETRACT_MS} from '../lib/logpanel';

export type StatusNote = {text: string; kind: 'ok' | 'err'; seq: number; leaving: boolean; mode: 'bar' | 'window'};

export function useStatusNote() {
    // The status bar's one text slot, at its left edge: every transient word —
    // notify toasts, the run state on hover, "node connected" when a start
    // lands — grows out of the rail's edge into this same place, and fades where
    // it stands when its time is up. A sequence key restarts the growth when one
    // message replaces another.
    //
    // One at a time, and the newest one wins: a note in the slot is replaced by
    // the next press the moment it arrives, timers rewound and the grow-in
    // replayed (the seq key does that). They used to queue — the head kept the
    // slot its full four seconds while the ones behind waited their turn, and a
    // burst of them read as the window answering minutes late. Real-time
    // overwrite is the reading that matches the actions: the last word said is
    // the state the machine is in now.
    // mode: where the note is shown. 'bar' is the status bar's own text slot
    // (hover hints, a wait in progress); 'window' is the separate floating
    // window centred on the main window, which is what every transient toast
    // (notify / app:message / connected) uses now.
    const statusNote = ref<StatusNote | null>(null);
    // Counts notes rather than stamping them with the clock: two of them can land in
    // the same millisecond, and the key they carry is what restarts the grow-in.
    let noteSeq = 0;
    const noteTimers: {t1?: number; t2?: number} = {};
    const clearNoteTimers = () => {
        clearTimeout(noteTimers.t1);
        clearTimeout(noteTimers.t2);
    };
    const showStatusNote = (text: string, kind: 'ok' | 'err' = 'ok', mode: 'bar' | 'window' = 'bar') => {
        clearNoteTimers();
        statusNote.value = {text, kind, seq: ++noteSeq, leaving: false, mode};
        noteTimers.t1 = window.setTimeout(
            () => { if (statusNote.value) statusNote.value = {...statusNote.value, leaving: true}; }, NOTE_MS);
        noteTimers.t2 = window.setTimeout(() => {
            statusNote.value = null;
        }, NOTE_MS + NOTE_RETRACT_MS);
    };
    const hideStatusNote = () => {
        clearNoteTimers();
        // The queue goes with it: this is the pointer leaving the run-state chip, and
        // what the pointer left behind is not something the reader is owed.
        statusNote.value = null;
    };
    // Neither timer may outlive the component: the second one reaches into state.
    onBeforeUnmount(clearNoteTimers);

    // The same slot, without the retract: the word stays until something replaces
    // it or clears it. For the waits this app can name — a mode switch is the one —
    // a timed entry would take the text away in the middle of the wait, which is
    // exactly the part someone is reading it for. Nothing clears it here because
    // nothing has to: every switch path ends in a message of its own (the backend's
    // for switching on, which arrives before the call even returns; this side's for
    // switching off), and that message lands in the same slot — overwriting this
    // one the instant it arrives, the way every other note does now.
    const holdStatusNote = (text: string, kind: 'ok' | 'err' = 'ok') => {
        clearNoteTimers();
        statusNote.value = {text, kind, seq: ++noteSeq, leaving: false, mode: 'bar'};
    };

    // Every notify lands in the floating message window centred on the main
    // window: newest wins, holds its four seconds, retracts. Errors wear the
    // red. The status bar keeps only the quiet words — hover hints and a wait
    // in progress.
    const notify = (text: string, kind: 'ok' | 'err' = 'ok') => {
        showStatusNote(text, kind, 'window');
    };

    return {statusNote, showStatusNote, hideStatusNote, holdStatusNote, notify};
}

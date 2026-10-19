import * as API from "../../wailsjs/go/main/App";

// A routing change put to work: ApplyRouting saves the profile and, with a node
// running, rebuilds the core against it at once (see the Go side). The method is new,
// and the Wails bindings are only regenerated when the backend itself is rebuilt — a
// dev session that hot-reloaded the frontend onto an older backend does not have it,
// which is what the runtime check below answers: fall back to the plain save, whose
// file is applied at the next start, instead of throwing "not a function" at the
// reader from a toast.
export const applyRoute = (name: string, form: unknown): Promise<void> =>
    typeof (API as {ApplyRouting?: (n: string, r: unknown) => Promise<void>}).ApplyRouting === 'function'
        ? (API as {ApplyRouting: (n: string, r: unknown) => Promise<void>}).ApplyRouting(name, form)
        : API.SaveRouting(name, form as never);

// Wails rejects with the Go error already serialised as "Error: <text>",
// and that prefix is pure noise in a toast. Strip it so the message reads
// as prose rather than as a stack-trace artefact.
export function errText(e: unknown): string {
    const raw = e instanceof Error ? e.message : String(e);
    return raw.replace(/^Error:\s*/, '');
}

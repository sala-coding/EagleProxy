// How traffic leaves, in the order the status bar's list shows it: the routing profile
// decides (the default), everything goes out unproxied, or everything goes through the
// proxy — and in all three the block lists refuse their matches first, which is why
// there is no fourth "block" mode. The strings are what the settings file carries and
// what configbuilder reads — a contract with the config, not labels, which is why they
// are not translated; the words shown beside them are, see egress.* in i18n. A settings
// file still carrying the retired "block" is read as "rule" (see the migration in
// buildRoute).
export const EGRESS_MODES = ['rule', 'direct', 'global'];

// How far the log panel may be dragged. The floor keeps it a panel rather than a
// peephole; the ceiling is a share of the window, because past it the node list
// above — the thing the log is about — is the part that gets squeezed out. Both
// ends are also in .console (min-height, max-height) so a height restored from a
// larger window is capped rather than trusted.
export const CONSOLE_MIN_H = 96;
export const CONSOLE_MAX_SHARE = 0.72;

export const clampConsoleHeight = (h: number, winH: number) =>
    Math.max(CONSOLE_MIN_H, Math.min(Math.round(h), Math.round(winH * CONSOLE_MAX_SHARE)));

// How tall the settings dock may be dragged. The floor keeps its header and the first
// frame of a card standing — below that it is a strip, not a panel; the ceiling is the
// window above the status bar, which is .settings-dock's own max-height as well, so a
// height restored on a shorter window is capped rather than trusted. The 54 is that
// max-height's arithmetic (the bar's 30 and the 24 it keeps clear of the window's top);
// the stylesheet is what holds the two to each other.
export const DOCK_MIN_H = 260;
export const clampDockHeight = (h: number, winH: number) =>
    Math.max(DOCK_MIN_H, Math.min(Math.round(h), Math.round(winH) - 54));

// How wide the subscriptions drawer is, and how wide it may be dragged.
//
// There is no default width any more: with nothing stored it takes a share of the
// window (PANEL_W_SHARE), so a wide screen gets a wider list rather than a column of
// empty space beside a 430px panel. The floor is roughly the narrowest useful shape —
// the name still has room and the figures still fit on one line — and it is what a
// narrow window lands on, because the share is clamped rather than trusted. The ceiling
// is past the point where the node list beside it stops being readable; the window
// itself is the other limit, and .app's min() in App.css applies it, so a width restored
// on a smaller screen is capped rather than trusted either.
export const PANEL_W_SHARE = 0.3;
export const PANEL_W_MIN = 300;
export const PANEL_W_MAX = 720;

export const clampPanelW = (w: number) =>
    Math.max(PANEL_W_MIN, Math.min(PANEL_W_MAX, Math.round(w)));

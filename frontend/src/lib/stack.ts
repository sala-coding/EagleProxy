// The stack the subscriptions drawer puts the node tables on, and the few numbers it takes to
// stand one up. This replaced a ring: the same four tables on a circle going round, driven
// frame by frame (2026-10-10). The stack is those tables standing still, so nearly all of the
// arithmetic that ring needed is gone — nothing here trades against anything else, and a
// table's size is two ceilings and a division.
//
// It stands in the same room the ring did (see the reel in App), which is the space the drawer
// leaves beside the window's right edge: that space is whatever the window and the dragged
// panel leave, so it is measured rather than worked out from the window, and everything below
// is a proportion or a plain subtraction of it.
//
// The hinge is each table's own right border, so the stack is a fan of leaves off one edge:
// every table turns the same way about the same line, and the layer of depth behind it is what
// the perspective draws. Everything about the picture follows from that one decision — which
// border is the axis, and how far the left border goes back behind it.
//
// At the 944x854 a 1440px window leaves beside the drawer, four tables come to about 246x315:
// the tilt takes the cosine off the width (so a table half-turned needs half again the width
// to show the same face), the three steps add their own, and the height is whatever is left
// over under the room's. Unlike the ring, none of that is searched — the depth behind the
// stack is STACK_DEPTH and not a radius to be traded against a card's size, so the width
// ceiling and the height ceiling are the whole of it.
// How many tables deep the stack is. Four is what the reader asked for; fewer subscriptions
// than that and the stack is as deep as the list is long, never padded out with blanks.
export const STACK_N = 4;

// How far each table's face turns away from the reader, in degrees. It is nought: the tables
// stand square to the room, read straight on, and the heap is told apart by its steps and its
// depth alone. The hinge stays where it was — the table's own right border, which is the
// room's right edge — so the turn below is a no-op rather than a removed part, and putting a
// few degrees back into this constant is all it would take to lean them again.
export const STACK_TILT = 0;

// How far each table's hinge edge stands to the left of the one in front of it, in px, and
// how much further back in Z the same step puts it. The first is what lets the table
// underneath show its own right border — that strip is the whole of what the reader gets of
// a subscription that is not the current one — and the reader asked for it to be wide: at
// eighteen the four tables were nearly flush and the heap read as one table with three thin
// edges on it. It then went to thirty and was too far the other way, three deep gutters
// down the right of the room reading as gaps rather than as steps, and twenty-two was still
// reading as gaps. Sixteen is the settle: enough that the staircase is a shape rather than
// a set of hairlines, little enough that the strip each table shows is read as an edge
// rather than as space. It is paid for out of the table's width, since the room is finite
// and the top table has to keep reaching the room's left border; see the width below. The
// second half of the pair is what gives the stack depth: a stack stepped sideways with
// nothing behind it reads as a pile of cut paper, and the perspective would have nothing
// to draw.
export const STACK_STEP = 16;
export const STACK_DEPTH = 34;

// How far off the eye the stack stands, as a share of the room's width. Only the spread
// between the layers matters — STACK_DEPTH, three steps of it end to end — so this is not
// the near eye a spinning ring wanted: close enough that the far edge of a table is visibly
// smaller than its hinge, far enough that the four of them do not step like a card game.
export const STACK_P_SHARE = 1.7;

// A table's proportion, its width over its height. This stood at 0.78 — taller than wide —
// which was the shape of a card going round a ring. The reader then asked for the table to
// reach the room's left border, and that is what the ratio is answering now: the width is
// fixed by the room, so the only thing a ratio has left to decide is how tall the table is,
// and the ratio is set to hold the height at about 435px while the width spends itself on
// the room. That is why it is not a round number: every step taken off the width to widen
// the staircase comes off the height unless the ratio follows it down, and the steps are
// narrow enough now that the width is very nearly the whole room again. Past about 1.7 the
// height ceiling below has the table by then and it only gets shorter.
// A table's proportion, its width over its height. With the height now taken from the room
// (see STACK_H_FILL), what this decides is the width a table of that height would want: the
// room's 706 x 401 gives a table 385 tall, and 385 * this has to reach the 658 the room can
// give it, so anything from 1.71 up leaves the width to the room, as it should be. 1.8 is
// that with a little to spare.
export const STACK_ASPECT = 1.8;

// How much of the room's height a table takes. It is the table's own height now rather than
// a ceiling over it (see the paint effect): the reader asked for the heap to stand just
// inside the rounded frame the table's own box draws, with a small even gap above and below
// it, and a height is the only thing that can say that. Taking the height from the room is
// also what keeps that gap the same at every window size — a height derived from the width
// closes up on a short window and opens out on a tall one.
// Not the whole of it: a table filled to the brim would sit on the frame's own edge, and a
// frame wants to be seen. .96 leaves eight pixels at each end on the usual window — the same
// eight the layer stands off the window's chrome by.
export const STACK_H_FILL = .96;

// What a table is sized against on the frame before the layer has reported its own box:
// first frame of the first opening, and that frame is at the very start of the fade in.
export const STACK_ROOM_FALLBACK = {w: 900, h: 780};

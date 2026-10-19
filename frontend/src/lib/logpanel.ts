// How long a status-bar note stays on screen (the retraction animation then plays
// for a few hundred milliseconds before the element unmounts, and the note waiting
// behind it takes the slot at the end of that — see runStatusNote).
export const NOTE_MS = 4000;
export const NOTE_RETRACT_MS = 400;

// One line's height in the well, for reading a stretch of it as a number of rows rather
// than a number of pixels. The follow measures its slack in the same line.
export const LOG_ROW_PX = 24;
// How close to the end of the log counts as being at it. Not zero: the reader puts the view
// at the end and a line arrives before the scroll event is even handled, so the end has
// already moved by a line by the time the two are compared — with no slack the reader was
// read as having scrolled away and the panel never took the following up again. A line's
// worth, and no more: a reader a line from the end is reading the end.
export const LOG_FOLLOW_SLACK_PX = LOG_ROW_PX;

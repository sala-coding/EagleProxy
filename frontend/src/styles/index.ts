// The stylesheets, in the order they cascade.
//
// App.css used to be one file of nine thousand lines, written in the order the
// window was built: the palette first, then the rail, then the drawer, then the
// table, then the log panel, then the settings form. It is now one file per
// part of that order, and this list is the order itself — the cascade does not
// care which file a rule is in, only where the rule sits, so the sequence
// here is the sequence there.
//
// The compact form (11) overrides the rules before it rather than repeating
// them, and the icon glyphs (12) come last because they are the fallback font
// the rest of the app asks for.
import './01-theme.css';
import './02-rail.css';
import './03-drawer.css';
import './04-subscription-cards.css';
import './05-layout.css';
import './06-node-table.css';
import './07-console.css';
import './08-modal.css';
import './09-settings.css';
import './10-panels.css';
import './11-compact.css';
import './12-icons.css';

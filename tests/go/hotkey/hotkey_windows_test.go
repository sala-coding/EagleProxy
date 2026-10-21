//go:build windows

package hotkey_test

import (
	. "eagle/internal/hotkey"

	"testing"
)

// TestReviewHotkeyNeedsAModifier pins down what a hotkey may be.
//
// A combination without a modifier is registered on its own, and a system-wide
// registration of a bare key takes it away from every other program on the
// machine. The capture field in the settings card does not offer one (the
// F-keys are the exception it allows), so this is only reached by a hand-edited
// settings file or one imported from another program — and refusing it there is
// much better than owning the bare "A" key.
func TestReviewHotkeyNeedsAModifier(t *testing.T) {
	if _, _, err := Parse("A"); err == nil {
		t.Errorf("a bare key was accepted: the app would own the bare %q key system-wide", "A")
	}
	if _, _, err := Parse("F5"); err != nil {
		t.Errorf("the F-keys are the exception the capture field allows, and they were refused: %v", err)
	}
	if _, _, err := Parse("Ctrl+Shift+A"); err != nil {
		t.Errorf("an ordinary combination was refused: %v", err)
	}
}

// TestReviewHotkeyRejectsAnUnknownModifier pins down the other half: a modifier
// the parser does not know used to be dropped without a word, and the key was
// then registered on its own — the same bare-key takeover, reached by a typo
// in a settings file.
func TestReviewHotkeyRejectsAnUnknownModifier(t *testing.T) {
	if _, _, err := Parse("Hyper+A"); err == nil {
		t.Errorf("an unknown modifier was accepted: it used to be dropped silently, and the " +
			"key was then registered on its own")
	}
	if _, _, err := Parse("Ctrl+"); err == nil {
		t.Errorf("a combination with no key was accepted")
	}
}

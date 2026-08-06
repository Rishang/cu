package cli

import (
	"testing"

	"github.com/Rishang/cloudutil/internal/ui"
)

// TestMarkCurrent covers the contract fzf depends on: the active entry is
// styled, everything else is untouched, and the visible text always stays the
// plain name so a selection maps back to it. Colour off falls back to a suffix.
func TestMarkCurrent(t *testing.T) {
	prev := ui.ColorEnabled()
	t.Cleanup(func() { ui.SetColor(prev) })

	ui.SetColor(true)
	if got := markCurrent("staging", "prod-eu"); got != "staging" {
		t.Errorf("markCurrent of a non-current name = %q, want it untouched", got)
	}
	styled := markCurrent("prod-eu", "prod-eu")
	if styled == "prod-eu" {
		t.Error("the current name is unstyled with colour on")
	}
	if plain := ui.StripANSI(styled); plain != "prod-eu" {
		t.Errorf("stripped styling = %q, want prod-eu — fzf would not map it back", plain)
	}

	ui.SetColor(false)
	if got := markCurrent("prod-eu", "prod-eu"); got != "prod-eu (current)" {
		t.Errorf("markCurrent with colour off = %q, want the suffix fallback", got)
	}
}

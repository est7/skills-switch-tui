package tui

import (
	"image/color"
	"math"
	"testing"
)

func TestThemesKeepInformationalTextReadable(t *testing.T) {
	tests := []struct {
		name string
		dark bool
	}{
		{name: "light", dark: false},
		{name: "dark", dark: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			theme := newStyles(tt.dark)
			for _, pair := range themeContrastPairs(theme) {
				ratio := contrastRatio(pair.foreground, pair.background)
				if ratio < 4.5 {
					t.Errorf("%s contrast = %.2f:1, want >= 4.5:1", pair.name, ratio)
				}
			}
			if theme.incompatible.GetFaint() {
				t.Error("incompatible state must not rely on terminal faint rendering")
			}
		})
	}
}

type contrastPair struct {
	name       string
	foreground color.Color
	background color.Color
}

func themeContrastPairs(theme styles) []contrastPair {
	panel := theme.panel.GetBackground()
	raised := theme.tableHeader.GetBackground()
	selected := theme.selected.GetBackground()
	selectedCell := theme.selectedCell.GetBackground()
	return []contrastPair{
		{name: "body", foreground: theme.child.GetForeground(), background: panel},
		{name: "secondary", foreground: theme.subtle.GetForeground(), background: panel},
		{name: "tab", foreground: theme.tab.GetForeground(), background: theme.tab.GetBackground()},
		{name: "active tab", foreground: theme.activeTab.GetForeground(), background: theme.activeTab.GetBackground()},
		{name: "table header", foreground: theme.tableHeader.GetForeground(), background: raised},
		{name: "selected row", foreground: theme.selected.GetForeground(), background: selected},
		{name: "selected cell", foreground: theme.selectedCell.GetForeground(), background: selectedCell},
		{name: "enabled state", foreground: theme.enabled.GetForeground(), background: panel},
		{name: "disabled state", foreground: theme.disabled.GetForeground(), background: panel},
		{name: "issue state", foreground: theme.issue.GetForeground(), background: panel},
		{name: "active filter", foreground: theme.activeFilter.GetForeground(), background: theme.activeFilter.GetBackground()},
		{name: "status", foreground: theme.status.GetForeground(), background: theme.statusBar.GetBackground()},
		{name: "error", foreground: theme.error.GetForeground(), background: theme.statusBar.GetBackground()},
	}
}

func contrastRatio(foreground, background color.Color) float64 {
	lighter := relativeLuminance(foreground)
	darker := relativeLuminance(background)
	if lighter < darker {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05)
}

func relativeLuminance(value color.Color) float64 {
	if value == nil {
		panic("contrast color is nil")
	}
	r, g, b, _ := value.RGBA()
	linearize := func(component uint32) float64 {
		normalized := float64(component) / 65535
		if normalized <= 0.04045 {
			return normalized / 12.92
		}
		return math.Pow((normalized+0.055)/1.055, 2.4)
	}
	return 0.2126*linearize(r) + 0.7152*linearize(g) + 0.0722*linearize(b)
}

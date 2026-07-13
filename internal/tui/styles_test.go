package tui

import (
	"image/color"
	"math"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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

func TestTableStylesNeverFallBackToTransparentBackgrounds(t *testing.T) {
	for _, dark := range []bool{false, true} {
		theme := newStyles(dark)
		if !sameColor(theme.panel.GetBackground(), theme.canvas) {
			t.Fatal("panel background must match the full-window canvas")
		}
		for name, background := range map[string]color.Color{
			"source kind":  theme.scopeLabel.GetBackground(),
			"group":        theme.group.GetBackground(),
			"child":        theme.child.GetBackground(),
			"enabled":      theme.enabled.GetBackground(),
			"disabled":     theme.disabled.GetBackground(),
			"incompatible": theme.incompatible.GetBackground(),
			"issue":        theme.issue.GetBackground(),
		} {
			if background == nil {
				t.Errorf("%s background is transparent", name)
			}
		}
	}
}

func TestDarkThemeUsesSlateGrayInsteadOfBlackCanvas(t *testing.T) {
	theme := newStyles(true)
	if !sameColor(theme.canvas, lipgloss.Color("#3B4449")) {
		t.Fatal("dark canvas must use the slate-gray application background")
	}
}

func TestCanvasBackgroundSurvivesNestedStyleResets(t *testing.T) {
	background := lipgloss.Color("#3B4449")
	backgroundSequence := ansi.Style{}.BackgroundColor(background).String()
	input := "left" + ansi.ResetStyle + " right"
	want := backgroundSequence + "left" + ansi.ResetStyle + backgroundSequence + " right" + ansi.ResetStyle
	if got := paintCanvasBackground(input, background); got != want {
		t.Fatalf("paintCanvasBackground() = %q, want %q", got, want)
	}
}

type contrastPair struct {
	name       string
	foreground color.Color
	background color.Color
}

func themeContrastPairs(theme styles) []contrastPair {
	canvas := theme.canvas
	panel := theme.panel.GetBackground()
	raised := theme.tableHeader.GetBackground()
	selected := theme.selected.GetBackground()
	selectedCell := theme.selectedCell.GetBackground()
	return []contrastPair{
		{name: "canvas title", foreground: theme.title.GetForeground(), background: canvas},
		{name: "canvas secondary", foreground: theme.subtitle.GetForeground(), background: canvas},
		{name: "canvas accent", foreground: theme.scopeLabel.GetForeground(), background: canvas},
		{name: "canvas filter", foreground: theme.filter.GetForeground(), background: canvas},
		{name: "canvas help key", foreground: theme.helpKey.GetForeground(), background: canvas},
		{name: "canvas help description", foreground: theme.helpDesc.GetForeground(), background: canvas},
		{name: "body", foreground: theme.child.GetForeground(), background: panel},
		{name: "secondary", foreground: theme.subtle.GetForeground(), background: panel},
		{name: "tab", foreground: theme.tab.GetForeground(), background: theme.tab.GetBackground()},
		{name: "active tab", foreground: theme.activeTab.GetForeground(), background: theme.activeTab.GetBackground()},
		{name: "table header", foreground: theme.tableHeader.GetForeground(), background: raised},
		{name: "selected row", foreground: theme.selected.GetForeground(), background: selected},
		{name: "selected row accent", foreground: theme.scopeLabel.GetForeground(), background: selected},
		{name: "selected row secondary", foreground: theme.disabled.GetForeground(), background: selected},
		{name: "selected row enabled", foreground: theme.enabled.GetForeground(), background: selected},
		{name: "selected cell", foreground: theme.selectedCell.GetForeground(), background: selectedCell},
		{name: "enabled state", foreground: theme.enabled.GetForeground(), background: panel},
		{name: "disabled state", foreground: theme.disabled.GetForeground(), background: panel},
		{name: "issue state", foreground: theme.issue.GetForeground(), background: panel},
		{name: "active filter", foreground: theme.activeFilter.GetForeground(), background: theme.activeFilter.GetBackground()},
		{name: "status", foreground: theme.status.GetForeground(), background: theme.statusBar.GetBackground()},
		{name: "status accent", foreground: theme.accent.GetForeground(), background: theme.statusBar.GetBackground()},
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

func sameColor(left, right color.Color) bool {
	if left == nil || right == nil {
		return left == right
	}
	lr, lg, lb, la := left.RGBA()
	rr, rg, rb, ra := right.RGBA()
	return lr == rr && lg == rg && lb == rb && la == ra
}

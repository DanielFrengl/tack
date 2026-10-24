// Package style holds the shared look: flat surfaces, hairline rules, color only where it carries meaning.
package style

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/DanielFrengl/tack/internal/meta"
	"github.com/DanielFrengl/tack/internal/store"
)

var (
	Text    = lipgloss.AdaptiveColor{Light: "#1f2328", Dark: "#e6e6e6"}
	Muted   = lipgloss.AdaptiveColor{Light: "#6e7781", Dark: "#8b8f98"}
	Faint   = lipgloss.AdaptiveColor{Light: "#d0d7de", Dark: "#3a3d44"}
	Accent  = lipgloss.AdaptiveColor{Light: "#4f46e5", Dark: "#8b8cf8"}
	Danger  = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f87171"}
	Warning = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#fbbf24"}
	Success = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#4ade80"}

	labelColors = map[string]lipgloss.AdaptiveColor{
		"red":    {Light: "#cf222e", Dark: "#f87171"},
		"orange": {Light: "#bc4c00", Dark: "#fb923c"},
		"yellow": {Light: "#9a6700", Dark: "#facc15"},
		"green":  {Light: "#1a7f37", Dark: "#4ade80"},
		"teal":   {Light: "#0f766e", Dark: "#2dd4bf"},
		"blue":   {Light: "#0969da", Dark: "#60a5fa"},
		"purple": {Light: "#8250df", Dark: "#c084fc"},
		"pink":   {Light: "#bf3989", Dark: "#f472b6"},
		"gray":   {Light: "#6e7781", Dark: "#9ca3af"},
	}

	Bold      = lipgloss.NewStyle().Bold(true).Foreground(Text)
	Dim       = lipgloss.NewStyle().Foreground(Muted)
	Rule      = lipgloss.NewStyle().Foreground(Faint)
	AccentFg  = lipgloss.NewStyle().Foreground(Accent)
	ErrorFg   = lipgloss.NewStyle().Foreground(Danger)
	SuccessFg = lipgloss.NewStyle().Foreground(Success)
)

func LabelColor(name string) lipgloss.AdaptiveColor {
	if c, ok := labelColors[name]; ok {
		return c
	}
	return labelColors["gray"]
}

func LabelTag(l store.Label) string {
	return lipgloss.NewStyle().Foreground(LabelColor(l.Color)).Render("● " + l.Name)
}

func Labels(ls []store.Label) string {
	parts := make([]string, len(ls))
	for i, l := range ls {
		parts[i] = LabelTag(l)
	}
	return strings.Join(parts, " ")
}

func Due(due time.Time, now time.Time) string {
	d := meta.DaysUntil(due, now)
	st := Dim
	switch {
	case d < 0:
		st = lipgloss.NewStyle().Foreground(Danger)
	case d <= 1:
		st = lipgloss.NewStyle().Foreground(Warning)
	}
	return st.Render("◷ " + meta.FormatDue(due, now))
}

func Priority(p int) string {
	switch p {
	case 1:
		return Dim.Render("!")
	case 2:
		return lipgloss.NewStyle().Foreground(Warning).Render("!!")
	case 3:
		return lipgloss.NewStyle().Foreground(Danger).Bold(true).Render("!!!")
	}
	return ""
}

// MetaLine joins the card's priority, due date, checklist progress, notes marker and labels.
func MetaLine(c store.Card, now time.Time) string {
	var parts []string
	if p := Priority(c.Priority); p != "" {
		parts = append(parts, p)
	}
	if c.Due != nil {
		parts = append(parts, Due(*c.Due, now))
	}
	if n := len(c.Checklist); n > 0 {
		done := c.ChecklistDone()
		st := Dim
		if done == n {
			st = SuccessFg
		}
		parts = append(parts, st.Render(fmt.Sprintf("☑ %d/%d", done, n)))
	}
	if strings.TrimSpace(c.Description) != "" {
		parts = append(parts, Dim.Render("≡"))
	}
	if len(c.Labels) > 0 {
		parts = append(parts, Labels(c.Labels))
	}
	return strings.Join(parts, "  ")
}

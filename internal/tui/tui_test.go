package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DanielFrengl/tack/internal/store"
)

func testModel(t *testing.T) *model {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "tack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	b, err := st.CurrentBoard()
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(st, b)
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 30})
	return m
}

// typeKeys sends each key; "enter", "esc", "space" and friends are named keys, anything else is typed as runes.
func typeKeys(m *model, keys ...string) {
	named := map[string]tea.KeyType{
		"enter": tea.KeyEnter, "esc": tea.KeyEsc, "space": tea.KeySpace, "backspace": tea.KeyBackspace,
		"shift+right": tea.KeyShiftRight, "shift+left": tea.KeyShiftLeft,
	}
	for _, k := range keys {
		if kt, ok := named[k]; ok {
			m.Update(tea.KeyMsg{Type: kt})
			continue
		}
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	}
}

func screen(m *model) string { return ansi.Strip(m.View()) }

func dump(t *testing.T, m *model) {
	if os.Getenv("TACK_SCREEN") != "" {
		t.Log("\n" + screen(m))
	}
}

func TestBoardFlow(t *testing.T) {
	m := testModel(t)
	typeKeys(m, "n", "Buy milk", "enter", "Fix login", "enter", "enter")
	if got := len(m.byCol[0]); got != 2 {
		t.Fatalf("want 2 cards in first column, got %d", got)
	}
	// Move "Fix login" right, then mark it done.
	typeKeys(m, "j", "L")
	if m.col != 1 || len(m.byCol[1]) != 1 {
		t.Fatalf("card did not move to Doing: col=%d", m.col)
	}
	typeKeys(m, "p", "p", "p", "t", "tomorrow", "enter", "#", "bug:red, backend", "enter")
	c, _ := m.selected()
	if c.Priority != 3 || c.Due == nil || len(c.Labels) != 2 {
		t.Fatalf("metadata not saved: %+v", c)
	}
	dump(t, m)
	if s := screen(m); !strings.Contains(s, "Fix login") || !strings.Contains(s, "!!!") || !strings.Contains(s, "● bug") {
		t.Fatalf("board doesn't show the card's metadata:\n%s", s)
	}
	typeKeys(m, "space")
	if m.col != 2 {
		t.Fatalf("space should move to the last column, at col %d", m.col)
	}

	// Detail view: checklist and notes.
	typeKeys(m, "enter", "c", "reproduce", "enter", "patch", "enter", "enter", "k", "space", "d", "repro on iOS", "esc")
	if m.mode != modeDetail || len(m.detail.Checklist) != 2 || !m.detail.Checklist[0].Done || m.detail.Description != "repro on iOS" {
		t.Fatalf("detail edits failed: %+v", m.detail)
	}
	dump(t, m)
	typeKeys(m, "esc")

	// Filter, archive, restore.
	typeKeys(m, "/", "#bug", "enter")
	if n := len(m.byCol[0]) + len(m.byCol[1]) + len(m.byCol[2]); n != 1 {
		t.Fatalf("filter should leave 1 card, got %d", n)
	}
	typeKeys(m, "esc", "a")
	if m.total != 1 {
		t.Fatalf("archive should leave 1 active card, got %d", m.total)
	}
	typeKeys(m, "A")
	dump(t, m)
	typeKeys(m, "u", "esc")
	if m.total != 2 {
		t.Fatalf("restore should bring back the card, got %d", m.total)
	}

	// Delete needs confirmation.
	typeKeys(m, "D", "n")
	if m.total != 2 {
		t.Fatal("delete went through without confirmation")
	}
	typeKeys(m, "D", "y")
	if m.total != 1 {
		t.Fatalf("confirmed delete didn't delete, total %d", m.total)
	}
}

func TestBoardsAndColumns(t *testing.T) {
	m := testModel(t)
	typeKeys(m, "C", "Review", "enter", "<")
	if len(m.cols) != 4 || m.cols[2].Name != "Review" || m.col != 2 {
		t.Fatalf("column add/move failed: %+v col=%d", m.cols, m.col)
	}
	typeKeys(m, "b", "n", "Bynaps", "enter", "enter")
	if m.board.Name != "Bynaps" || m.mode != modeBoard {
		t.Fatalf("expected to be on Bynaps, on %q", m.board.Name)
	}
	typeKeys(m, "b")
	dump(t, m)
	typeKeys(m, "D", "y")
	if m.board.Name != "Personal" {
		t.Fatalf("deleting the open board should fall back to Personal, on %q", m.board.Name)
	}
}

func TestManyCardsScroll(t *testing.T) {
	m := testModel(t)
	typeKeys(m, "n")
	for i := 0; i < 30; i++ {
		typeKeys(m, "card", "enter")
	}
	typeKeys(m, "enter", "G")
	s := screen(m)
	dump(t, m)
	if !strings.Contains(s, "↑") || !strings.Contains(s, "▌") {
		t.Fatalf("scrolled column should show the focused card and an ↑ marker:\n%s", s)
	}
	if lines := strings.Count(s, "\n") + 1; lines != 30 {
		t.Fatalf("view should fill the terminal height exactly, got %d lines", lines)
	}
}

// Package tui is the interactive kanban board.
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DanielFrengl/tack/internal/meta"
	"github.com/DanielFrengl/tack/internal/store"
	"github.com/DanielFrengl/tack/internal/style"
)

type mode int

const (
	modeBoard mode = iota
	modeDetail
	modeBoards
	modeArchive
	modeHelp
)

type promptKind int

const (
	promptNone promptKind = iota
	promptNewCard
	promptEditTitle
	promptDue
	promptLabels
	promptNewColumn
	promptRenameColumn
	promptNewBoard
	promptRenameBoard
	promptNewCheck
	promptEditCheck
	promptSearch
)

type confirmation struct {
	msg string
	run func() error
}

type boardRow struct {
	board store.Board
	cards int
}

type model struct {
	st    *store.Store
	board store.Board

	cols   []store.Column
	byCol  [][]store.Card // active cards per column, after the filter
	total  int            // active cards on the board, ignoring the filter
	labels []store.Label

	col, row int // focused column and card
	colOff   int // first visible column
	filter   string

	mode     mode
	prevMode mode // where help returns to

	prompt     promptKind
	input      textinput.Model
	target     int64  // card the prompt edits
	targetItem int64  // checklist item the prompt edits
	oldFilter  string // restored when a search is cancelled

	confirm *confirmation

	ta          textarea.Model
	editingDesc bool

	detail   store.Card
	checkIdx int

	boards   []boardRow
	boardIdx int

	archived []store.Card
	archIdx  int

	status    string
	statusErr bool

	w, h int
}

// Run opens the board full-screen and returns when the user quits.
func Run(st *store.Store, b store.Board) error {
	_, err := tea.NewProgram(newModel(st, b), tea.WithAltScreen()).Run()
	return err
}

func newModel(st *store.Store, b store.Board) *model {
	m := &model{st: st, board: b, w: 100, h: 30}
	m.input = textinput.New()
	m.input.PromptStyle = style.AccentFg
	m.input.Cursor.Style = style.AccentFg
	m.ta = textarea.New()
	m.ta.ShowLineNumbers = false
	m.ta.Prompt = "  "
	m.ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	m.ta.FocusedStyle.Base = lipgloss.NewStyle()
	m.ta.BlurredStyle.Base = lipgloss.NewStyle()
	m.ta.CharLimit = 0
	m.ta.Placeholder = "Write notes…"
	m.reload(0)
	return m
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) fail(err error) bool {
	if err != nil {
		m.status, m.statusErr = err.Error(), true
		return true
	}
	return false
}

func (m *model) info(format string, a ...any) {
	m.status, m.statusErr = fmt.Sprintf(format, a...), false
}

// reload reads the board from the database; a non-zero keepID refocuses that card.
func (m *model) reload(keepID int64) {
	var err error
	if m.cols, err = m.st.Columns(m.board.ID); m.fail(err) {
		return
	}
	cards, err := m.st.Cards(m.board.ID, false)
	if m.fail(err) {
		return
	}
	if m.labels, err = m.st.Labels(m.board.ID); m.fail(err) {
		return
	}
	m.total = len(cards)
	idx := map[int64]int{}
	for i, c := range m.cols {
		idx[c.ID] = i
	}
	m.byCol = make([][]store.Card, len(m.cols))
	for _, c := range cards {
		if i, ok := idx[c.ColumnID]; ok && meta.Matches(c, m.filter) {
			m.byCol[i] = append(m.byCol[i], c)
		}
	}
	if keepID != 0 {
		for ci, list := range m.byCol {
			for ri, c := range list {
				if c.ID == keepID {
					m.col, m.row = ci, ri
				}
			}
		}
	}
	m.clamp()
}

func (m *model) clamp() {
	m.col = clampInt(m.col, 0, len(m.cols)-1)
	if m.col < len(m.byCol) {
		m.row = clampInt(m.row, 0, len(m.byCol[m.col])-1)
	} else {
		m.row = 0
	}
	n := m.visibleCols()
	if m.col < m.colOff {
		m.colOff = m.col
	}
	if m.col >= m.colOff+n {
		m.colOff = m.col - n + 1
	}
	m.colOff = clampInt(m.colOff, 0, len(m.cols)-n)
}

func (m *model) visibleCols() int {
	n := m.w / minColWidth
	if n < 1 {
		n = 1
	}
	if n > len(m.cols) {
		n = len(m.cols)
	}
	return n
}

func (m *model) selected() (store.Card, bool) {
	if m.col >= len(m.byCol) || m.row >= len(m.byCol[m.col]) || len(m.byCol[m.col]) == 0 {
		return store.Card{}, false
	}
	return m.byCol[m.col][m.row], true
}

// current is the card being acted on: the open card in detail view, else the focused one.
func (m *model) current() (store.Card, bool) {
	if m.mode == modeDetail {
		return m.detail, true
	}
	return m.selected()
}

func (m *model) refreshDetail() {
	c, err := m.st.Card(m.detail.ID)
	if m.fail(err) {
		m.mode = modeBoard
		return
	}
	m.detail = c
	m.checkIdx = clampInt(m.checkIdx, 0, len(c.Checklist)-1)
}

// afterCardChange re-reads whatever view is showing the card.
func (m *model) afterCardChange(id int64) {
	m.reload(id)
	if m.mode == modeDetail {
		m.refreshDetail()
	}
}

func (m *model) openPrompt(kind promptKind, label, value string) tea.Cmd {
	m.prompt = kind
	m.input.Prompt = label + " › "
	m.input.Placeholder = ""
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.input.Width = m.w - lipgloss.Width(m.input.Prompt) - 2
	return m.input.Focus()
}

func (m *model) closePrompt() {
	m.prompt = promptNone
	m.input.Blur()
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.ta.SetWidth(min(m.w, detailWidth) - 4)
		m.ta.SetHeight(max(3, min(12, m.h/3)))
		m.clamp()
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		m.status = ""
		switch {
		case m.confirm != nil:
			return m, m.updateConfirm(msg)
		case m.prompt != promptNone:
			return m, m.updatePrompt(msg)
		case m.editingDesc:
			return m, m.updateDesc(msg)
		}
		switch m.mode {
		case modeBoard:
			return m, m.updateBoard(msg)
		case modeDetail:
			return m, m.updateDetail(msg)
		case modeBoards:
			return m, m.updateBoards(msg)
		case modeArchive:
			return m, m.updateArchive(msg)
		case modeHelp:
			m.mode = m.prevMode
			return m, nil
		}
	}
	if m.prompt != promptNone {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	if m.editingDesc {
		var cmd tea.Cmd
		m.ta, cmd = m.ta.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) updateConfirm(msg tea.KeyMsg) tea.Cmd {
	c := m.confirm
	m.confirm = nil
	if k := msg.String(); k == "y" || k == "Y" {
		m.fail(c.run())
	} else {
		m.info("cancelled")
	}
	return nil
}

func (m *model) updateDesc(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "ctrl+s":
		m.editingDesc = false
		m.ta.Blur()
		c := m.detail
		c.Description = strings.TrimRight(m.ta.Value(), "\n ")
		if !m.fail(m.st.UpdateCard(c)) {
			m.info("notes saved")
		}
		m.afterCardChange(c.ID)
		return nil
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return cmd
}

func (m *model) updatePrompt(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		if m.prompt == promptSearch {
			m.filter = m.oldFilter
			m.reload(0)
		}
		m.closePrompt()
		return nil
	case "enter":
		return m.submitPrompt(strings.TrimSpace(m.input.Value()))
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.prompt == promptSearch {
		id := int64(0)
		if c, ok := m.selected(); ok {
			id = c.ID
		}
		m.filter = m.input.Value()
		m.reload(id)
	}
	return cmd
}

func (m *model) submitPrompt(v string) tea.Cmd {
	kind := m.prompt
	m.closePrompt()
	switch kind {
	case promptNewCard:
		if v == "" {
			return nil
		}
		if len(m.cols) == 0 {
			m.fail(fmt.Errorf("add a column first (C)"))
			return nil
		}
		c, err := m.st.CreateCard(m.board.ID, m.cols[m.col].ID, v)
		if m.fail(err) {
			return nil
		}
		m.reload(c.ID)
		if m.filter != "" && !meta.Matches(c, m.filter) {
			m.info("added #%d (hidden by the filter)", c.ID)
		}
		// Stay in the prompt so several cards can be typed in a row.
		return m.openPrompt(promptNewCard, "new card in "+m.cols[m.col].Name, "")

	case promptEditTitle:
		c, err := m.st.Card(m.target)
		if m.fail(err) || v == "" {
			return nil
		}
		c.Title = v
		m.fail(m.st.UpdateCard(c))
		m.afterCardChange(c.ID)

	case promptDue:
		c, err := m.st.Card(m.target)
		if m.fail(err) {
			return nil
		}
		if c.Due, err = meta.ParseDue(v, time.Now()); m.fail(err) {
			return nil
		}
		m.fail(m.st.UpdateCard(c))
		m.afterCardChange(c.ID)

	case promptLabels:
		var ids []int64
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "#"))
			if part == "" {
				continue
			}
			name, color, _ := strings.Cut(part, ":")
			l, err := m.st.EnsureLabel(m.board.ID, name, strings.ToLower(strings.TrimSpace(color)))
			if m.fail(err) {
				return nil
			}
			ids = append(ids, l.ID)
		}
		m.fail(m.st.SetCardLabels(m.target, ids))
		m.afterCardChange(m.target)

	case promptNewColumn:
		if v == "" {
			return nil
		}
		c, err := m.st.CreateColumn(m.board.ID, v)
		if m.fail(err) {
			return nil
		}
		m.reload(0)
		for i, col := range m.cols {
			if col.ID == c.ID {
				m.col, m.row = i, 0
			}
		}
		m.clamp()

	case promptRenameColumn:
		if v != "" && len(m.cols) > 0 {
			m.fail(m.st.RenameColumn(m.cols[m.col].ID, v))
			m.reload(0)
		}

	case promptNewBoard:
		if v == "" {
			return nil
		}
		b, err := m.st.CreateBoard(v, store.DefaultColumns)
		if m.fail(err) {
			return nil
		}
		m.loadBoards()
		for i, r := range m.boards {
			if r.board.ID == b.ID {
				m.boardIdx = i
			}
		}

	case promptRenameBoard:
		if v != "" && m.boardIdx < len(m.boards) {
			id := m.boards[m.boardIdx].board.ID
			if !m.fail(m.st.RenameBoard(id, v)) && id == m.board.ID {
				m.board.Name = v
			}
			m.loadBoards()
		}

	case promptNewCheck:
		if v == "" {
			return nil
		}
		if _, err := m.st.AddChecklistItem(m.detail.ID, v); m.fail(err) {
			return nil
		}
		m.afterCardChange(m.detail.ID)
		m.checkIdx = len(m.detail.Checklist) - 1
		return m.openPrompt(promptNewCheck, "checklist item", "")

	case promptEditCheck:
		if v != "" {
			m.fail(m.st.RenameChecklistItem(m.targetItem, v))
			m.afterCardChange(m.detail.ID)
		}

	case promptSearch:
		m.filter = v
		m.reload(0)
	}
	return nil
}

// ---- board ----

func (m *model) updateBoard(msg tea.KeyMsg) tea.Cmd {
	card, hasCard := m.selected()
	switch msg.String() {
	case "q":
		return tea.Quit
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.reload(card.ID)
		}
	case "?":
		m.prevMode, m.mode = m.mode, modeHelp
	case "h", "left":
		m.col--
		m.clamp()
	case "l", "right":
		m.col++
		m.clamp()
	case "k", "up":
		m.row--
		m.clamp()
	case "j", "down":
		m.row++
		m.clamp()
	case "g", "home":
		m.row = 0
	case "G", "end":
		if m.col < len(m.byCol) {
			m.row = len(m.byCol[m.col]) - 1
		}
		m.clamp()
	case "0":
		m.col = 0
		m.clamp()
	case "$":
		m.col = len(m.cols) - 1
		m.clamp()

	case "H", "shift+left":
		if hasCard {
			m.moveCardToColumn(card, m.col-1)
		}
	case "L", "shift+right":
		if hasCard {
			m.moveCardToColumn(card, m.col+1)
		}
	case "K", "shift+up":
		if hasCard {
			m.reorder(card, -1)
		}
	case "J", "shift+down":
		if hasCard {
			m.reorder(card, 1)
		}
	case " ":
		if hasCard {
			m.moveCardToColumn(card, len(m.cols)-1)
		}

	case "n", "o":
		if len(m.cols) == 0 {
			m.fail(fmt.Errorf("add a column first (C)"))
			return nil
		}
		return m.openPrompt(promptNewCard, "new card in "+m.cols[m.col].Name, "")
	case "enter":
		if hasCard {
			m.detail, m.checkIdx, m.mode = card, 0, modeDetail
		}
	case "/":
		m.oldFilter = m.filter
		cmd := m.openPrompt(promptSearch, "filter", m.filter)
		m.input.Placeholder = "words, #label, !high"
		return cmd

	case "C":
		return m.openPrompt(promptNewColumn, "new column", "")
	case "R":
		if len(m.cols) > 0 {
			return m.openPrompt(promptRenameColumn, "rename column", m.cols[m.col].Name)
		}
	case "X":
		if len(m.cols) > 0 {
			col := m.cols[m.col]
			n, _ := m.st.CountCards(col.ID)
			m.confirm = &confirmation{
				msg: fmt.Sprintf("delete column %q and its %d cards?", col.Name, n),
				run: func() error {
					if err := m.st.DeleteColumn(col.ID); err != nil {
						return err
					}
					m.reload(0)
					m.info("deleted column %s", col.Name)
					return nil
				},
			}
		}
	case "<", ",":
		if len(m.cols) > 0 {
			m.moveColumn(-1)
		}
	case ">", ".":
		if len(m.cols) > 0 {
			m.moveColumn(1)
		}

	case "b", "B":
		m.loadBoards()
		m.mode = modeBoards
	case "A":
		m.loadArchive()
		m.mode = modeArchive

	default:
		if hasCard {
			return m.cardAction(msg.String(), card)
		}
	}
	return nil
}

// cardAction handles keys shared by the board and the card detail view.
func (m *model) cardAction(key string, card store.Card) tea.Cmd {
	switch key {
	case "e":
		m.target = card.ID
		return m.openPrompt(promptEditTitle, "title", card.Title)
	case "p":
		card.Priority = (card.Priority + 1) % 4
		if !m.fail(m.st.UpdateCard(card)) {
			m.info("priority: %s", meta.PriorityNames[card.Priority])
		}
		m.afterCardChange(card.ID)
	case "t", "@":
		m.target = card.ID
		v := ""
		if card.Due != nil {
			v = card.Due.Format("2006-01-02")
		}
		cmd := m.openPrompt(promptDue, "due", v)
		m.input.Placeholder = "2026-11-03, 3.11., today, tomorrow, fri, +3d, none"
		return cmd
	case "#":
		m.target = card.ID
		names := make([]string, len(card.Labels))
		for i, l := range card.Labels {
			names[i] = l.Name
		}
		v := strings.Join(names, ", ")
		if v != "" {
			v += ", "
		}
		cmd := m.openPrompt(promptLabels, "labels", v)
		m.input.Placeholder = "bug, design:blue …"
		if len(m.labels) > 0 {
			all := make([]string, len(m.labels))
			for i, l := range m.labels {
				all[i] = l.Name
			}
			m.info("labels on this board: %s  ·  name:color sets a color (%s)", strings.Join(all, ", "), strings.Join(store.LabelColors, " "))
		} else {
			m.info("comma-separated; name:color sets a color (%s)", strings.Join(store.LabelColors, " "))
		}
		return cmd
	case "a":
		if !m.fail(m.st.SetArchived(card.ID, true)) {
			m.info("archived #%d · A shows the archive", card.ID)
		}
		if m.mode == modeDetail {
			m.mode = modeBoard
		}
		m.reload(0)
	case "D", "delete":
		m.confirm = &confirmation{
			msg: fmt.Sprintf("delete #%d %q for good?", card.ID, card.Title),
			run: func() error {
				if err := m.st.DeleteCard(card.ID); err != nil {
					return err
				}
				if m.mode == modeDetail {
					m.mode = modeBoard
				}
				m.reload(0)
				m.info("deleted #%d", card.ID)
				return nil
			},
		}
	}
	return nil
}

func (m *model) moveCardToColumn(card store.Card, to int) {
	if to < 0 || to >= len(m.cols) || m.cols[to].ID == card.ColumnID {
		return
	}
	idx := -1
	if m.filter == "" {
		idx = min(m.row, len(m.byCol[to]))
	}
	if m.fail(m.st.MoveCard(card.ID, m.cols[to].ID, idx)) {
		return
	}
	if to == len(m.cols)-1 && m.mode == modeBoard {
		m.info("#%d → %s", card.ID, m.cols[to].Name)
	}
	m.afterCardChange(card.ID)
}

func (m *model) reorder(card store.Card, delta int) {
	if m.filter != "" {
		m.fail(fmt.Errorf("clear the filter (esc) to reorder"))
		return
	}
	to := m.row + delta
	if to < 0 || to >= len(m.byCol[m.col]) {
		return
	}
	if !m.fail(m.st.MoveCard(card.ID, card.ColumnID, to)) {
		m.reload(card.ID)
	}
}

func (m *model) moveColumn(delta int) {
	id := m.cols[m.col].ID
	if m.fail(m.st.MoveColumn(id, delta)) {
		return
	}
	m.reload(0)
	for i, c := range m.cols {
		if c.ID == id {
			m.col = i
		}
	}
	m.clamp()
}

// ---- detail ----

func (m *model) updateDetail(msg tea.KeyMsg) tea.Cmd {
	c := m.detail
	switch msg.String() {
	case "esc", "q", "enter":
		m.mode = modeBoard
		m.reload(c.ID)
	case "?":
		m.prevMode, m.mode = m.mode, modeHelp
	case "j", "down":
		m.checkIdx = clampInt(m.checkIdx+1, 0, len(c.Checklist)-1)
	case "k", "up":
		m.checkIdx = clampInt(m.checkIdx-1, 0, len(c.Checklist)-1)
	case " ", "x":
		if m.checkIdx < len(c.Checklist) {
			m.fail(m.st.ToggleChecklistItem(c.Checklist[m.checkIdx].ID))
			m.afterCardChange(c.ID)
		}
	case "c", "i":
		return m.openPrompt(promptNewCheck, "checklist item", "")
	case "r":
		if m.checkIdx < len(c.Checklist) {
			it := c.Checklist[m.checkIdx]
			m.targetItem = it.ID
			return m.openPrompt(promptEditCheck, "checklist item", it.Text)
		}
	case "X":
		if m.checkIdx < len(c.Checklist) {
			m.fail(m.st.DeleteChecklistItem(c.Checklist[m.checkIdx].ID))
			m.afterCardChange(c.ID)
		}
	case "d", "n":
		m.editingDesc = true
		m.ta.SetValue(c.Description)
		return m.ta.Focus()
	case "H", "shift+left", "L", "shift+right":
		from := -1
		for i, col := range m.cols {
			if col.ID == c.ColumnID {
				from = i
			}
		}
		to := from + 1
		if k := msg.String(); k == "H" || k == "shift+left" {
			to = from - 1
		}
		m.moveCardToColumn(c, to)
	default:
		return m.cardAction(msg.String(), c)
	}
	return nil
}

// ---- boards ----

func (m *model) loadBoards() {
	boards, err := m.st.Boards()
	if m.fail(err) {
		return
	}
	m.boards = m.boards[:0]
	for i, b := range boards {
		cards, _ := m.st.Cards(b.ID, false)
		m.boards = append(m.boards, boardRow{board: b, cards: len(cards)})
		if b.ID == m.board.ID && m.mode != modeBoards {
			m.boardIdx = i
		}
	}
	m.boardIdx = clampInt(m.boardIdx, 0, len(m.boards)-1)
}

func (m *model) switchBoard(b store.Board) {
	m.board = b
	m.fail(m.st.SetCurrentBoard(b.ID))
	m.col, m.row, m.colOff, m.filter = 0, 0, 0, ""
	m.reload(0)
}

func (m *model) updateBoards(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q", "b":
		m.mode = modeBoard
	case "?":
		m.prevMode, m.mode = m.mode, modeHelp
	case "j", "down":
		m.boardIdx = clampInt(m.boardIdx+1, 0, len(m.boards)-1)
	case "k", "up":
		m.boardIdx = clampInt(m.boardIdx-1, 0, len(m.boards)-1)
	case "enter", "l", "right":
		if m.boardIdx < len(m.boards) {
			m.switchBoard(m.boards[m.boardIdx].board)
			m.mode = modeBoard
		}
	case "n", "o":
		return m.openPrompt(promptNewBoard, "new board", "")
	case "r", "e":
		if m.boardIdx < len(m.boards) {
			return m.openPrompt(promptRenameBoard, "rename board", m.boards[m.boardIdx].board.Name)
		}
	case "D", "delete":
		if m.boardIdx >= len(m.boards) {
			return nil
		}
		if len(m.boards) == 1 {
			m.fail(fmt.Errorf("can't delete the only board"))
			return nil
		}
		row := m.boards[m.boardIdx]
		m.confirm = &confirmation{
			msg: fmt.Sprintf("delete board %q and its %d cards?", row.board.Name, row.cards),
			run: func() error {
				if err := m.st.DeleteBoard(row.board.ID); err != nil {
					return err
				}
				if row.board.ID == m.board.ID {
					b, err := m.st.CurrentBoard()
					if err != nil {
						return err
					}
					m.switchBoard(b)
				}
				m.loadBoards()
				m.info("deleted board %s", row.board.Name)
				return nil
			},
		}
	}
	return nil
}

// ---- archive ----

func (m *model) loadArchive() {
	var err error
	m.archived, err = m.st.Cards(m.board.ID, true)
	m.fail(err)
	m.archIdx = clampInt(m.archIdx, 0, len(m.archived)-1)
}

func (m *model) updateArchive(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q", "A":
		m.mode = modeBoard
		m.reload(0)
	case "?":
		m.prevMode, m.mode = m.mode, modeHelp
	case "j", "down":
		m.archIdx = clampInt(m.archIdx+1, 0, len(m.archived)-1)
	case "k", "up":
		m.archIdx = clampInt(m.archIdx-1, 0, len(m.archived)-1)
	case "u", "enter":
		if m.archIdx < len(m.archived) {
			c := m.archived[m.archIdx]
			if !m.fail(m.st.SetArchived(c.ID, false)) {
				m.info("restored #%d", c.ID)
			}
			m.loadArchive()
		}
	case "D", "delete":
		if m.archIdx < len(m.archived) {
			c := m.archived[m.archIdx]
			m.confirm = &confirmation{
				msg: fmt.Sprintf("delete #%d %q for good?", c.ID, c.Title),
				run: func() error {
					if err := m.st.DeleteCard(c.ID); err != nil {
						return err
					}
					m.loadArchive()
					m.info("deleted #%d", c.ID)
					return nil
				},
			}
		}
	}
	return nil
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DanielFrengl/tack/internal/meta"
	"github.com/DanielFrengl/tack/internal/store"
	"github.com/DanielFrengl/tack/internal/style"
)

const (
	minColWidth = 30
	detailWidth = 80
)

func (m *model) View() string {
	var body string
	switch m.mode {
	case modeDetail:
		body = m.viewDetail()
	case modeBoards:
		body = m.viewBoards()
	case modeArchive:
		body = m.viewArchive()
	case modeHelp:
		body = m.viewHelp()
	default:
		body = m.viewBoard()
	}
	bodyH := m.h - 3
	lines := strings.Split(body, "\n")
	if len(lines) > bodyH {
		lines = lines[:max(bodyH, 0)]
	}
	for len(lines) < bodyH {
		lines = append(lines, "")
	}
	return m.viewHeader() + "\n" + m.rule(style.Rule) + "\n" + strings.Join(lines, "\n") + "\n" + m.viewFooter()
}

func (m *model) rule(st lipgloss.Style) string {
	return st.Render(strings.Repeat("─", max(m.w, 0)))
}

func (m *model) viewHeader() string {
	left := style.AccentFg.Bold(true).Render(" tack") + style.Dim.Render("  ·  ") + style.Bold.Render(m.board.Name)
	switch m.mode {
	case modeBoards:
		left += style.Dim.Render("  ·  boards")
	case modeArchive:
		left += style.Dim.Render("  ·  archive")
	case modeHelp:
		left += style.Dim.Render("  ·  keys")
	default:
		left += style.Dim.Render(fmt.Sprintf("  ·  %d cards", m.total))
		if m.filter != "" {
			shown := 0
			for _, l := range m.byCol {
				shown += len(l)
			}
			left += style.AccentFg.Render(fmt.Sprintf("  ·  filter %q", m.filter)) + style.Dim.Render(fmt.Sprintf(" %d shown", shown))
		}
		if n := m.visibleCols(); n < len(m.cols) {
			left += style.Dim.Render(fmt.Sprintf("  ·  columns %d–%d of %d", m.colOff+1, m.colOff+n, len(m.cols)))
		}
	}
	right := style.Dim.Render("? keys  q quit ")
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return ansi.Truncate(left, m.w, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *model) viewFooter() string {
	switch {
	case m.confirm != nil:
		return style.ErrorFg.Render(" "+m.confirm.msg) + style.Dim.Render("  y / n")
	case m.prompt != promptNone:
		return " " + m.input.View()
	case m.status != "":
		st := style.Dim
		if m.statusErr {
			st = style.ErrorFg
		}
		return ansi.Truncate(st.Render(" "+m.status), m.w, "…")
	}
	var hints string
	switch {
	case m.editingDesc:
		hints = "esc save notes"
	case m.mode == modeDetail:
		hints = "e title  d notes  c add item  space tick  r rename item  X remove item  p priority  t due  # labels  H/L move  a archive  esc back"
	case m.mode == modeBoards:
		hints = "enter open  n new  r rename  D delete  esc back"
	case m.mode == modeArchive:
		hints = "u restore  D delete  esc back"
	case m.mode == modeHelp:
		hints = "any key to close"
	default:
		hints = "n new  enter open  H/L move  space done  e edit  p priority  t due  # labels  / filter  b boards  ? more"
	}
	return ansi.Truncate(style.Dim.Render(" "+hints), m.w, "…")
}

// ---- board ----

func (m *model) viewBoard() string {
	if len(m.cols) == 0 {
		return "\n" + style.Dim.Render("  This board has no columns. Press C to add one.")
	}
	n := m.visibleCols()
	sep := 3 // " │ "
	colW := (m.w - 1 - (n-1)*sep) / n
	h := m.h - 3
	blocks := make([][]string, 0, n)
	for i := m.colOff; i < m.colOff+n && i < len(m.cols); i++ {
		blocks = append(blocks, m.viewColumn(i, colW, h))
	}
	var out strings.Builder
	bar := style.Rule.Render(" │ ")
	for line := 0; line < h; line++ {
		out.WriteString(" ")
		for bi, b := range blocks {
			if bi > 0 {
				out.WriteString(bar)
			}
			out.WriteString(pad(b[line], colW))
		}
		if line < h-1 {
			out.WriteString("\n")
		}
	}
	return out.String()
}

func (m *model) viewColumn(i, w, h int) []string {
	focused := i == m.col
	col := m.cols[i]
	cards := m.byCol[i]
	lines := make([]string, 0, h)

	name := style.Bold.Render(col.Name)
	rule := style.Rule
	if focused {
		name = style.AccentFg.Bold(true).Render(col.Name)
		rule = style.AccentFg
	}
	lines = append(lines, ansi.Truncate(name+style.Dim.Render(fmt.Sprintf("  %d", len(cards))), w, "…"))
	lines = append(lines, rule.Render(strings.Repeat("─", w)))

	now := time.Now()
	rendered := make([][]string, len(cards))
	for ci, c := range cards {
		rendered[ci] = renderCard(c, w, focused && ci == m.row, now)
	}
	avail := h - len(lines)
	if len(cards) == 0 {
		hint := "no cards"
		if focused {
			hint = "no cards · n to add one"
		}
		lines = append(lines, "", style.Dim.Render(hint))
		for len(lines) < h {
			lines = append(lines, "")
		}
		return lines[:h]
	}

	// Scroll just far enough to keep the focused card in view.
	sel := 0
	if focused {
		sel = m.row
	}
	height := func(from, to int) int {
		n := 0
		for k := from; k <= to; k++ {
			n += len(rendered[k]) + 1
		}
		if from > 0 {
			n++
		}
		if to < len(cards)-1 {
			n++
		}
		return n
	}
	start := 0
	for start < sel && height(start, sel) > avail {
		start++
	}
	if start > 0 {
		lines = append(lines, style.Dim.Render(fmt.Sprintf("↑ %d more", start)))
	} else {
		lines = append(lines, "")
	}
	for k := start; k < len(cards); k++ {
		need := len(rendered[k]) + 1
		left := h - len(lines)
		if k < len(cards)-1 {
			left-- // keep a line for "↓ n more"
		}
		if need > left {
			lines = append(lines, style.Dim.Render(fmt.Sprintf("↓ %d more", len(cards)-k)))
			break
		}
		lines = append(lines, rendered[k]...)
		lines = append(lines, "")
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}

func renderCard(c store.Card, w int, selected bool, now time.Time) []string {
	inner := w - 2
	marker := "  "
	title := lipgloss.NewStyle().Foreground(style.Text)
	if selected {
		marker = style.AccentFg.Render("▌ ")
		title = title.Bold(true)
	}
	var out []string
	for _, l := range wrap(c.Title, inner) {
		out = append(out, marker+title.Render(l))
	}
	if m := style.MetaLine(c, now); m != "" {
		dim := "  "
		if selected {
			dim = marker
		}
		out = append(out, dim+ansi.Truncate(m, inner, "…"))
	}
	return out
}

// ---- detail ----

func (m *model) viewDetail() string {
	c := m.detail
	w := min(m.w-4, detailWidth)
	now := time.Now()
	var b strings.Builder
	line := func(s string) { b.WriteString("  " + s + "\n") }

	colName := ""
	for _, col := range m.cols {
		if col.ID == c.ColumnID {
			colName = col.Name
		}
	}
	line("")
	line(style.Dim.Render(fmt.Sprintf("#%d · %s › %s", c.ID, m.board.Name, colName)))
	for _, l := range wrap(c.Title, w) {
		line(style.Bold.Render(l))
	}
	line(style.Rule.Render(strings.Repeat("─", w)))

	field := func(k, v string) { line(style.Dim.Render(fmt.Sprintf("%-10s", k)) + v) }
	none := style.Dim.Render("—")
	if c.Priority > 0 {
		field("priority", meta.PriorityNames[c.Priority]+"  "+style.Priority(c.Priority))
	} else {
		field("priority", none)
	}
	if c.Due != nil {
		field("due", c.Due.Format("Mon Jan 2 2006")+"  "+style.Due(*c.Due, now))
	} else {
		field("due", none)
	}
	if len(c.Labels) > 0 {
		field("labels", style.Labels(c.Labels))
	} else {
		field("labels", none)
	}
	field("created", style.Dim.Render(c.CreatedAt.Format("Jan 2 2006 15:04")))

	line("")
	line(style.Dim.Render("notes"))
	if m.editingDesc {
		for _, l := range strings.Split(m.ta.View(), "\n") {
			b.WriteString(l + "\n")
		}
	} else if strings.TrimSpace(c.Description) == "" {
		line(style.Dim.Render("nothing yet · d to write"))
	} else {
		for _, para := range strings.Split(c.Description, "\n") {
			for _, l := range wrap(para, w) {
				line(l)
			}
		}
	}

	line("")
	if len(c.Checklist) == 0 {
		line(style.Dim.Render("checklist"))
		line(style.Dim.Render("empty · c to add an item"))
	} else {
		done := c.ChecklistDone()
		barW := 20
		fill := barW * done / len(c.Checklist)
		bar := style.SuccessFg.Render(strings.Repeat("━", fill)) + style.Rule.Render(strings.Repeat("━", barW-fill))
		line(style.Dim.Render(fmt.Sprintf("checklist %d/%d  ", done, len(c.Checklist))) + bar)
		for i, it := range c.Checklist {
			cur := "  "
			if i == m.checkIdx && !m.editingDesc {
				cur = style.AccentFg.Render("▌ ")
			}
			box, text := "[ ]", it.Text
			if it.Done {
				box, text = style.SuccessFg.Render("[x]"), style.Dim.Strikethrough(true).Render(text)
			}
			line(cur + box + " " + ansi.Truncate(text, w-6, "…"))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// ---- boards & archive ----

func (m *model) viewBoards() string {
	var b strings.Builder
	b.WriteString("\n")
	for i, r := range m.boards {
		cur, name := "  ", r.board.Name
		if i == m.boardIdx {
			cur, name = style.AccentFg.Render("▌ "), style.Bold.Render(name)
		}
		suffix := style.Dim.Render(fmt.Sprintf("  %d cards", r.cards))
		if r.board.ID == m.board.ID {
			suffix += style.AccentFg.Render("  · open")
		}
		b.WriteString("  " + cur + name + suffix + "\n")
	}
	return b.String()
}

func (m *model) viewArchive() string {
	var b strings.Builder
	b.WriteString("\n")
	if len(m.archived) == 0 {
		b.WriteString(style.Dim.Render("  Nothing archived on this board. Press a on a card to archive it.") + "\n")
		return b.String()
	}
	colNames := map[int64]string{}
	for _, c := range m.cols {
		colNames[c.ID] = c.Name
	}
	now := time.Now()
	for i, c := range m.archived {
		cur := "  "
		title := c.Title
		if i == m.archIdx {
			cur, title = style.AccentFg.Render("▌ "), style.Bold.Render(title)
		}
		s := cur + style.Dim.Render(fmt.Sprintf("#%-4d ", c.ID)) + title + style.Dim.Render("  "+colNames[c.ColumnID])
		if meta := style.MetaLine(c, now); meta != "" {
			s += "  " + meta
		}
		b.WriteString("  " + ansi.Truncate(s, m.w-2, "…") + "\n")
	}
	return b.String()
}

var helpSections = []struct {
	title string
	keys  [][2]string
}{
	{"Move around", [][2]string{
		{"h j k l / arrows", "focus column / card"},
		{"g  G", "first / last card"},
		{"0  $", "first / last column"},
		{"enter", "open card"},
		{"b", "boards"},
		{"A", "archive"},
	}},
	{"Cards", [][2]string{
		{"n", "new card (keeps prompting, empty enter stops)"},
		{"H  L  /  shift+←→", "move card to the column left / right"},
		{"J  K  /  shift+↑↓", "move card down / up"},
		{"space", "move card to the last column (done)"},
		{"e", "edit title"},
		{"p", "cycle priority: none → low → medium → high"},
		{"t", "due date: 2026-11-03, 3.11., tomorrow, fri, +3d, none"},
		{"#", "labels, comma-separated; name:color sets a color"},
		{"a", "archive"},
		{"D", "delete for good"},
	}},
	{"Card view", [][2]string{
		{"d", "edit notes (esc saves)"},
		{"c", "add checklist items"},
		{"j  k  space", "pick and tick an item"},
		{"r  X", "rename / remove an item"},
	}},
	{"Board", [][2]string{
		{"/", "filter: words, #label, !high (esc clears)"},
		{"C  R  X", "add / rename / delete column"},
		{"<  >", "move column left / right"},
		{"q  ctrl+c", "quit"},
	}},
}

func (m *model) viewHelp() string {
	var b strings.Builder
	for _, s := range helpSections {
		b.WriteString("\n  " + style.Bold.Render(s.title) + "\n")
		for _, k := range s.keys {
			b.WriteString("  " + style.AccentFg.Render(fmt.Sprintf("  %-20s", k[0])) + style.Dim.Render(k[1]) + "\n")
		}
	}
	b.WriteString("\n  " + style.Dim.Render("CLI: tack --help · tack add \"title\" -c doing -l bug -d fri -p high"))
	return b.String()
}

// ---- helpers ----

func pad(s string, w int) string {
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
}

// wrap breaks plain text into lines of at most w cells, splitting long words.
func wrap(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	out := strings.Split(ansi.Wrap(s, w, ""), "\n")
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

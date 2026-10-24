// Package store keeps boards, columns, cards, labels and checklists in SQLite.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Board struct {
	ID       int64
	Name     string
	Position int
}

type Column struct {
	ID       int64
	BoardID  int64
	Name     string
	Position int
}

type Label struct {
	ID      int64
	BoardID int64
	Name    string
	Color   string
}

type ChecklistItem struct {
	ID       int64
	CardID   int64
	Text     string
	Done     bool
	Position int
}

type Card struct {
	ID          int64
	BoardID     int64
	ColumnID    int64
	Title       string
	Description string
	Priority    int // 0 none, 1 low, 2 medium, 3 high
	Due         *time.Time
	Position    int
	Archived    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Labels      []Label
	Checklist   []ChecklistItem
}

// ChecklistDone returns how many checklist items are ticked off.
func (c Card) ChecklistDone() int {
	n := 0
	for _, it := range c.Checklist {
		if it.Done {
			n++
		}
	}
	return n
}

type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS boards (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL UNIQUE COLLATE NOCASE,
	position INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS columns (
	id INTEGER PRIMARY KEY,
	board_id INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	position INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS cards (
	id INTEGER PRIMARY KEY,
	board_id INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
	column_id INTEGER NOT NULL REFERENCES columns(id) ON DELETE CASCADE,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	priority INTEGER NOT NULL DEFAULT 0,
	due TEXT,
	position INTEGER NOT NULL DEFAULT 0,
	archived INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS cards_column ON cards(column_id, archived, position);
CREATE TABLE IF NOT EXISTS labels (
	id INTEGER PRIMARY KEY,
	board_id INTEGER NOT NULL REFERENCES boards(id) ON DELETE CASCADE,
	name TEXT NOT NULL COLLATE NOCASE,
	color TEXT NOT NULL,
	UNIQUE(board_id, name)
);
CREATE TABLE IF NOT EXISTS card_labels (
	card_id INTEGER NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
	label_id INTEGER NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
	PRIMARY KEY(card_id, label_id)
);
CREATE TABLE IF NOT EXISTS checklist_items (
	id INTEGER PRIMARY KEY,
	card_id INTEGER NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
	text TEXT NOT NULL,
	done INTEGER NOT NULL DEFAULT 0,
	position INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
`

// DefaultPath is $TACK_DB, else $XDG_DATA_HOME/tack/tack.db, else ~/.local/share/tack/tack.db.
func DefaultPath() (string, error) {
	if p := os.Getenv("TACK_DB"); p != "" {
		return p, nil
	}
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "tack", "tack.db"), nil
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func now() int64 { return time.Now().Unix() }

// ---- boards ----

func (s *Store) Boards() ([]Board, error) {
	rows, err := s.db.Query(`SELECT id, name, position FROM boards ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Board
	for rows.Next() {
		var b Board
		if err := rows.Scan(&b.ID, &b.Name, &b.Position); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) Board(id int64) (Board, error) {
	var b Board
	err := s.db.QueryRow(`SELECT id, name, position FROM boards WHERE id = ?`, id).Scan(&b.ID, &b.Name, &b.Position)
	if errors.Is(err, sql.ErrNoRows) {
		return b, fmt.Errorf("board %d: %w", id, ErrNotFound)
	}
	return b, err
}

// BoardByName matches case-insensitively, falling back to a unique prefix.
func (s *Store) BoardByName(name string) (Board, error) {
	boards, err := s.Boards()
	if err != nil {
		return Board{}, err
	}
	var prefix []Board
	for _, b := range boards {
		if strings.EqualFold(b.Name, name) {
			return b, nil
		}
		if strings.HasPrefix(strings.ToLower(b.Name), strings.ToLower(name)) {
			prefix = append(prefix, b)
		}
	}
	if len(prefix) == 1 {
		return prefix[0], nil
	}
	return Board{}, fmt.Errorf("board %q: %w", name, ErrNotFound)
}

var DefaultColumns = []string{"To do", "Doing", "Done"}

func (s *Store) CreateBoard(name string, columns []string) (Board, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Board{}, errors.New("board name is empty")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Board{}, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO boards(name, position) VALUES(?, (SELECT COALESCE(MAX(position), -1) + 1 FROM boards))`, name)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Board{}, fmt.Errorf("board %q already exists", name)
		}
		return Board{}, err
	}
	id, _ := res.LastInsertId()
	for i, c := range columns {
		if _, err := tx.Exec(`INSERT INTO columns(board_id, name, position) VALUES(?, ?, ?)`, id, c, i); err != nil {
			return Board{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Board{}, err
	}
	return s.Board(id)
}

func (s *Store) RenameBoard(id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("board name is empty")
	}
	_, err := s.db.Exec(`UPDATE boards SET name = ? WHERE id = ?`, name, id)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("board %q already exists", name)
	}
	return err
}

func (s *Store) DeleteBoard(id int64) error {
	_, err := s.db.Exec(`DELETE FROM boards WHERE id = ?`, id)
	return err
}

// CurrentBoard returns the selected board, creating a "Personal" board on first run.
func (s *Store) CurrentBoard() (Board, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = 'current_board'`).Scan(&v)
	if err == nil {
		var id int64
		fmt.Sscan(v, &id)
		if b, err := s.Board(id); err == nil {
			return b, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Board{}, err
	}
	boards, err := s.Boards()
	if err != nil {
		return Board{}, err
	}
	var b Board
	if len(boards) > 0 {
		b = boards[0]
	} else if b, err = s.CreateBoard("Personal", DefaultColumns); err != nil {
		return Board{}, err
	}
	return b, s.SetCurrentBoard(b.ID)
}

func (s *Store) SetCurrentBoard(id int64) error {
	_, err := s.db.Exec(`INSERT INTO settings(key, value) VALUES('current_board', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, fmt.Sprint(id))
	return err
}

// ---- columns ----

func (s *Store) Columns(boardID int64) ([]Column, error) {
	rows, err := s.db.Query(`SELECT id, board_id, name, position FROM columns WHERE board_id = ? ORDER BY position, id`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Column
	for rows.Next() {
		var c Column
		if err := rows.Scan(&c.ID, &c.BoardID, &c.Name, &c.Position); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ColumnByName matches case-insensitively, falling back to a unique prefix.
func (s *Store) ColumnByName(boardID int64, name string) (Column, error) {
	cols, err := s.Columns(boardID)
	if err != nil {
		return Column{}, err
	}
	var prefix []Column
	for _, c := range cols {
		if strings.EqualFold(c.Name, name) {
			return c, nil
		}
		if strings.HasPrefix(strings.ToLower(c.Name), strings.ToLower(name)) {
			prefix = append(prefix, c)
		}
	}
	if len(prefix) == 1 {
		return prefix[0], nil
	}
	return Column{}, fmt.Errorf("column %q: %w", name, ErrNotFound)
}

func (s *Store) CreateColumn(boardID int64, name string) (Column, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Column{}, errors.New("column name is empty")
	}
	res, err := s.db.Exec(`INSERT INTO columns(board_id, name, position)
		VALUES(?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM columns WHERE board_id = ?))`, boardID, name, boardID)
	if err != nil {
		return Column{}, err
	}
	id, _ := res.LastInsertId()
	var c Column
	err = s.db.QueryRow(`SELECT id, board_id, name, position FROM columns WHERE id = ?`, id).Scan(&c.ID, &c.BoardID, &c.Name, &c.Position)
	return c, err
}

func (s *Store) RenameColumn(id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("column name is empty")
	}
	_, err := s.db.Exec(`UPDATE columns SET name = ? WHERE id = ?`, name, id)
	return err
}

// DeleteColumn removes a column together with all its cards, archived ones included.
func (s *Store) DeleteColumn(id int64) error {
	_, err := s.db.Exec(`DELETE FROM columns WHERE id = ?`, id)
	return err
}

// MoveColumn shifts a column left (delta < 0) or right (delta > 0).
func (s *Store) MoveColumn(id int64, delta int) error {
	var boardID int64
	if err := s.db.QueryRow(`SELECT board_id FROM columns WHERE id = ?`, id).Scan(&boardID); err != nil {
		return err
	}
	cols, err := s.Columns(boardID)
	if err != nil {
		return err
	}
	ids := make([]int64, len(cols))
	from := -1
	for i, c := range cols {
		ids[i] = c.ID
		if c.ID == id {
			from = i
		}
	}
	to := clamp(from+delta, 0, len(ids)-1)
	if to == from {
		return nil
	}
	ids = moveID(ids, from, to)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, cid := range ids {
		if _, err := tx.Exec(`UPDATE columns SET position = ? WHERE id = ?`, i, cid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CountCards counts all cards in a column, archived ones included.
func (s *Store) CountCards(columnID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM cards WHERE column_id = ?`, columnID).Scan(&n)
	return n, err
}

// ---- cards ----

const cardCols = `id, board_id, column_id, title, description, priority, due, position, archived, created_at, updated_at`

func scanCard(sc interface{ Scan(...any) error }) (Card, error) {
	var c Card
	var due sql.NullString
	var created, updated int64
	err := sc.Scan(&c.ID, &c.BoardID, &c.ColumnID, &c.Title, &c.Description, &c.Priority, &due, &c.Position, &c.Archived, &created, &updated)
	if err != nil {
		return c, err
	}
	if due.Valid && due.String != "" {
		if t, err := time.ParseInLocation("2006-01-02", due.String, time.Local); err == nil {
			c.Due = &t
		}
	}
	c.CreatedAt = time.Unix(created, 0)
	c.UpdatedAt = time.Unix(updated, 0)
	return c, nil
}

func (s *Store) queryCards(q string, args ...any) ([]Card, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	var out []Card
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, s.attach(out)
}

// attach loads labels and checklist items for the given cards.
func (s *Store) attach(cards []Card) error {
	if len(cards) == 0 {
		return nil
	}
	idx := make(map[int64]int, len(cards))
	ph := make([]string, len(cards))
	args := make([]any, len(cards))
	for i, c := range cards {
		idx[c.ID] = i
		ph[i] = "?"
		args[i] = c.ID
	}
	in := strings.Join(ph, ",")

	rows, err := s.db.Query(`SELECT cl.card_id, l.id, l.board_id, l.name, l.color
		FROM card_labels cl JOIN labels l ON l.id = cl.label_id
		WHERE cl.card_id IN (`+in+`) ORDER BY l.name`, args...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cardID int64
		var l Label
		if err := rows.Scan(&cardID, &l.ID, &l.BoardID, &l.Name, &l.Color); err != nil {
			rows.Close()
			return err
		}
		i := idx[cardID]
		cards[i].Labels = append(cards[i].Labels, l)
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT id, card_id, text, done, position FROM checklist_items
		WHERE card_id IN (`+in+`) ORDER BY position, id`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var it ChecklistItem
		if err := rows.Scan(&it.ID, &it.CardID, &it.Text, &it.Done, &it.Position); err != nil {
			return err
		}
		i := idx[it.CardID]
		cards[i].Checklist = append(cards[i].Checklist, it)
	}
	return rows.Err()
}

// Cards returns a board's cards ordered by column and position.
func (s *Store) Cards(boardID int64, archived bool) ([]Card, error) {
	return s.queryCards(`SELECT `+prefixCols("c")+` FROM cards c JOIN columns col ON col.id = c.column_id
		WHERE c.board_id = ? AND c.archived = ? ORDER BY col.position, c.position, c.id`, boardID, archived)
}

// AllCards returns cards across every board.
func (s *Store) AllCards(archived bool) ([]Card, error) {
	return s.queryCards(`SELECT `+prefixCols("c")+` FROM cards c
		JOIN columns col ON col.id = c.column_id JOIN boards b ON b.id = c.board_id
		WHERE c.archived = ? ORDER BY b.position, col.position, c.position, c.id`, archived)
}

func prefixCols(p string) string {
	parts := strings.Split(cardCols, ", ")
	for i := range parts {
		parts[i] = p + "." + parts[i]
	}
	return strings.Join(parts, ", ")
}

func (s *Store) Card(id int64) (Card, error) {
	cards, err := s.queryCards(`SELECT `+cardCols+` FROM cards WHERE id = ?`, id)
	if err != nil {
		return Card{}, err
	}
	if len(cards) == 0 {
		return Card{}, fmt.Errorf("card #%d: %w", id, ErrNotFound)
	}
	return cards[0], nil
}

func (s *Store) CreateCard(boardID, columnID int64, title string) (Card, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Card{}, errors.New("card title is empty")
	}
	t := now()
	res, err := s.db.Exec(`INSERT INTO cards(board_id, column_id, title, position, created_at, updated_at)
		VALUES(?, ?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM cards WHERE column_id = ?), ?, ?)`,
		boardID, columnID, title, columnID, t, t)
	if err != nil {
		return Card{}, err
	}
	id, _ := res.LastInsertId()
	return s.Card(id)
}

// UpdateCard saves title, description, priority and due date.
func (s *Store) UpdateCard(c Card) error {
	if strings.TrimSpace(c.Title) == "" {
		return errors.New("card title is empty")
	}
	var due any
	if c.Due != nil {
		due = c.Due.Format("2006-01-02")
	}
	_, err := s.db.Exec(`UPDATE cards SET title = ?, description = ?, priority = ?, due = ?, updated_at = ? WHERE id = ?`,
		strings.TrimSpace(c.Title), c.Description, c.Priority, due, now(), c.ID)
	return err
}

// MoveCard puts a card into columnID at index among that column's active cards (-1 = end).
func (s *Store) MoveCard(id, columnID int64, index int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT id FROM cards WHERE column_id = ? AND archived = 0 AND id != ? ORDER BY position, id`, columnID, id)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var cid int64
		if err := rows.Scan(&cid); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, cid)
	}
	rows.Close()
	if index < 0 || index > len(ids) {
		index = len(ids)
	}
	ids = append(ids[:index], append([]int64{id}, ids[index:]...)...)
	for i, cid := range ids {
		if _, err := tx.Exec(`UPDATE cards SET position = ? WHERE id = ?`, i, cid); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE cards SET column_id = ?, updated_at = ? WHERE id = ?`, columnID, now(), id); err != nil {
		return err
	}
	return tx.Commit()
}

// SetArchived archives a card, or restores it to the end of its column.
func (s *Store) SetArchived(id int64, archived bool) error {
	if archived {
		_, err := s.db.Exec(`UPDATE cards SET archived = 1, updated_at = ? WHERE id = ?`, now(), id)
		return err
	}
	_, err := s.db.Exec(`UPDATE cards SET archived = 0, updated_at = ?,
		position = (SELECT COALESCE(MAX(position), -1) + 1 FROM cards x WHERE x.column_id = cards.column_id AND x.archived = 0)
		WHERE id = ?`, now(), id)
	return err
}

func (s *Store) DeleteCard(id int64) error {
	_, err := s.db.Exec(`DELETE FROM cards WHERE id = ?`, id)
	return err
}

// ---- labels ----

var LabelColors = []string{"red", "orange", "yellow", "green", "teal", "blue", "purple", "pink", "gray"}

func ValidColor(c string) bool {
	for _, x := range LabelColors {
		if x == c {
			return true
		}
	}
	return false
}

func (s *Store) Labels(boardID int64) ([]Label, error) {
	rows, err := s.db.Query(`SELECT id, board_id, name, color FROM labels WHERE board_id = ? ORDER BY name`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Label
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.ID, &l.BoardID, &l.Name, &l.Color); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// EnsureLabel returns the board's label with this name, creating it if needed.
// An empty color keeps an existing label's color or picks the next one in the palette.
func (s *Store) EnsureLabel(boardID int64, name, color string) (Label, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Label{}, errors.New("label name is empty")
	}
	if color != "" && !ValidColor(color) {
		return Label{}, fmt.Errorf("unknown color %q (use %s)", color, strings.Join(LabelColors, ", "))
	}
	var l Label
	err := s.db.QueryRow(`SELECT id, board_id, name, color FROM labels WHERE board_id = ? AND name = ?`, boardID, name).
		Scan(&l.ID, &l.BoardID, &l.Name, &l.Color)
	if err == nil {
		if color != "" && color != l.Color {
			if _, err := s.db.Exec(`UPDATE labels SET color = ? WHERE id = ?`, color, l.ID); err != nil {
				return l, err
			}
			l.Color = color
		}
		return l, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return l, err
	}
	if color == "" {
		var n int
		s.db.QueryRow(`SELECT COUNT(*) FROM labels WHERE board_id = ?`, boardID).Scan(&n)
		color = LabelColors[n%(len(LabelColors)-1)] // gray is never auto-picked
	}
	res, err := s.db.Exec(`INSERT INTO labels(board_id, name, color) VALUES(?, ?, ?)`, boardID, name, color)
	if err != nil {
		return l, err
	}
	id, _ := res.LastInsertId()
	return Label{ID: id, BoardID: boardID, Name: name, Color: color}, nil
}

func (s *Store) DeleteLabel(id int64) error {
	_, err := s.db.Exec(`DELETE FROM labels WHERE id = ?`, id)
	return err
}

func (s *Store) SetCardLabels(cardID int64, labelIDs []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM card_labels WHERE card_id = ?`, cardID); err != nil {
		return err
	}
	for _, id := range labelIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO card_labels(card_id, label_id) VALUES(?, ?)`, cardID, id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`UPDATE cards SET updated_at = ? WHERE id = ?`, now(), cardID); err != nil {
		return err
	}
	return tx.Commit()
}

// ---- checklist ----

func (s *Store) AddChecklistItem(cardID int64, text string) (ChecklistItem, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return ChecklistItem{}, errors.New("checklist item is empty")
	}
	res, err := s.db.Exec(`INSERT INTO checklist_items(card_id, text, position)
		VALUES(?, ?, (SELECT COALESCE(MAX(position), -1) + 1 FROM checklist_items WHERE card_id = ?))`, cardID, text, cardID)
	if err != nil {
		return ChecklistItem{}, err
	}
	id, _ := res.LastInsertId()
	return ChecklistItem{ID: id, CardID: cardID, Text: text}, nil
}

func (s *Store) ToggleChecklistItem(id int64) error {
	_, err := s.db.Exec(`UPDATE checklist_items SET done = 1 - done WHERE id = ?`, id)
	return err
}

func (s *Store) RenameChecklistItem(id int64, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("checklist item is empty")
	}
	_, err := s.db.Exec(`UPDATE checklist_items SET text = ? WHERE id = ?`, text, id)
	return err
}

func (s *Store) DeleteChecklistItem(id int64) error {
	_, err := s.db.Exec(`DELETE FROM checklist_items WHERE id = ?`, id)
	return err
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func moveID(ids []int64, from, to int) []int64 {
	id := ids[from]
	ids = append(ids[:from], ids[from+1:]...)
	return append(ids[:to], append([]int64{id}, ids[to:]...)...)
}

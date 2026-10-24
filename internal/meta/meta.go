// Package meta parses and formats card metadata: due dates, priorities and search queries.
package meta

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/DanielFrengl/tack/internal/store"
)

func today(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// ParseDue understands YYYY-MM-DD, today, tomorrow, +3d, +2w, weekday names
// (next occurrence) and none/clear. A nil result means "no due date".
func ParseDue(s string, now time.Time) (*time.Time, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	t0 := today(now)
	var t time.Time
	switch {
	case s == "" || s == "none" || s == "clear" || s == "-":
		return nil, nil
	case s == "today" || s == "tod":
		t = t0
	case s == "tomorrow" || s == "tom":
		t = t0.AddDate(0, 0, 1)
	case strings.HasPrefix(s, "+") && len(s) > 2:
		n, err := strconv.Atoi(s[1 : len(s)-1])
		if err != nil {
			return nil, fmt.Errorf("bad due date %q", s)
		}
		switch s[len(s)-1] {
		case 'd':
			t = t0.AddDate(0, 0, n)
		case 'w':
			t = t0.AddDate(0, 0, 7*n)
		case 'm':
			t = t0.AddDate(0, n, 0)
		default:
			return nil, fmt.Errorf("bad due date %q (try +3d, +2w, +1m)", s)
		}
	default:
		if len(s) >= 3 {
			if wd, ok := weekdays[s[:3]]; ok {
				diff := (int(wd) - int(t0.Weekday()) + 7) % 7
				if diff == 0 {
					diff = 7
				}
				t = t0.AddDate(0, 0, diff)
				break
			}
		}
		var err error
		for _, layout := range []string{"2006-01-02", "2.1.2006", "2.1."} {
			if t, err = time.ParseInLocation(layout, s, time.Local); err == nil {
				if layout == "2.1." {
					t = time.Date(t0.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
					if t.Before(t0) {
						t = t.AddDate(1, 0, 0)
					}
				}
				break
			}
		}
		if err != nil {
			return nil, fmt.Errorf("bad due date %q (try 2026-11-03, 3.11., tomorrow, fri, +3d, none)", s)
		}
	}
	return &t, nil
}

// DaysUntil is negative for overdue dates.
func DaysUntil(due time.Time, now time.Time) int {
	return int(today(due).Sub(today(now)).Hours() / 24)
}

// FormatDue renders a due date relative to now: "today", "tomorrow", "in 3d", "2d late", "Nov 3".
func FormatDue(due time.Time, now time.Time) string {
	d := DaysUntil(due, now)
	switch {
	case d == 0:
		return "today"
	case d == 1:
		return "tomorrow"
	case d == -1:
		return "yesterday"
	case d < 0:
		return fmt.Sprintf("%dd late", -d)
	case d < 7:
		return due.Format("Mon")
	case due.Year() == now.Year():
		return due.Format("Jan 2")
	default:
		return due.Format("Jan 2 2006")
	}
}

var PriorityNames = []string{"none", "low", "medium", "high"}

func ParsePriority(s string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "0", "none", "no", "-":
		return 0, nil
	case "1", "low", "l":
		return 1, nil
	case "2", "medium", "med", "m":
		return 2, nil
	case "3", "high", "hi", "h":
		return 3, nil
	}
	return 0, fmt.Errorf("bad priority %q (none, low, medium, high)", s)
}

// Matches reports whether a card matches every word of the query. Each word is
// matched against title, description and label names; a word that isn't a
// substring of any of them still matches if its letters appear in order in the title.
// Words like #bug restrict to a label, and !high to a priority.
func Matches(c store.Card, query string) bool {
	for _, w := range strings.Fields(strings.ToLower(query)) {
		if !matchWord(c, w) {
			return false
		}
	}
	return true
}

func matchWord(c store.Card, w string) bool {
	if strings.HasPrefix(w, "#") && len(w) > 1 {
		for _, l := range c.Labels {
			if strings.HasPrefix(strings.ToLower(l.Name), w[1:]) {
				return true
			}
		}
		return false
	}
	if strings.HasPrefix(w, "!") && len(w) > 1 {
		p, err := ParsePriority(w[1:])
		return err == nil && c.Priority == p
	}
	title := strings.ToLower(c.Title)
	if strings.Contains(title, w) || strings.Contains(strings.ToLower(c.Description), w) {
		return true
	}
	for _, l := range c.Labels {
		if strings.Contains(strings.ToLower(l.Name), w) {
			return true
		}
	}
	return len([]rune(w)) >= 3 && subsequence(title, w)
}

func subsequence(s, sub string) bool {
	r := []rune(sub)
	i := 0
	for _, ch := range s {
		if i < len(r) && ch == r[i] {
			i++
		}
	}
	return i == len(r)
}

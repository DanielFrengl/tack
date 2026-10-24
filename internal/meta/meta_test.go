package meta

import (
	"testing"
	"time"

	"github.com/DanielFrengl/tack/internal/store"
)

func TestParseDue(t *testing.T) {
	now := time.Date(2026, 10, 24, 15, 0, 0, 0, time.Local) // a Saturday
	cases := map[string]string{
		"today":      "2026-10-24",
		"tomorrow":   "2026-10-25",
		"+3d":        "2026-10-27",
		"+2w":        "2026-11-07",
		"+1m":        "2026-11-24",
		"fri":        "2026-10-30",
		"saturday":   "2026-10-31",
		"2026-12-01": "2026-12-01",
		"3.11.":      "2026-11-03",
		"1.2.":       "2027-02-01",
		"5.1.2027":   "2027-01-05",
	}
	for in, want := range cases {
		got, err := ParseDue(in, now)
		if err != nil || got == nil || got.Format("2006-01-02") != want {
			t.Errorf("ParseDue(%q) = %v, %v; want %s", in, got, err, want)
		}
	}
	if got, err := ParseDue("none", now); got != nil || err != nil {
		t.Errorf("none should clear the date, got %v %v", got, err)
	}
	if _, err := ParseDue("someday", now); err == nil {
		t.Error("someday should not parse")
	}
}

func TestMatches(t *testing.T) {
	c := store.Card{Title: "Fix login on Safari", Description: "iOS only", Priority: 3,
		Labels: []store.Label{{Name: "bug"}}}
	for q, want := range map[string]bool{
		"":           true,
		"login":      true,
		"ios":        true,
		"#bug":       true,
		"#design":    false,
		"!high":      true,
		"!low":       false,
		"fxlgn":      true, // letters in order
		"login #bug": true,
		"login zz":   false,
	} {
		if got := Matches(c, q); got != want {
			t.Errorf("Matches(%q) = %v, want %v", q, got, want)
		}
	}
}

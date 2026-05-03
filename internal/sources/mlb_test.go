package sources

import (
	"testing"
	"time"
)

func TestWindowAroundNow(t *testing.T) {
	now := time.Now()
	mk := func(d time.Duration) Game { return Game{When: now.Add(d)} }
	games := []Game{
		mk(-30 * 24 * time.Hour),
		mk(-7 * 24 * time.Hour),
		mk(-2 * 24 * time.Hour),
		mk(-1 * 24 * time.Hour), // closest in past
		mk(1 * 24 * time.Hour),  // closest in future
		mk(3 * 24 * time.Hour),
		mk(8 * 24 * time.Hour),
		mk(20 * 24 * time.Hour),
	}
	got := windowAroundNow(games, 5)
	if len(got) != 5 {
		t.Fatalf("want 5 games, got %d", len(got))
	}
	// Both games adjacent to "now" should land in the window, regardless
	// of which side wins the tie when picking the focal.
	want := map[time.Time]bool{games[3].When: false, games[4].When: false}
	for _, g := range got {
		if _, ok := want[g.When]; ok {
			want[g.When] = true
		}
	}
	for ts, seen := range want {
		if !seen {
			t.Errorf("expected game at %v in window; got %+v", ts, got)
		}
	}
}

func TestWindowShortSchedule(t *testing.T) {
	games := []Game{{}, {}, {}}
	got := windowAroundNow(games, 5)
	if len(got) != 3 {
		t.Errorf("short schedule should pass through; got %d", len(got))
	}
}

func TestWindowEndOfSeason(t *testing.T) {
	now := time.Now()
	games := []Game{
		{When: now.Add(-100 * 24 * time.Hour)},
		{When: now.Add(-90 * 24 * time.Hour)},
		{When: now.Add(-80 * 24 * time.Hour)},
		{When: now.Add(-70 * 24 * time.Hour)},
		{When: now.Add(-60 * 24 * time.Hour)},
		{When: now.Add(-50 * 24 * time.Hour)},
		{When: now.Add(-40 * 24 * time.Hour)}, // closest, but at end
	}
	got := windowAroundNow(games, 5)
	if len(got) != 5 {
		t.Fatalf("want 5 games, got %d", len(got))
	}
	// Closest is the last one — window should clamp to the final 5.
	if got[len(got)-1] != games[len(games)-1] {
		t.Error("expected window to include the last game")
	}
}

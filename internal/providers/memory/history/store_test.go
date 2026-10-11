package history_test

import (
	"context"
	"strings"
	"testing"

	domain "nadir/internal/core/conversation/history"
	"nadir/internal/providers/memory/history"
)

var ctx = context.Background()

func TestAppendMintsSessionAndSequences(t *testing.T) {
	s := history.New()
	if err := s.AppendTurn(ctx, "s1", domain.Turn{Query: "first"}, "  first question "); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendTurn(ctx, "s1", domain.Turn{Query: "second"}, "ignored"); err != nil {
		t.Fatal(err)
	}
	sess, err := s.GetSession(ctx, "s1")
	if err != nil || sess.Title != "first question" || sess.TurnCount != 2 {
		t.Fatalf("session = %+v, %v", sess, err)
	}
	turns, _ := s.ListTurns(ctx, "s1")
	if len(turns) != 2 || turns[0].Sequence != 0 || turns[1].Sequence != 1 || turns[1].Query != "second" || turns[0].ID == "" {
		t.Fatalf("turns = %+v", turns)
	}
}

func TestTruncateDropsTurnAndTail(t *testing.T) {
	s := history.New()
	for _, q := range []string{"a", "b", "c"} {
		_ = s.AppendTurn(ctx, "s1", domain.Turn{Query: q}, q)
	}
	if err := s.TruncateSession(ctx, "s1", 1); err != nil {
		t.Fatal(err)
	}
	turns, _ := s.ListTurns(ctx, "s1")
	if len(turns) != 1 || turns[0].Query != "a" {
		t.Fatalf("turns = %+v, want only a", turns)
	}
	if err := s.TruncateSession(ctx, "s1", 5); err == nil {
		t.Fatal("truncating past the end succeeded")
	}
	if err := s.TruncateSession(ctx, "missing", 0); err == nil {
		t.Fatal("truncating a missing session succeeded")
	}
}

func TestListSessionsNewestFirstAndDelete(t *testing.T) {
	s := history.New()
	first, _ := s.CreateSession(ctx, "")
	second, _ := s.CreateSession(ctx, strings.Repeat("x", 100))
	_ = s.AppendTurn(ctx, first.ID, domain.Turn{Query: "bump"}, "")

	listed, _ := s.ListSessions(ctx, 10)
	if len(listed) != 2 || listed[0].ID != first.ID {
		t.Fatalf("listed = %+v, want the updated session first", listed)
	}
	if listed[1].Title != strings.Repeat("x", 60)+"…" || first.Title != "New chat" {
		t.Fatalf("titles = %q / %q", listed[1].Title, first.Title)
	}
	_ = s.DeleteSession(ctx, second.ID)
	if _, err := s.GetSession(ctx, second.ID); err == nil {
		t.Fatal("deleted session still found")
	}
	_ = s.DeleteAllSessions(ctx)
	if listed, _ = s.ListSessions(ctx, 10); len(listed) != 0 {
		t.Fatalf("sessions after delete all = %+v", listed)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	s := history.New()
	_ = s.AppendTurn(ctx, "s1", domain.Turn{Query: "q", Answer: "a", HasAnswer: true}, "q")
	data, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	restored := history.New()
	if err := restored.Restore(data); err != nil {
		t.Fatal(err)
	}
	turns, _ := restored.ListTurns(ctx, "s1")
	sess, _ := restored.GetSession(ctx, "s1")
	if len(turns) != 1 || turns[0].Answer != "a" || sess.TurnCount != 1 {
		t.Fatalf("restored = %+v / %+v", sess, turns)
	}
}

func TestRecencyDoesNotDependOnClockResolution(t *testing.T) {
	// A frozen clock makes every timestamp equal, as on coarse WASM clocks.
	s := history.New()
	history.FreezeClock(s)
	first, _ := s.CreateSession(ctx, "first")
	second, _ := s.CreateSession(ctx, "second")
	third, _ := s.CreateSession(ctx, "third")
	_ = s.AppendTurn(ctx, first.ID, domain.Turn{Query: "bump"}, "")

	listed, _ := s.ListSessions(ctx, 10)
	got := []string{listed[0].ID, listed[1].ID, listed[2].ID}
	want := []string{first.ID, third.ID, second.ID}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

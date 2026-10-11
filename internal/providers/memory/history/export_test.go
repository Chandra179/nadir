package history

import "time"

// FreezeClock makes every timestamp identical so tests can prove ordering does
// not depend on the wall clock.
func FreezeClock(s *Store) {
	frozen := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return frozen }
}

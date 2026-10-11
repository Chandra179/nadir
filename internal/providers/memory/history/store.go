// Package history is an in-memory chat history Adapter for runtimes without a
// server database, such as the browser. It satisfies the Conversation history
// seams with the same semantics as the Qdrant Adapter: AppendTurn mints the
// session on demand and assigns sequence numbers, and TruncateSession drops a
// turn and its tail. Snapshots let a host persist the conversations.
package history

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	domain "nadir/internal/core/conversation/history"
)

const (
	titleMaxLen      = 60
	defaultListLimit = 50
)

var _ domain.Reader = (*Store)(nil)

type session struct {
	domain.Session
	turns []domain.Turn
	// touched orders sessions by last mutation. Wall-clock timestamps can tie
	// on coarse clocks (WASM), so recency never relies on them alone.
	touched uint64
}

// Store keeps sessions and their ordered turns in memory.
type Store struct {
	mu       sync.RWMutex
	sessions map[string]*session
	now      func() time.Time
	clock    uint64
}

// touch marks a session as the most recently changed. Callers hold s.mu.
func (s *Store) touch(sess *session) {
	s.clock++
	sess.touched = s.clock
}

// New returns an empty store.
func New() *Store {
	return &Store{sessions: map[string]*session{}, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Store) CreateSession(_ context.Context, title string) (domain.Session, error) {
	now := s.now()
	created := domain.Session{ID: uuid.NewString(), Title: truncateTitle(title), CreatedAt: now, UpdatedAt: now}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := &session{Session: created}
	s.touch(sess)
	s.sessions[created.ID] = sess
	return created, nil
}

func (s *Store) TruncateSession(_ context.Context, sessionID string, beforeSequence int) error {
	if beforeSequence < 0 {
		return fmt.Errorf("history: invalid edit position: %d", beforeSequence)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		return fmt.Errorf("history: edit session: session not found: %s", sessionID)
	}
	if beforeSequence > len(sess.turns) {
		return fmt.Errorf("history: edit position %d exceeds %d turns", beforeSequence, len(sess.turns))
	}
	sess.turns = sess.turns[:beforeSequence]
	sess.TurnCount = beforeSequence
	sess.UpdatedAt = s.now()
	s.touch(sess)
	return nil
}

func (s *Store) AppendTurn(_ context.Context, sessionID string, turn domain.Turn, firstTurnTitle string) error {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		sess = &session{Session: domain.Session{ID: sessionID, Title: truncateTitle(firstTurnTitle), CreatedAt: now}}
		s.sessions[sessionID] = sess
	}
	turn.ID = uuid.NewString()
	turn.SessionID = sessionID
	turn.Sequence = len(sess.turns)
	turn.CreatedAt = now
	sess.turns = append(sess.turns, turn)
	sess.TurnCount = len(sess.turns)
	sess.UpdatedAt = now
	s.touch(sess)
	return nil
}

// ListSessions returns the most recently updated sessions first.
func (s *Store) ListSessions(_ context.Context, limit int) ([]domain.Session, error) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ordered := make([]*session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		ordered = append(ordered, sess)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].touched > ordered[j].touched })
	if len(ordered) > limit {
		ordered = ordered[:limit]
	}
	out := make([]domain.Session, len(ordered))
	for i, sess := range ordered {
		out[i] = sess.Session
	}
	return out, nil
}

func (s *Store) GetSession(_ context.Context, sessionID string) (domain.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		return domain.Session{}, fmt.Errorf("history: session not found: %s", sessionID)
	}
	return sess.Session, nil
}

func (s *Store) ListTurns(_ context.Context, sessionID string) ([]domain.Turn, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		return nil, nil
	}
	return append([]domain.Turn(nil), sess.turns...), nil
}

func (s *Store) DeleteSession(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
	return nil
}

func (s *Store) DeleteAllSessions(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = map[string]*session{}
	return nil
}

func truncateTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "New chat"
	}
	if r := []rune(title); len(r) > titleMaxLen {
		return string(r[:titleMaxLen]) + "…"
	}
	return title
}

type snapshotSession struct {
	Session domain.Session `json:"session"`
	Turns   []domain.Turn  `json:"turns"`
}

type snapshot struct {
	Version  int               `json:"version"`
	Sessions []snapshotSession `json:"sessions"`
}

// Snapshot serializes every conversation so a host can persist it.
func (s *Store) Snapshot() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := snapshot{Version: 1}
	for _, sess := range s.sessions {
		snap.Sessions = append(snap.Sessions, snapshotSession{Session: sess.Session, Turns: sess.turns})
	}
	sort.Slice(snap.Sessions, func(i, j int) bool { return snap.Sessions[i].Session.ID < snap.Sessions[j].Session.ID })
	return json.Marshal(snap)
}

// Restore replaces all conversations with a snapshot taken by Snapshot.
func (s *Store) Restore(data []byte) error {
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("decode history snapshot: %w", err)
	}
	if snap.Version != 1 {
		return fmt.Errorf("unsupported history snapshot version %d", snap.Version)
	}
	// Recency survives a restore through UpdatedAt; ties fall back to ID.
	sort.Slice(snap.Sessions, func(i, j int) bool {
		a, b := snap.Sessions[i].Session, snap.Sessions[j].Session
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			return a.UpdatedAt.Before(b.UpdatedAt)
		}
		return a.ID > b.ID
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions, s.clock = make(map[string]*session, len(snap.Sessions)), 0
	for _, item := range snap.Sessions {
		sess := &session{Session: item.Session, turns: item.Turns}
		sess.TurnCount = len(sess.turns)
		s.touch(sess)
		s.sessions[sess.ID] = sess
	}
	return nil
}

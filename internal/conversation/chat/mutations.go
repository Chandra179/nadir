package chat

import (
	"context"
	"errors"
	"sync"

	"nadir/internal/conversation/history"
)

var errStaleHistoryMutation = errors.New("chat: history mutation is stale")

// historyMutation identifies the chat state observed when a turn started.
// A session revision changes after an in-place edit or single-session delete;
// the global revision changes after delete-all. Appends carrying an older
// mutation are discarded so detached generation cannot recreate pruned data.
type historyMutation struct {
	sessionID       string
	globalRevision  uint64
	sessionRevision uint64
}

type trackedGeneration struct {
	sessionID string
	stream    *turnStream
}

// historyMutations is the chat lifecycle seam for destructive history
// operations. It serializes the mutation check with the underlying history
// call, so a delete cannot race an append after the check has passed.
type historyMutations struct {
	history historyStore
	mu      sync.Mutex

	globalRevision   uint64
	sessionRevisions map[string]uint64
	active           map[string]trackedGeneration
}

func newHistoryMutations(history historyStore) *historyMutations {
	return &historyMutations{
		history:          history,
		sessionRevisions: make(map[string]uint64),
		active:           make(map[string]trackedGeneration),
	}
}

func (m *historyMutations) capture(sessionID string) historyMutation {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentLocked(sessionID)
}

func (m *historyMutations) current(token historyMutation) bool {
	if token.sessionID == "" {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentLocked(token.sessionID) == token
}

func (m *historyMutations) currentLocked(sessionID string) historyMutation {
	return historyMutation{
		sessionID:       sessionID,
		globalRevision:  m.globalRevision,
		sessionRevision: m.sessionRevisions[sessionID],
	}
}

func (m *historyMutations) createSession(ctx context.Context, title string) (history.Session, historyMutation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, err := m.history.CreateSession(ctx, title)
	if err != nil {
		return history.Session{}, historyMutation{}, err
	}
	return session, m.currentLocked(session.ID), nil
}

func (m *historyMutations) prepareEdit(ctx context.Context, sessionID string, beforeSequence int) (historyMutation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Keep the lock through truncation. Existing appends either land before
	// the prune (and are removed when they are in the edited tail) or wait and
	// observe the new revision after the prune succeeds.
	if err := m.history.TruncateSession(ctx, sessionID, beforeSequence); err != nil {
		return historyMutation{}, err
	}
	m.sessionRevisions[sessionID]++
	m.cancelSessionLocked(sessionID)
	return m.currentLocked(sessionID), nil
}

func (m *historyMutations) append(ctx context.Context, token historyMutation, turn history.Turn, firstTurnTitle string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.currentLocked(token.sessionID).equals(token) {
		return errStaleHistoryMutation
	}
	return m.history.AppendTurn(ctx, token.sessionID, turn, firstTurnTitle)
}

func (m *historyMutations) deleteSession(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Invalidate before the destructive call. If Qdrant partially fails, old
	// detached saves remain invalid and a retry cannot resurrect deleted data.
	m.sessionRevisions[sessionID]++
	m.cancelSessionLocked(sessionID)
	return m.history.DeleteSession(ctx, sessionID)
}

func (m *historyMutations) deleteAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.globalRevision++
	for id, generation := range m.active {
		generation.stream.cancelGeneration()
		delete(m.active, id)
	}
	return m.history.DeleteAllSessions(ctx)
}

// cancelAll cancels every generation that is tied to a persisted session. It
// deliberately does not advance a revision: a shutdown cancellation must
// still allow the supervisor to persist its partial answer. A destructive
// delete uses deleteAll instead, which invalidates older mutations first.
func (m *historyMutations) cancelAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, generation := range m.active {
		generation.stream.cancelGeneration()
		delete(m.active, id)
	}
}

func (m *historyMutations) registerGeneration(turnID string, token historyMutation, stream *turnStream) bool {
	if token.sessionID == "" {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.currentLocked(token.sessionID).equals(token) {
		return false
	}
	m.active[turnID] = trackedGeneration{sessionID: token.sessionID, stream: stream}
	return true
}

func (m *historyMutations) unregisterGeneration(turnID string) {
	if turnID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.active, turnID)
}

func (m *historyMutations) cancelSessionLocked(sessionID string) {
	for id, generation := range m.active {
		if generation.sessionID != sessionID {
			continue
		}
		generation.stream.cancelGeneration()
		delete(m.active, id)
	}
}

func (t historyMutation) equals(other historyMutation) bool {
	return t.sessionID == other.sessionID &&
		t.globalRevision == other.globalRevision &&
		t.sessionRevision == other.sessionRevision
}

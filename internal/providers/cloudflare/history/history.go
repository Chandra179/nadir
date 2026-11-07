// Package history persists Conversation sessions and turns in Cloudflare D1.
// It owns the SQL schema, row encoding and D1 errors; Chat owns mutation
// ordering, revision checks and deletion policy.
//
// Unlike the Qdrant history Adapter there are no placeholder vectors: sessions
// and turns are plain rows. A session's turn count is derived from its turns,
// so it cannot drift from them.
package history

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	domainhistory "nadir/internal/core/conversation/history"
	"nadir/internal/providers/cloudflare/d1"
)

const (
	defaultListLimit    = 50
	defaultTurnPageSize = 500
	titleMaxLen         = 60
)

// Executor is the D1 capability this Adapter needs.
type Executor interface {
	Exec(ctx context.Context, statements ...d1.Statement) ([][]d1.Row, error)
}

// DependenciesConfig groups the D1 executor and the turn page size.
type DependenciesConfig struct {
	DB           Executor
	TurnPageSize int
}

type dependencies struct {
	db           Executor
	turnPageSize int
	// writeMu keeps the read-then-write truncate and the count-derived turn
	// sequence consistent between concurrent requests in this process.
	writeMu sync.Mutex
}

var _ domainhistory.Reader = (*dependencies)(nil)

// NewDependencies constructs a D1 chat-history Adapter.
func NewDependencies(cfg DependenciesConfig) (*dependencies, error) {
	if cfg.DB == nil {
		return nil, fmt.Errorf("history: d1 executor is required")
	}
	page := cfg.TurnPageSize
	if page <= 0 {
		page = defaultTurnPageSize
	}
	return &dependencies{db: cfg.DB, turnPageSize: page}, nil
}

var schema = []d1.Statement{
	{SQL: `CREATE TABLE IF NOT EXISTS chat_sessions (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`},
	{SQL: `CREATE INDEX IF NOT EXISTS chat_sessions_updated_at ON chat_sessions (updated_at DESC)`},
	{SQL: `CREATE TABLE IF NOT EXISTS chat_turns (
		session_id TEXT NOT NULL,
		sequence INTEGER NOT NULL,
		id TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		query TEXT NOT NULL,
		rewritten_query TEXT NOT NULL,
		subject_json TEXT NOT NULL,
		attached_files TEXT NOT NULL,
		top_k INTEGER NOT NULL,
		generate INTEGER NOT NULL,
		results_json TEXT NOT NULL,
		citations_json TEXT NOT NULL,
		result_count INTEGER NOT NULL,
		elapsed_ms INTEGER NOT NULL,
		from_cache INTEGER NOT NULL,
		prompt TEXT NOT NULL,
		answer TEXT NOT NULL,
		has_answer INTEGER NOT NULL,
		model TEXT NOT NULL,
		error TEXT NOT NULL,
		generate_error TEXT NOT NULL,
		failed INTEGER NOT NULL,
		PRIMARY KEY (session_id, sequence)
	)`},
}

// EnsureSchema creates the history tables when they do not exist.
func (d *dependencies) EnsureSchema(ctx context.Context) error {
	if _, err := d.db.Exec(ctx, schema...); err != nil {
		return fmt.Errorf("history: ensure schema: %w", err)
	}
	return nil
}

const sessionSelect = `SELECT s.id, s.title, s.created_at, s.updated_at,
	(SELECT COUNT(*) FROM chat_turns t WHERE t.session_id = s.id) AS turn_count
	FROM chat_sessions s`

// CreateSession creates a persisted chat session.
func (d *dependencies) CreateSession(ctx context.Context, title string) (domainhistory.Session, error) {
	title = truncateTitle(title)
	now := time.Now().UTC()
	id := uuid.NewString()
	if _, err := d.db.Exec(ctx, d1.Statement{
		SQL:    `INSERT INTO chat_sessions (id, title, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		Params: []any{id, title, now.UnixMilli(), now.UnixMilli()},
	}); err != nil {
		return domainhistory.Session{}, fmt.Errorf("history: create session: %w", err)
	}
	return domainhistory.Session{ID: id, Title: title, CreatedAt: now, UpdatedAt: now}, nil
}

// AppendTurn adds a turn to a session, creating the session (titled from
// firstTurnTitle) when it does not exist yet. The turn's sequence is the
// session's current turn count; the primary key rejects a duplicate.
func (d *dependencies) AppendTurn(ctx context.Context, sessionID string, turn domainhistory.Turn, firstTurnTitle string) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	now := time.Now().UTC().UnixMilli()

	turnID := turn.ID
	if turnID == "" {
		turnID = uuid.NewString()
	}
	results, err := jsonText(turn.Results, "results")
	if err != nil {
		return err
	}
	attached, err := jsonText(turn.AttachedFiles, "attached files")
	if err != nil {
		return err
	}
	citations, err := jsonText(turn.Citations, "citations")
	if err != nil {
		return err
	}
	subject, err := jsonText(turn.Subject, "subject")
	if err != nil {
		return err
	}

	_, err = d.db.Exec(ctx,
		d1.Statement{
			SQL:    `INSERT OR IGNORE INTO chat_sessions (id, title, created_at, updated_at) VALUES (?, ?, ?, ?)`,
			Params: []any{sessionID, truncateTitle(firstTurnTitle), now, now},
		},
		d1.Statement{
			SQL: `INSERT INTO chat_turns (session_id, sequence, id, created_at, query, rewritten_query, subject_json,
				attached_files, top_k, generate, results_json, citations_json, result_count, elapsed_ms, from_cache,
				prompt, answer, has_answer, model, error, generate_error, failed)
				VALUES (?, (SELECT COUNT(*) FROM chat_turns WHERE session_id = ?), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			Params: []any{sessionID, sessionID, turnID, now, turn.Query, turn.RewrittenQuery, subject,
				attached, turn.TopK, boolInt(turn.Generate), results, citations, turn.Count, turn.ElapsedMS, boolInt(turn.FromCache),
				turn.Prompt, turn.Answer, boolInt(turn.HasAnswer), turn.Model, turn.Error, turn.GenerateError, boolInt(turn.Failed)},
		},
		d1.Statement{
			SQL:    `UPDATE chat_sessions SET updated_at = ? WHERE id = ?`,
			Params: []any{now, sessionID},
		},
	)
	if err != nil {
		return fmt.Errorf("history: append turn: %w", err)
	}
	return nil
}

// TruncateSession replaces the tail of an existing conversation in place: the
// turn at beforeSequence and every later turn are removed.
func (d *dependencies) TruncateSession(ctx context.Context, sessionID string, beforeSequence int) error {
	if beforeSequence < 0 {
		return fmt.Errorf("history: invalid edit position: %d", beforeSequence)
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	session, err := d.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("history: edit session: %w", err)
	}
	if beforeSequence > session.TurnCount {
		return fmt.Errorf("history: edit position %d exceeds %d turns", beforeSequence, session.TurnCount)
	}
	if _, err := d.db.Exec(ctx,
		d1.Statement{SQL: `DELETE FROM chat_turns WHERE session_id = ? AND sequence >= ?`, Params: []any{sessionID, beforeSequence}},
		d1.Statement{SQL: `UPDATE chat_sessions SET updated_at = ? WHERE id = ?`, Params: []any{time.Now().UTC().UnixMilli(), sessionID}},
	); err != nil {
		return fmt.Errorf("history: truncate session: %w", err)
	}
	return nil
}

// ListSessions returns the most recently updated sessions first.
func (d *dependencies) ListSessions(ctx context.Context, limit int) ([]domainhistory.Session, error) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	rows, err := d.db.Exec(ctx, d1.Statement{
		SQL:    sessionSelect + ` ORDER BY s.updated_at DESC, s.id LIMIT ?`,
		Params: []any{limit},
	})
	if err != nil {
		return nil, fmt.Errorf("history: list sessions: %w", err)
	}
	out := make([]domainhistory.Session, len(rows[0]))
	for i, row := range rows[0] {
		out[i] = sessionFromRow(row)
	}
	return out, nil
}

// GetSession returns one session or an error when it does not exist.
func (d *dependencies) GetSession(ctx context.Context, sessionID string) (domainhistory.Session, error) {
	rows, err := d.db.Exec(ctx, d1.Statement{SQL: sessionSelect + ` WHERE s.id = ?`, Params: []any{sessionID}})
	if err != nil {
		return domainhistory.Session{}, fmt.Errorf("history: get session: %w", err)
	}
	if len(rows[0]) == 0 {
		return domainhistory.Session{}, fmt.Errorf("history: session not found: %s", sessionID)
	}
	return sessionFromRow(rows[0][0]), nil
}

// ListTurns returns a session's turns in sequence order, reading in pages so
// one response never carries an unbounded number of large rows.
func (d *dependencies) ListTurns(ctx context.Context, sessionID string) ([]domainhistory.Turn, error) {
	var out []domainhistory.Turn
	next := int64(0)
	for {
		rows, err := d.db.Exec(ctx, d1.Statement{
			SQL:    `SELECT * FROM chat_turns WHERE session_id = ? AND sequence >= ? ORDER BY sequence LIMIT ?`,
			Params: []any{sessionID, next, d.turnPageSize},
		})
		if err != nil {
			return nil, fmt.Errorf("history: list turns: %w", err)
		}
		for _, row := range rows[0] {
			turn, err := turnFromRow(row)
			if err != nil {
				return nil, err
			}
			out = append(out, turn)
			next = int64(turn.Sequence) + 1
		}
		if len(rows[0]) < d.turnPageSize {
			return out, nil
		}
	}
}

// DeleteSession removes a session and all of its turns.
func (d *dependencies) DeleteSession(ctx context.Context, sessionID string) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if _, err := d.db.Exec(ctx,
		d1.Statement{SQL: `DELETE FROM chat_turns WHERE session_id = ?`, Params: []any{sessionID}},
		d1.Statement{SQL: `DELETE FROM chat_sessions WHERE id = ?`, Params: []any{sessionID}},
	); err != nil {
		return fmt.Errorf("history: delete session: %w", err)
	}
	return nil
}

// DeleteAllSessions permanently removes every persisted chat session and turn,
// leaving the document corpus and semantic cache untouched.
func (d *dependencies) DeleteAllSessions(ctx context.Context) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if _, err := d.db.Exec(ctx,
		d1.Statement{SQL: `DELETE FROM chat_turns`},
		d1.Statement{SQL: `DELETE FROM chat_sessions`},
	); err != nil {
		return fmt.Errorf("history: delete all sessions: %w", err)
	}
	return nil
}

func truncateTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "New chat"
	}
	r := []rune(s)
	if len(r) > titleMaxLen {
		return string(r[:titleMaxLen]) + "…"
	}
	return s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func jsonText(v any, what string) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("history: marshal %s: %w", what, err)
	}
	return string(raw), nil
}

func sessionFromRow(r d1.Row) domainhistory.Session {
	return domainhistory.Session{
		ID:        r.Str("id"),
		Title:     r.Str("title"),
		CreatedAt: time.UnixMilli(r.Int("created_at")).UTC(),
		UpdatedAt: time.UnixMilli(r.Int("updated_at")).UTC(),
		TurnCount: int(r.Int("turn_count")),
	}
}

func turnFromRow(r d1.Row) (domainhistory.Turn, error) {
	turn := domainhistory.Turn{
		ID:             r.Str("id"),
		SessionID:      r.Str("session_id"),
		Sequence:       int(r.Int("sequence")),
		CreatedAt:      time.UnixMilli(r.Int("created_at")).UTC(),
		Query:          r.Str("query"),
		RewrittenQuery: r.Str("rewritten_query"),
		TopK:           int(r.Int("top_k")),
		Generate:       r.Bool("generate"),
		Count:          int(r.Int("result_count")),
		ElapsedMS:      r.Int("elapsed_ms"),
		FromCache:      r.Bool("from_cache"),
		Prompt:         r.Str("prompt"),
		Answer:         r.Str("answer"),
		HasAnswer:      r.Bool("has_answer"),
		Model:          r.Str("model"),
		Error:          r.Str("error"),
		GenerateError:  r.Str("generate_error"),
		Failed:         r.Bool("failed"),
	}
	for _, field := range []struct {
		column string
		into   any
	}{
		{"subject_json", &turn.Subject},
		{"citations_json", &turn.Citations},
		{"results_json", &turn.Results},
		{"attached_files", &turn.AttachedFiles},
	} {
		if raw := r.Str(field.column); raw != "" {
			if err := json.Unmarshal([]byte(raw), field.into); err != nil {
				return domainhistory.Turn{}, fmt.Errorf("history: decode %s: %w", field.column, err)
			}
		}
	}
	return turn, nil
}

package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	domainhistory "nadir/internal/core/conversation/history"
	"nadir/internal/providers/cloudflare/d1"
)

// fakeD1 serves D1's REST query endpoint over a real in-memory SQLite, so the
// adapter's SQL is executed rather than pattern-matched. A batch runs in one
// transaction, as D1 documents.
type fakeD1 struct {
	db      *sql.DB
	srv     *httptest.Server
	failSQL string // statements containing this text fail
}

func newFakeD1(t *testing.T) (*fakeD1, *dependencies) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	f := &fakeD1{db: db}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(func() { f.srv.Close(); _ = db.Close() })

	client, err := d1.New(d1.Config{AccountID: "acct", APIToken: "tok", DatabaseID: "db", BaseURL: f.srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewDependencies(DependenciesConfig{DB: client, TurnPageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	return f, h
}

type fakeStatement struct {
	SQL    string `json:"sql"`
	Params []any  `json:"params"`
}

func (f *fakeD1) handle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		fakeStatement
		Batch []fakeStatement `json:"batch"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	statements := body.Batch
	if len(statements) == 0 {
		statements = []fakeStatement{body.fakeStatement}
	}
	tx, _ := f.db.Begin()
	var results []map[string]any
	for _, s := range statements {
		if f.failSQL != "" && strings.Contains(s.SQL, f.failSQL) {
			_ = tx.Rollback()
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]string{{"message": "injected failure"}}, "result": []any{}})
			return
		}
		rows, err := f.run(tx, s)
		if err != nil {
			_ = tx.Rollback()
			_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]string{{"message": err.Error()}}, "result": []any{}})
			return
		}
		results = append(results, map[string]any{"success": true, "results": rows})
	}
	_ = tx.Commit()
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": results})
}

func (f *fakeD1) run(tx *sql.Tx, s fakeStatement) ([]map[string]any, error) {
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(s.SQL)), "SELECT") {
		_, err := tx.Exec(s.SQL, s.Params...)
		return []map[string]any{}, err
	}
	rows, err := tx.Query(s.SQL, s.Params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	out := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, c := range cols {
			if b, ok := values[i].([]byte); ok {
				values[i] = string(b)
			}
			row[c] = values[i]
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func turn(id, query string) domainhistory.Turn {
	return domainhistory.Turn{ID: id, Query: query, Answer: "a-" + query, HasAnswer: true, Generate: true, TopK: 5, Count: 1}
}

func TestEnsureSchemaIsIdempotent(t *testing.T) {
	_, h := newFakeD1(t)
	if err := h.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}

func TestCreateSessionAndList(t *testing.T) {
	_, h := newFakeD1(t)
	ctx := context.Background()
	first, err := h.CreateSession(ctx, "  First  ")
	if err != nil || first.Title != "First" || first.ID == "" {
		t.Fatalf("first = %+v, %v", first, err)
	}
	time.Sleep(2 * time.Millisecond)
	blank, _ := h.CreateSession(ctx, "   ")
	if blank.Title != "New chat" {
		t.Fatalf("blank title = %q", blank.Title)
	}
	sessions, err := h.ListSessions(ctx, 0)
	if err != nil || len(sessions) != 2 || sessions[0].ID != blank.ID || sessions[1].ID != first.ID {
		t.Fatalf("sessions = %+v, %v (want newest first)", sessions, err)
	}
	if limited, _ := h.ListSessions(ctx, 1); len(limited) != 1 {
		t.Fatalf("limit not applied: %+v", limited)
	}
	got, err := h.GetSession(ctx, first.ID)
	if err != nil || got.ID != first.ID || got.TurnCount != 0 || !got.CreatedAt.Equal(first.CreatedAt.Truncate(time.Millisecond)) {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if _, err := h.GetSession(ctx, "missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing err = %v", err)
	}
}

func TestAppendTurnCreatesSessionAndSequencesTurns(t *testing.T) {
	_, h := newFakeD1(t)
	ctx := context.Background()
	long := strings.Repeat("x", titleMaxLen+10)
	if err := h.AppendTurn(ctx, "s1", turn("t0", "q0"), long); err != nil {
		t.Fatal(err)
	}
	if err := h.AppendTurn(ctx, "s1", turn("t1", "q1"), "ignored title"); err != nil {
		t.Fatal(err)
	}
	session, err := h.GetSession(ctx, "s1")
	if err != nil || session.TurnCount != 2 || session.Title != strings.Repeat("x", titleMaxLen)+"…" {
		t.Fatalf("session = %+v, %v", session, err)
	}
	turns, err := h.ListTurns(ctx, "s1")
	if err != nil || len(turns) != 2 || turns[0].Sequence != 0 || turns[1].Sequence != 1 || turns[1].ID != "t1" || turns[1].SessionID != "s1" {
		t.Fatalf("turns = %+v, %v", turns, err)
	}

	if err := h.AppendTurn(ctx, "s1", domainhistory.Turn{Query: "no id"}, ""); err != nil {
		t.Fatal(err)
	}
	turns, _ = h.ListTurns(ctx, "s1")
	if turns[2].ID == "" {
		t.Fatal("a turn without an id must get one")
	}
}

func TestTurnRoundTripPreservesEvidenceSnapshots(t *testing.T) {
	_, h := newFakeD1(t)
	ctx := context.Background()
	original := domainhistory.Turn{
		ID: "stream-1", Query: "what?", RewrittenQuery: "what exactly?", Answer: "Answer [2].", Prompt: "Context",
		Subject:       &domainhistory.Subject{FilePath: "versioned.md", Header: "Transfers > Durable"},
		AttachedFiles: []string{"a.md", "b.md"},
		Results:       []domainhistory.TurnResult{{FilePath: "versioned.md", LineStart: 9, ChunkIndex: 4, Score: 0.5, SourceSHA: "old", Text: "evidence ’ quotes “ and \n newlines"}},
		Citations:     []domainhistory.Citation{{Number: 2, RetrievalRank: 1, FilePath: "versioned.md", Header: "H", LineStart: 9, ChunkIndex: 4, SourceSHA: "old", Text: "t", Truncated: true}},
		TopK:          7, Generate: true, Count: 3, ElapsedMS: 1234, FromCache: true, HasAnswer: true,
		Model: "m", Error: "search failed", GenerateError: "gen failed", Failed: true,
	}
	if err := h.AppendTurn(ctx, "s", original, "title"); err != nil {
		t.Fatal(err)
	}
	turns, err := h.ListTurns(ctx, "s")
	if err != nil || len(turns) != 1 {
		t.Fatalf("turns = %+v, %v", turns, err)
	}
	got := turns[0]
	got.CreatedAt = time.Time{}
	original.SessionID, original.Sequence = "s", 0
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("round trip differs:\n got  %+v\n want %+v", got, original)
	}

	if err := h.AppendTurn(ctx, "s", domainhistory.Turn{ID: "plain", Query: "q"}, ""); err != nil {
		t.Fatal(err)
	}
	turns, _ = h.ListTurns(ctx, "s")
	if turns[1].Subject != nil || turns[1].Failed || turns[1].HasAnswer {
		t.Fatalf("zero-value turn = %+v", turns[1])
	}
}

func TestListTurnsPaginatesInSequenceOrder(t *testing.T) {
	_, h := newFakeD1(t) // page size 2
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := h.AppendTurn(ctx, "s", turn(string(rune('a'+i)), "q"), "t"); err != nil {
			t.Fatal(err)
		}
	}
	turns, err := h.ListTurns(ctx, "s")
	if err != nil || len(turns) != 5 {
		t.Fatalf("turns = %d, %v", len(turns), err)
	}
	for i, tr := range turns {
		if tr.Sequence != i {
			t.Fatalf("turn %d has sequence %d", i, tr.Sequence)
		}
	}
	if other, _ := h.ListTurns(ctx, "other"); len(other) != 0 {
		t.Fatalf("other session turns = %v", other)
	}
}

func TestTruncateSession(t *testing.T) {
	_, h := newFakeD1(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		_ = h.AppendTurn(ctx, "s", turn(string(rune('a'+i)), "q"), "t")
	}
	if err := h.TruncateSession(ctx, "s", 2); err != nil {
		t.Fatal(err)
	}
	turns, _ := h.ListTurns(ctx, "s")
	session, _ := h.GetSession(ctx, "s")
	if len(turns) != 2 || session.TurnCount != 2 {
		t.Fatalf("after truncate: %d turns, count %d", len(turns), session.TurnCount)
	}
	// A replacement turn takes the freed sequence number.
	if err := h.AppendTurn(ctx, "s", turn("z", "q"), "t"); err != nil {
		t.Fatal(err)
	}
	turns, _ = h.ListTurns(ctx, "s")
	if len(turns) != 3 || turns[2].ID != "z" || turns[2].Sequence != 2 {
		t.Fatalf("turns = %+v", turns)
	}

	if err := h.TruncateSession(ctx, "s", 99); err == nil || !strings.Contains(err.Error(), "exceeds 3 turns") {
		t.Fatalf("beyond end err = %v", err)
	}
	if err := h.TruncateSession(ctx, "s", -1); err == nil {
		t.Fatal("negative position must fail")
	}
	if err := h.TruncateSession(ctx, "missing", 0); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing session err = %v", err)
	}
	if err := h.TruncateSession(ctx, "s", 0); err != nil {
		t.Fatal(err)
	}
	if turns, _ := h.ListTurns(ctx, "s"); len(turns) != 0 {
		t.Fatalf("truncate to 0 left %d turns", len(turns))
	}
}

func TestDeleteSessionAndAll(t *testing.T) {
	_, h := newFakeD1(t)
	ctx := context.Background()
	_ = h.AppendTurn(ctx, "keep", turn("k", "q"), "keep")
	_ = h.AppendTurn(ctx, "drop", turn("d", "q"), "drop")
	if err := h.DeleteSession(ctx, "drop"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.GetSession(ctx, "drop"); err == nil {
		t.Fatal("deleted session still exists")
	}
	if turns, _ := h.ListTurns(ctx, "drop"); len(turns) != 0 {
		t.Fatal("deleted session left turns behind")
	}
	if turns, _ := h.ListTurns(ctx, "keep"); len(turns) != 1 {
		t.Fatal("deleting one session removed another's turns")
	}
	if err := h.DeleteSession(ctx, "never-existed"); err != nil {
		t.Fatalf("deleting a missing session must be a no-op: %v", err)
	}
	if err := h.DeleteAllSessions(ctx); err != nil {
		t.Fatal(err)
	}
	if sessions, _ := h.ListSessions(ctx, 0); len(sessions) != 0 {
		t.Fatalf("sessions after delete all = %v", sessions)
	}
	if turns, _ := h.ListTurns(ctx, "keep"); len(turns) != 0 {
		t.Fatalf("turns after delete all = %v", turns)
	}
}

func TestFailedAppendLeavesNoPartialTurn(t *testing.T) {
	f, h := newFakeD1(t)
	ctx := context.Background()
	_ = h.AppendTurn(ctx, "s", turn("t0", "q"), "t")
	f.failSQL = "UPDATE chat_sessions"
	if err := h.AppendTurn(ctx, "s", turn("t1", "q"), "t"); err == nil || !strings.Contains(err.Error(), "injected failure") {
		t.Fatalf("err = %v", err)
	}
	f.failSQL = ""
	session, _ := h.GetSession(ctx, "s")
	turns, _ := h.ListTurns(ctx, "s")
	if len(turns) != 1 || session.TurnCount != 1 {
		t.Fatalf("a failed batch left %d turns (count %d)", len(turns), session.TurnCount)
	}
}

func TestNewDependenciesRequiresExecutor(t *testing.T) {
	if _, err := NewDependencies(DependenciesConfig{}); err == nil {
		t.Fatal("expected error without an executor")
	}
}

package documents

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	indexing "nadir/internal/core/documents/indexing"
	"nadir/internal/providers/cloudflare/d1"
	"nadir/internal/providers/cloudflare/vectorize"
)

// fakeCloud serves D1's query endpoint over a real in-memory SQLite (FTS5
// included) and Vectorize's endpoints over a stateful cosine index, so the
// adapter's SQL and request shapes are executed, not pattern-matched.
type fakeCloud struct {
	mu sync.Mutex

	db *sql.DB

	vectors  map[string]fakeVector
	mutation int
	dims     int
	metric   string
	indexes  []map[string]string

	// Fault injection and observation.
	failSQL       string // D1 statements containing this fail (the batch rolls back)
	failVecPath   string // Vectorize requests whose path ends with this fail
	upsertBatches int
	deleteCalls   int
	d1Calls       int
}

type fakeVector struct {
	values   []float32
	metadata map[string]string
}

func newFakeCloud(t *testing.T) (*fakeCloud, *dependencies) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	f := &fakeCloud{
		db: db, vectors: map[string]fakeVector{}, dims: 3, metric: "cosine",
		indexes: []map[string]string{
			{"propertyName": metaFile, "indexType": "string"},
			{"propertyName": metaHeader, "indexType": "string"},
			{"propertyName": metaSHA, "indexType": "string"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(func() { srv.Close(); _ = db.Close() })

	dbClient, err := d1.New(d1.Config{AccountID: "acct", APIToken: "tok", DatabaseID: "db", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	idx, err := vectorize.New(vectorize.Config{AccountID: "acct", APIToken: "tok", Index: "docs", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewDependencies(DependenciesConfig{DB: dbClient, Index: idx, IndexName: "docs", AdaptiveSignals: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureStore(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	return f, s
}

func (f *fakeCloud) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	if strings.Contains(path, "/d1/database/") {
		f.handleD1(w, r)
		return
	}
	if f.failVecPath != "" && strings.HasSuffix(path, f.failVecPath) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":[{"message":"vectorize down"}]}`))
		return
	}
	ok := func(result any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": result})
	}
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/indexes/docs"):
		ok(map[string]any{"config": map[string]any{"dimensions": f.dims, "metric": f.metric}})
	case strings.HasSuffix(path, "/metadata_index/list"):
		ok(map[string]any{"metadataIndexes": f.indexes})
	case strings.HasSuffix(path, "/info"):
		ok(map[string]any{"processedUpToMutation": fmt.Sprintf("m-%d", f.mutation)})
	case strings.HasSuffix(path, "/upsert"):
		f.upsertBatches++
		scanner := bufio.NewScanner(r.Body)
		scanner.Buffer(make([]byte, 1<<20), 16<<20)
		for scanner.Scan() {
			var line struct {
				ID       string            `json:"id"`
				Values   []float32         `json:"values"`
				Metadata map[string]string `json:"metadata"`
			}
			_ = json.Unmarshal(scanner.Bytes(), &line)
			f.vectors[line.ID] = fakeVector{values: line.Values, metadata: line.Metadata}
		}
		f.mutation++
		ok(map[string]any{"mutationId": fmt.Sprintf("m-%d", f.mutation)})
	case strings.HasSuffix(path, "/delete_by_ids"):
		f.deleteCalls++
		var body struct {
			IDs []string `json:"ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for _, id := range body.IDs {
			delete(f.vectors, id)
		}
		f.mutation++
		ok(map[string]any{"mutationId": fmt.Sprintf("m-%d", f.mutation)})
	case strings.HasSuffix(path, "/query"):
		var body struct {
			Vector []float32         `json:"vector"`
			TopK   int               `json:"topK"`
			Filter map[string]string `json:"filter"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.TopK > maxQueryTopK {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":[{"message":"topK too large"}]}`))
			return
		}
		type match struct {
			ID    string  `json:"id"`
			Score float64 `json:"score"`
		}
		var matches []match
		for id, v := range f.vectors {
			keep := true
			for k, want := range body.Filter {
				if v.metadata[k] != want {
					keep = false
				}
			}
			if keep {
				matches = append(matches, match{id, cosine(body.Vector, v.values)})
			}
		}
		sort.Slice(matches, func(i, j int) bool {
			if matches[i].Score != matches[j].Score {
				return matches[i].Score > matches[j].Score
			}
			return matches[i].ID < matches[j].ID
		})
		if len(matches) > body.TopK {
			matches = matches[:body.TopK]
		}
		ok(map[string]any{"count": len(matches), "matches": matches})
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintf(w, `{"errors":[{"message":"unexpected %s %s"}]}`, r.Method, path)
	}
}

func (f *fakeCloud) handleD1(w http.ResponseWriter, r *http.Request) {
	f.d1Calls++
	var body struct {
		SQL    string `json:"sql"`
		Params []any  `json:"params"`
		Batch  []struct {
			SQL    string `json:"sql"`
			Params []any  `json:"params"`
		} `json:"batch"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	type statement struct {
		sql    string
		params []any
	}
	var statements []statement
	if len(body.Batch) > 0 {
		for _, b := range body.Batch {
			statements = append(statements, statement{b.SQL, b.Params})
		}
	} else {
		statements = []statement{{body.SQL, body.Params}}
	}
	fail := func(msg string) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]string{{"message": msg}}, "result": []any{}})
	}
	tx, _ := f.db.Begin()
	var results []map[string]any
	for _, s := range statements {
		if f.failSQL != "" && strings.Contains(s.sql, f.failSQL) {
			_ = tx.Rollback()
			fail("injected failure")
			return
		}
		if len(s.params) > 100 {
			_ = tx.Rollback()
			fail("too many parameters")
			return
		}
		rows, err := runStatement(tx, s.sql, s.params)
		if err != nil {
			_ = tx.Rollback()
			fail(err.Error())
			return
		}
		results = append(results, map[string]any{"success": true, "results": rows})
	}
	_ = tx.Commit()
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": results})
}

func runStatement(tx *sql.Tx, query string, params []any) ([]map[string]any, error) {
	head := strings.ToUpper(strings.TrimSpace(query))
	if !strings.HasPrefix(head, "SELECT") {
		_, err := tx.Exec(query, params...)
		return []map[string]any{}, err
	}
	rows, err := tx.Query(query, params...)
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

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// count runs a scalar SQL count against the fake database.
func (f *fakeCloud) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var n int
	if err := f.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fakeCloud) vectorCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.vectors)
}

// chunk builds an indexed chunk whose vector points along one axis.
func chunk(path, sha, header, text string, index int, vector []float32) indexing.IndexedChunk {
	return indexing.IndexedChunk{
		FilePath: path, SourceSHA: sha, Header: header, Text: text, WindowText: "window: " + text,
		SectionPath: "root > " + header, LineStart: index * 10, ChunkIndex: index, Vector: vector,
	}
}

// nopDB and nopIndex let a test construct the store without a backend; every
// operation under test fails before it reaches them.
type nopDB struct{}

func (nopDB) Exec(context.Context, ...d1.Statement) ([][]d1.Row, error) {
	return nil, fmt.Errorf("unexpected D1 call")
}

type nopIndex struct{}

func (nopIndex) Describe(context.Context) (vectorize.IndexConfig, error) {
	return vectorize.IndexConfig{}, fmt.Errorf("unexpected call")
}
func (nopIndex) MetadataIndexes(context.Context) ([]vectorize.MetadataIndex, error) {
	return nil, fmt.Errorf("unexpected call")
}
func (nopIndex) QueryWith(context.Context, []float32, vectorize.QueryOptions) ([]vectorize.Match, error) {
	return nil, fmt.Errorf("unexpected call")
}
func (nopIndex) UpsertMany(context.Context, []vectorize.Vector) (string, error) {
	return "", fmt.Errorf("unexpected call")
}
func (nopIndex) DeleteByIDs(context.Context, []string) error { return fmt.Errorf("unexpected call") }
func (nopIndex) WaitForMutation(context.Context, string, time.Duration) (bool, error) {
	return false, fmt.Errorf("unexpected call")
}

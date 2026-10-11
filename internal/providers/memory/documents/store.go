// Package documents is an in-memory document store Adapter. It implements the
// same persistence capability as the Qdrant Adapter (versioned replacement,
// dense and BM25 legs fused with RRF, payload filters) without any server, so
// it serves the browser runtime and acts as a behavioral reference. Snapshots
// let a host persist and restore the corpus.
//
// The dense leg scans every vector (exact) for small corpora and for filtered
// queries, and uses an HNSW index once the corpus reaches Config.IndexThreshold.
package documents

import (
	"cmp"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"sync"
	"unicode"

	"nadir/internal/core/documents/indexing"
	"nadir/internal/core/retrieval/search"
	"nadir/internal/providers/memory/hnsw"
)

const (
	rrfK       = 60
	bm25K1     = 1.2
	bm25B      = 0.75
	snapshotV2 = 2

	// The graph costs seconds per ten thousand chunks to build and is rebuilt on
	// every Restore, so it only pays off where an exact scan is slow.
	defaultIndexThreshold = 20000
	annMinEf              = 64
	annEfConstruction     = 64
	annSeed               = 1
)

// Config tunes candidate depth. PrefetchMul matches the Qdrant adapter: each
// leg contributes topK*PrefetchMul candidates to fusion.
type Config struct {
	PrefetchMul int
	// IndexThreshold is the number of chunks at which dense search switches
	// from an exact scan to an HNSW index. Zero means 20000.
	IndexThreshold int
	// ExactOnly keeps dense search exact regardless of corpus size.
	ExactOnly bool
}

type entry struct {
	id     uint32
	chunk  indexing.IndexedChunk
	tokens map[string]int
	length int
}

// Store holds every document's active chunks. A document is replaced as one
// unit under the lock, so readers never observe a partial version.
type Store struct {
	mu          sync.RWMutex
	prefetchMul int
	dimensions  int
	docs        map[string][]*entry

	threshold int
	exactOnly bool
	nextID    uint32
	live      int
	byID      map[uint32]*entry
	idx       *hnsw.Index // nil until the corpus is large enough
	annOff    bool        // an index insert failed; stay exact until the next reset
}

// New returns an empty store.
func New(cfg Config) *Store {
	if cfg.PrefetchMul <= 0 {
		cfg.PrefetchMul = 4
	}
	if cfg.IndexThreshold <= 0 {
		cfg.IndexThreshold = defaultIndexThreshold
	}
	return &Store{
		prefetchMul: cfg.PrefetchMul,
		threshold:   cfg.IndexThreshold,
		exactOnly:   cfg.ExactOnly,
		docs:        map[string][]*entry{},
		byID:        map[uint32]*entry{},
	}
}

// reset empties the corpus and its index. The caller holds the write lock.
func (s *Store) reset() {
	s.docs = map[string][]*entry{}
	s.byID = map[uint32]*entry{}
	s.live = 0
	s.idx = nil
	s.annOff = false
}

// adopt numbers entries and adds them to the index when there is one.
func (s *Store) adopt(entries []*entry) {
	for _, e := range entries {
		s.nextID++
		e.id = s.nextID
		s.byID[e.id] = e
		s.live++
		if s.idx != nil && s.idx.Add(e.id, e.chunk.Vector) != nil {
			s.idx, s.annOff = nil, true // unusable vector: stay exact
		}
	}
}

// release forgets entries that a replacement or deletion removed.
func (s *Store) release(entries []*entry) {
	for _, e := range entries {
		delete(s.byID, e.id)
		s.live--
		if s.idx != nil {
			s.idx.Remove(e.id)
		}
	}
}

// syncIndex builds the graph once the corpus reaches the threshold, drops it
// when the corpus shrinks well below it, and rebuilds it when removed nodes
// pile up. The caller holds the write lock.
func (s *Store) syncIndex() {
	switch {
	case s.exactOnly, s.annOff:
		return
	case s.idx == nil && s.live >= s.threshold, s.idx != nil && s.idx.NeedsRebuild():
		s.buildIndex()
	case s.idx != nil && s.live < s.threshold/2:
		s.idx = nil
	}
}

func (s *Store) buildIndex() {
	ids := slices.Sorted(maps.Keys(s.byID))
	idx := hnsw.New(hnsw.Config{EfConstruction: annEfConstruction, Seed: annSeed})
	for _, id := range ids {
		if idx.Add(id, s.byID[id].chunk.Vector) != nil {
			s.idx, s.annOff = nil, true
			return
		}
	}
	s.idx = idx
}

// EnsureCollection records the vector size. A store restored with a different
// size is cleared because its vectors are unusable with the new embedder.
func (s *Store) EnsureCollection(_ context.Context, dimensions int) error {
	if dimensions <= 0 {
		return fmt.Errorf("dimensions must be positive, got %d", dimensions)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dimensions != 0 && s.dimensions != dimensions {
		s.reset()
	}
	s.dimensions = dimensions
	return nil
}

func (s *Store) GetAllFileSHAs(context.Context) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	shas := make(map[string]string, len(s.docs))
	for path, entries := range s.docs {
		shas[path] = entries[0].chunk.SourceSHA
	}
	return shas, nil
}

// ReplaceDocument publishes the new version of one file atomically. Empty
// chunk lists are rejected so a failed chunking pass cannot erase a document.
func (s *Store) ReplaceDocument(_ context.Context, filePath, sourceSHA string, chunks []indexing.IndexedChunk) error {
	if len(chunks) == 0 {
		return fmt.Errorf("replace %q: no chunks", filePath)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := make([]*entry, len(chunks))
	for i, c := range chunks {
		if s.dimensions != 0 && len(c.Vector) != s.dimensions {
			return fmt.Errorf("replace %q: chunk %d has %d dimensions, want %d", filePath, i, len(c.Vector), s.dimensions)
		}
		c.FilePath, c.SourceSHA = filePath, sourceSHA
		entries[i] = newEntry(c)
	}
	s.release(s.docs[filePath])
	s.docs[filePath] = entries
	s.adopt(entries)
	s.syncIndex()
	return nil
}

func (s *Store) DeleteDocument(_ context.Context, filePath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.release(s.docs[filePath])
	delete(s.docs, filePath)
	s.syncIndex()
	return nil
}

func (s *Store) DeleteAll(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reset()
	return nil
}

// HybridSearch returns the dense leg, the BM25 leg, and their RRF fusion.
func (s *Store) HybridSearch(_ context.Context, vector []float32, query string, topK int, filter *search.Filter) (search.HybridSearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if topK <= 0 {
		return search.HybridSearchResult{}, nil
	}
	pool := s.filtered(filter)
	fetch := topK * s.prefetchMul

	// Unfiltered queries on a large corpus use the graph; a filter leaves a
	// small pool that an exact scan handles faster and without approximation.
	var dense []scored
	if filter == nil {
		dense = s.annDense(vector, fetch)
	}
	if dense == nil {
		dense = rank(pool, fetch, func(e *entry) float64 { return cosine(vector, e.chunk.Vector) })
	}
	lexical := s.bm25(pool, queryTerms(query), fetch)

	legs := [][]scored{dense}
	if len(lexical) > 0 {
		legs = append(legs, lexical)
	}
	return search.HybridSearchResult{
		Fused:   candidates(fuse(legs), topK),
		Dense:   candidates(dense, topK),
		Lexical: candidates(lexical, topK),
	}, nil
}

// KeywordSearch ranks a keyword-only query with BM25, keeping every term.
func (s *Store) KeywordSearch(_ context.Context, keyword string, topK int, filter *search.Filter) ([]search.SearchCandidate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if topK <= 0 {
		return nil, nil
	}
	return candidates(s.bm25(s.filtered(filter), tokenize(keyword), topK), topK), nil
}

// annDense returns the nearest chunks from the HNSW index, scored with the same
// exact cosine as the scan so both paths report identical scores. It returns
// nil when no index applies.
func (s *Store) annDense(vector []float32, fetch int) []scored {
	if s.idx == nil {
		return nil
	}
	hits := s.idx.Search(vector, fetch, max(annMinEf, fetch))
	if len(hits) == 0 {
		return nil
	}
	out := make([]scored, 0, len(hits))
	for _, h := range hits {
		e := s.byID[h.ID]
		out = append(out, scored{e, cosine(vector, e.chunk.Vector)})
	}
	sortScored(out)
	return out
}

type scored struct {
	entry *entry
	score float64
}

func (s *Store) filtered(f *search.Filter) []*entry {
	var pool []*entry
	for _, entries := range s.docs {
		for _, e := range entries {
			if f != nil {
				if f.FilePath != "" && e.chunk.FilePath != f.FilePath ||
					f.Header != "" && e.chunk.Header != f.Header ||
					f.SourceSHA != "" && e.chunk.SourceSHA != f.SourceSHA {
					continue
				}
			}
			pool = append(pool, e)
		}
	}
	return pool
}

// rank orders by score descending, breaking ties by identity so results are
// deterministic across map iteration order.
func rank(pool []*entry, limit int, score func(*entry) float64) []scored {
	out := make([]scored, 0, len(pool))
	for _, e := range pool {
		out = append(out, scored{e, score(e)})
	}
	sortScored(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func sortScored(items []scored) {
	slices.SortFunc(items, func(x, y scored) int {
		if c := cmp.Compare(y.score, x.score); c != 0 {
			return c
		}
		a, b := x.entry.chunk, y.entry.chunk
		if c := cmp.Compare(a.FilePath, b.FilePath); c != 0 {
			return c
		}
		return cmp.Compare(a.ChunkIndex, b.ChunkIndex)
	})
}

func (s *Store) bm25(pool []*entry, terms []string, limit int) []scored {
	if len(terms) == 0 || len(pool) == 0 {
		return nil
	}
	var totalLen int
	docFreq := make(map[string]int, len(terms))
	for _, e := range pool {
		totalLen += e.length
		for _, t := range terms {
			if e.tokens[t] > 0 {
				docFreq[t]++
			}
		}
	}
	avgLen := float64(totalLen) / float64(len(pool))
	n := float64(len(pool))

	var out []scored
	for _, e := range pool {
		var total float64
		for _, t := range terms {
			tf := float64(e.tokens[t])
			if tf == 0 {
				continue
			}
			df := float64(docFreq[t])
			idf := math.Log(1 + (n-df+0.5)/(df+0.5))
			total += idf * tf * (bm25K1 + 1) / (tf + bm25K1*(1-bm25B+bm25B*float64(e.length)/avgLen))
		}
		if total > 0 {
			out = append(out, scored{e, total})
		}
	}
	sortScored(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// fuse applies reciprocal rank fusion across ranked legs.
func fuse(legs [][]scored) []scored {
	totals := map[*entry]float64{}
	for _, leg := range legs {
		for i, item := range leg {
			totals[item.entry] += 1 / float64(rrfK+i+1)
		}
	}
	out := make([]scored, 0, len(totals))
	for e, score := range totals {
		out = append(out, scored{e, score})
	}
	sortScored(out)
	return out
}

func candidates(items []scored, limit int) []search.SearchCandidate {
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]search.SearchCandidate, len(items))
	for i, item := range items {
		c := item.entry.chunk
		out[i] = search.SearchCandidate{
			Text: c.Text, WindowText: c.WindowText, FilePath: c.FilePath, Header: c.Header,
			SectionPath: c.SectionPath, LineStart: c.LineStart, ChunkIndex: c.ChunkIndex,
			SourceSHA: c.SourceSHA, Score: float32(item.score),
		}
	}
	return out
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return math.Inf(-1)
	}
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

func newEntry(c indexing.IndexedChunk) *entry {
	source := c.SparseText
	if source == "" {
		source = contextualText(c.FilePath, c.Header, c.Text)
	}
	terms := tokenize(source)
	counts := make(map[string]int, len(terms))
	for _, t := range terms {
		counts[t]++
	}
	return &entry{chunk: c, tokens: counts, length: len(terms)}
}

// contextualText mirrors the chunker's ContextualText so the lexical leg sees
// the same context the dense leg embeds.
func contextualText(filePath, header, text string) string {
	var sb strings.Builder
	sb.WriteString(filePath)
	if header != "" {
		sb.WriteString(" > ")
		sb.WriteString(header)
	}
	sb.WriteString("\n")
	sb.WriteString(text)
	return sb.String()
}

func tokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// queryTerms drops common English function words so natural questions are
// ranked on their topic words, as the Qdrant adapter does.
func queryTerms(query string) []string {
	var terms []string
	for _, term := range tokenize(query) {
		switch term {
		case "a", "an", "the", "i", "it", "its", "we", "our", "my", "me", "you", "your",
			"what", "which", "how", "why", "do", "does", "did", "is", "are", "was", "were",
			"can", "could", "should", "would", "to", "of", "for", "from", "at", "on", "in", "and", "or", "with", "while", "when":
			continue
		}
		terms = append(terms, term)
	}
	return terms
}

// snapshotChunk is the persisted form of a chunk. Vectors are stored as
// little-endian float32 bytes (base64 in JSON), about a third the size of a
// decimal number array and much faster to encode.
type snapshotChunk struct {
	indexing.IndexedChunk
	Vector []byte `json:"Vector"`
}

type snapshot struct {
	Version    int             `json:"version"`
	Dimensions int             `json:"dimensions"`
	Chunks     []snapshotChunk `json:"chunks"`
}

func encodeVector(v []float32) []byte {
	out, _ := binary.Append(nil, binary.LittleEndian, v) // fixed-size values cannot fail
	return out
}

func decodeVector(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("vector has %d bytes, not a multiple of 4", len(b))
	}
	out := make([]float32, len(b)/4)
	if _, err := binary.Decode(b, binary.LittleEndian, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Snapshot serializes the corpus so a host can persist it.
func (s *Store) Snapshot() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := snapshot{Version: snapshotV2, Dimensions: s.dimensions}
	for _, path := range slices.Sorted(maps.Keys(s.docs)) {
		for _, e := range s.docs[path] {
			chunk := snapshotChunk{IndexedChunk: e.chunk, Vector: encodeVector(e.chunk.Vector)}
			chunk.IndexedChunk.Vector = nil
			snap.Chunks = append(snap.Chunks, chunk)
		}
	}
	return json.Marshal(snap)
}

// Restore replaces the corpus with a snapshot taken by Snapshot.
func (s *Store) Restore(data []byte) error {
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("decode snapshot: %w", err)
	}
	if snap.Version != snapshotV2 {
		return fmt.Errorf("unsupported snapshot version %d", snap.Version)
	}
	docs := map[string][]*entry{}
	var all []*entry
	for _, c := range snap.Chunks {
		vector, err := decodeVector(c.Vector)
		if err != nil {
			return fmt.Errorf("snapshot chunk %s#%d: %w", c.FilePath, c.ChunkIndex, err)
		}
		if snap.Dimensions != 0 && len(vector) != snap.Dimensions {
			return fmt.Errorf("snapshot chunk %s#%d has %d dimensions, want %d", c.FilePath, c.ChunkIndex, len(vector), snap.Dimensions)
		}
		chunk := c.IndexedChunk
		chunk.Vector = vector
		e := newEntry(chunk)
		docs[chunk.FilePath] = append(docs[chunk.FilePath], e)
		all = append(all, e)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reset()
	s.docs, s.dimensions = docs, snap.Dimensions
	s.adopt(all)
	s.syncIndex()
	return nil
}

package store

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"nadir/internal/adapters/qdrant/shared"
	"nadir/internal/knowledge/indexing"
	"nadir/internal/retrieval/search"

	qdrant "github.com/qdrant/go-client/qdrant"
)

func TestNewDependenciesRequiresSharedClients(t *testing.T) {
	if _, err := NewDependencies(DependenciesConfig{}); err == nil {
		t.Fatal("NewDependencies accepted an empty Qdrant client set")
	}
}

func TestTokenizeAndVectorizeSparseAreStableByTermFrequency(t *testing.T) {
	if got := tokenize("Power-rule, POWER rule! x²"); len(got) != 5 {
		t.Fatalf("tokenize() = %#v, want punctuation-free lowercase terms", got)
	}
	indices, values := vectorizeSparse("Power power rule")
	if len(indices) != 2 || len(values) != 2 {
		t.Fatalf("vectorizeSparse() = %v/%v, want two unique terms", indices, values)
	}
	freq := make(map[uint32]float32, len(indices))
	for i, index := range indices {
		freq[index] = values[i]
	}
	for _, term := range []struct {
		text string
		want float32
	}{
		{text: "power", want: 2},
		{text: "rule", want: 1},
	} {
		indices, _ := vectorizeSparse(term.text)
		if len(indices) != 1 || freq[indices[0]] != term.want {
			t.Fatalf("term %q frequency = %v, want %v", term.text, freq[indices[0]], term.want)
		}
	}
}

func TestStorePayloadAndPointIdentity(t *testing.T) {
	chunk := indexing.IndexedChunk{Text: "text", WindowText: "window", FilePath: "doc.md", Header: "Intro", LineStart: 4, ChunkIndex: 2, SourceSHA: "sha", IngestedAt: "time"}
	payload := map[string]*qdrant.Value{
		"text":        qdrantutil.StringValue(chunk.Text),
		"window_text": qdrantutil.StringValue(chunk.WindowText),
		"file_path":   qdrantutil.StringValue(chunk.FilePath),
		"header":      qdrantutil.StringValue(chunk.Header),
		"line_start":  qdrantutil.IntValue(int64(chunk.LineStart)),
		"chunk_index": qdrantutil.IntValue(int64(chunk.ChunkIndex)),
		"source_sha":  qdrantutil.StringValue(chunk.SourceSHA),
		"ingested_at": qdrantutil.StringValue(chunk.IngestedAt),
	}
	got := chunkFromPayload(payload)
	want := search.SearchCandidate{Text: chunk.Text, WindowText: chunk.WindowText, FilePath: chunk.FilePath, Header: chunk.Header, LineStart: chunk.LineStart, ChunkIndex: chunk.ChunkIndex, SourceSHA: chunk.SourceSHA}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("chunkFromPayload() = %+v, want %+v", got, chunk)
	}
	if pointID(chunk) != pointID(chunk) {
		t.Fatal("pointID is not deterministic")
	}
	otherVersion := chunk
	otherVersion.SourceSHA = "another-sha"
	if pointID(chunk) == pointID(otherVersion) {
		t.Fatal("different document versions must have distinct point IDs")
	}
	hype := chunk
	hype.HypeQuestion = "what is intro?"
	hype.HypeIndex = 1
	if pointID(chunk) == pointID(hype) {
		t.Fatal("HyPE sibling must have a distinct point ID")
	}
}

func TestBuildFilterConditions(t *testing.T) {
	conds := buildFilterConditions(&search.Filter{FilePath: "doc.md", Header: "Intro", SourceSHA: "sha"})
	if len(conds) != 3 {
		t.Fatalf("buildFilterConditions() returned %d conditions, want 3", len(conds))
	}
	if toQdrantFilter(nil) == nil || toQdrantFilter(conds) == nil {
		t.Fatal("toQdrantFilter() must always exclude inactive points")
	}
	if len(toQdrantFilter(nil).MustNot) != 1 {
		t.Fatal("toQdrantFilter() is missing the inactive-point guard")
	}
}

func TestReplaceDocumentValidatesVersionIdentity(t *testing.T) {
	s := &dependencies{}
	tests := []struct {
		name  string
		path  string
		sha   string
		chunk indexing.IndexedChunk
		want  string
	}{
		{name: "missing path", sha: "sha", want: "file path is required"},
		{name: "missing sha", path: "doc.md", want: "source SHA is required"},
		{name: "chunk path mismatch", path: "doc.md", sha: "sha", chunk: indexing.IndexedChunk{FilePath: "other.md", SourceSHA: "sha"}, want: "does not match \"doc.md\""},
		{name: "chunk sha mismatch", path: "doc.md", sha: "sha", chunk: indexing.IndexedChunk{FilePath: "doc.md", SourceSHA: "other"}, want: "does not match \"sha\""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.ReplaceDocument(context.Background(), tt.path, tt.sha, []indexing.IndexedChunk{tt.chunk})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ReplaceDocument() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

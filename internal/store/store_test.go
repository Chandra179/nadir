package store

import (
	"reflect"
	"testing"

	"nadir/internal/qdrantutil"

	qdrant "github.com/qdrant/go-client/qdrant"
)

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
	chunk := ScoredChunk{Text: "text", WindowText: "window", FilePath: "doc.md", Header: "Intro", LineStart: 4, ChunkIndex: 2, SourceSHA: "sha", IngestedAt: "time"}
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
	if !reflect.DeepEqual(got, chunk) {
		t.Fatalf("chunkFromPayload() = %+v, want %+v", got, chunk)
	}
	if pointID(chunk) != pointID(chunk) {
		t.Fatal("pointID is not deterministic")
	}
	hype := chunk
	hype.HypeQuestion = "what is intro?"
	hype.HypeIndex = 1
	if pointID(chunk) == pointID(hype) {
		t.Fatal("HyPE sibling must have a distinct point ID")
	}
}

func TestBuildFilterConditions(t *testing.T) {
	conds := buildFilterConditions(&SearchFilter{FilePath: "doc.md", Header: "Intro", SourceSHA: "sha"})
	if len(conds) != 3 {
		t.Fatalf("buildFilterConditions() returned %d conditions, want 3", len(conds))
	}
	if toQdrantFilter(nil) != nil || toQdrantFilter(conds) == nil {
		t.Fatal("toQdrantFilter() nil handling is incorrect")
	}
}

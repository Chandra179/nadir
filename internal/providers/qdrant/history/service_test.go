package history

import (
	"reflect"
	"testing"
	"time"

	qdrant "github.com/qdrant/go-client/qdrant"
	domainhistory "nadir/internal/core/conversation/history"
	qdrantutil "nadir/internal/providers/qdrant/shared"
)

func TestTurnFromPayloadPreservesStreamID(t *testing.T) {
	turn, err := turnFromPayload("point-id", map[string]*qdrant.Value{
		"turn_id":    qdrantutil.StringValue("stream-id"),
		"session_id": qdrantutil.StringValue("session-id"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if turn.ID != "stream-id" {
		t.Fatalf("turn ID = %q, want persisted stream ID", turn.ID)
	}
}

func TestTurnPayloadRoundTripPreservesEvidenceSnapshots(t *testing.T) {
	original := Turn{ID: "stream-1", Query: "what?", Answer: "Answer [2].", Prompt: "Context with stable numbers",
		Results: []TurnResult{{FilePath: "versioned.md", LineStart: 9, ChunkIndex: 4, SourceSHA: "old-sha", Text: "full evidence"}},
		Citations: []domainhistory.Citation{{Number: 2, RetrievalRank: 1, FilePath: "versioned.md", Header: "Heading", LineStart: 9, ChunkIndex: 4,
			SourceSHA: "old-sha", Text: "exact admitted evidence\n[truncated]", Truncated: true}},
	}
	payload, err := turnPayload("session-1", 3, time.Unix(0, 0), original)
	if err != nil {
		t.Fatal(err)
	}
	got, err := turnFromPayload("point-1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Results, original.Results) || !reflect.DeepEqual(got.Citations, original.Citations) || got.Prompt != original.Prompt || got.Answer != original.Answer {
		t.Fatalf("history round trip lost provenance: %+v", got)
	}
	delete(payload, "citations_json")
	legacy, err := turnFromPayload("point-1", payload)
	if err != nil || len(legacy.Citations) != 0 || !reflect.DeepEqual(legacy.Results, original.Results) {
		t.Fatalf("legacy read invented or lost evidence: %+v err=%v", legacy, err)
	}
}

func TestTurnFromPayloadFallsBackForLegacyRecords(t *testing.T) {
	turn, err := turnFromPayload("legacy-point", nil)
	if err != nil {
		t.Fatal(err)
	}
	if turn.ID != "legacy-point" {
		t.Fatalf("legacy turn ID = %q, want point ID fallback", turn.ID)
	}
}

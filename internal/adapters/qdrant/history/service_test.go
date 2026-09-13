package history

import (
	"testing"

	qdrant "github.com/qdrant/go-client/qdrant"
	qdrantutil "nadir/internal/adapters/qdrant/shared"
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

func TestTurnFromPayloadFallsBackForLegacyRecords(t *testing.T) {
	turn, err := turnFromPayload("legacy-point", nil)
	if err != nil {
		t.Fatal(err)
	}
	if turn.ID != "legacy-point" {
		t.Fatalf("legacy turn ID = %q, want point ID fallback", turn.ID)
	}
}

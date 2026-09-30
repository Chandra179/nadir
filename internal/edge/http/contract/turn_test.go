package contract

import (
	"encoding/json"
	"reflect"
	"testing"

	"nadir/internal/core/conversation/chat"
	"nadir/internal/core/conversation/history"
	"nadir/internal/core/retrieval/search"
)

func TestLiveAndSavedTurnContractsPreserveCitationIdentity(t *testing.T) {
	citation := chat.Citation{Number: 2, RetrievalRank: 1, FilePath: "snapshot.md", Header: "Formula", LineStart: 12,
		ChunkIndex: 7, SourceSHA: "version-1", Text: "Admitted evidence\n[truncated]", Truncated: true}
	live := FromChatTurn(chat.Request{Query: "what?", TopK: 5}, chat.Turn{
		Query: "what?", Chunks: []search.Chunk{{FilePath: "snapshot.md", LineStart: 12, ChunkIndex: 7, Text: "Full evidence"}}, Citations: []chat.Citation{citation},
	})
	saved := FromHistoryTurn(history.Turn{Query: "what?", Results: []history.TurnResult{{FilePath: "snapshot.md", LineStart: 12, ChunkIndex: 7, Text: "Full evidence"}}, Citations: []history.Citation{{
		Number: citation.Number, RetrievalRank: citation.RetrievalRank, FilePath: citation.FilePath, Header: citation.Header,
		LineStart: citation.LineStart, ChunkIndex: citation.ChunkIndex, SourceSHA: citation.SourceSHA, Text: citation.Text, Truncated: citation.Truncated,
	}}})
	if !reflect.DeepEqual(live.Citations, saved.Citations) || !reflect.DeepEqual(live.Results, saved.Results) {
		t.Fatalf("live/saved contract drift: live=%+v saved=%+v", live, saved)
	}
	raw, err := json.Marshal(live)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Results []struct {
			ChunkIndex int `json:"chunk_index"`
			Rank       int `json:"retrieval_rank"`
		} `json:"results"`
		Citations []struct {
			Number int    `json:"number"`
			Rank   int    `json:"retrieval_rank"`
			Text   string `json:"text"`
			SHA    string `json:"source_sha"`
		} `json:"citations"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Citations) != 1 || payload.Citations[0].Number != 2 || payload.Citations[0].Rank != 1 || payload.Citations[0].Text != citation.Text || payload.Citations[0].SHA != "version-1" || payload.Results[0].ChunkIndex != 7 || payload.Results[0].Rank != 1 {
		t.Fatalf("wire provenance missing: %s", raw)
	}
}

func TestLegacyHistoryContractDoesNotInventCitationMap(t *testing.T) {
	response := FromHistoryTurn(history.Turn{Answer: "Saved answer [1].", Results: []history.TurnResult{{FilePath: "old.md", LineStart: 4, Text: "old evidence"}}})
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if _, exists := payload["citations"]; exists || response.Results[0].RetrievalRank != 1 {
		t.Fatalf("legacy contract invented citation metadata: %s", raw)
	}
}

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"nadir/internal/core/documents/indexing"

	qdrant "github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc"
)

type replacementRetryPoints struct {
	qdrant.PointsClient
	active          map[string]bool
	activationCalls int
	stageCalls      int
	countErr        error
	countFilter     *qdrant.Filter
	countStarted    chan struct{}
	stageStarted    chan struct{}
	stageRelease    chan struct{}
	deactivationErr error
}

func (p *replacementRetryPoints) Upsert(_ context.Context, request *qdrant.UpsertPoints, _ ...grpc.CallOption) (*qdrant.PointsOperationResponse, error) {
	p.stageCalls++
	if p.stageStarted != nil && p.stageCalls == 1 {
		close(p.stageStarted)
		<-p.stageRelease
	}
	for _, point := range request.Points {
		p.active[point.Payload["source_sha"].GetStringValue()] = point.Payload["active"].GetBoolValue()
	}
	return &qdrant.PointsOperationResponse{}, nil
}

func (p *replacementRetryPoints) Count(_ context.Context, request *qdrant.CountPoints, _ ...grpc.CallOption) (*qdrant.CountResponse, error) {
	p.countFilter = request.Filter
	if p.countStarted != nil {
		p.countStarted <- struct{}{}
	}
	if p.countErr != nil {
		return nil, p.countErr
	}
	var count uint64
	for sha, active := range p.active {
		if active && replacementMatchesVersion(request.Filter, sha) {
			count++
		}
	}
	return &qdrant.CountResponse{Result: &qdrant.CountResult{Count: count}}, nil
}

func TestReplacementOnlyTreatsReadableVersionAsPublished(t *testing.T) {
	points := &replacementRetryPoints{active: map[string]bool{"old": true, "new": false}}
	store := &dependencies{points: points, activeAlias: "docs__active"}
	chunks := []indexing.IndexedChunk{{FilePath: "a.md", SourceSHA: "new", Vector: []float32{1}}}
	err := store.ReplaceDocument(context.Background(), "a.md", "new", chunks)
	if !indexing.WasPublished(err) || !points.active["new"] || points.stageCalls != 1 {
		t.Fatalf("inactive staging was mistaken for a published version: active=%v stages=%d err=%v", points.active, points.stageCalls, err)
	}
	filter := points.countFilter
	if len(filter.Must) != 2 || len(filter.MustNot) != 1 ||
		filter.MustNot[0].GetField().GetKey() != "active" || filter.MustNot[0].GetField().GetMatch().GetBoolean() {
		t.Fatalf("published count must match path/SHA and exclude inactive points: %+v", filter)
	}
}

func TestReplacementReadFailureNeverRestagesVisiblePoints(t *testing.T) {
	points := &replacementRetryPoints{active: map[string]bool{"new": true}, countErr: errors.New("count unavailable")}
	store := &dependencies{points: points, activeAlias: "docs__active"}
	err := store.ReplaceDocument(context.Background(), "a.md", "new", []indexing.IndexedChunk{{FilePath: "a.md", SourceSHA: "new"}})
	if err == nil || indexing.WasPublished(err) || points.stageCalls != 0 || points.activationCalls != 0 || !points.active["new"] {
		t.Fatalf("failed published-version read mutated visibility: active=%v err=%v", points.active, err)
	}
}

func TestEmptyReplacementPublishesAbsenceWithoutActivatingStaging(t *testing.T) {
	points := &replacementRetryPoints{active: map[string]bool{"old": true, "new": false}}
	store := &dependencies{points: points, activeAlias: "docs__active"}
	for attempt := 0; attempt < 2; attempt++ {
		err := store.ReplaceDocument(context.Background(), "a.md", "new", nil)
		if !indexing.WasPublished(err) || points.active["old"] || points.active["new"] || points.activationCalls != 0 || points.stageCalls != 0 {
			t.Fatalf("empty replacement exposed content on attempt %d: active=%v err=%v", attempt+1, points.active, err)
		}
	}
}

func TestConcurrentReplacementCannotRestagePublishedVersion(t *testing.T) {
	points := &replacementRetryPoints{
		active: map[string]bool{"old": true}, countStarted: make(chan struct{}, 2),
		stageStarted: make(chan struct{}), stageRelease: make(chan struct{}),
	}
	store := &dependencies{points: points, activeAlias: "docs__active"}
	chunks := []indexing.IndexedChunk{{FilePath: "a.md", SourceSHA: "new", Vector: []float32{1}}}
	done := make(chan error, 2)
	go func() { done <- store.ReplaceDocument(context.Background(), "a.md", "new", chunks) }()
	select {
	case <-points.stageStarted:
	case <-time.After(time.Second):
		t.Fatal("replacement did not reach staging")
	}
	<-points.countStarted
	go func() { done <- store.ReplaceDocument(context.Background(), "a.md", "new", chunks) }()
	select {
	case <-points.countStarted:
		close(points.stageRelease)
		t.Fatal("concurrent replacement read publication state before the first commit finished")
	case <-time.After(25 * time.Millisecond):
	}
	close(points.stageRelease)
	for range 2 {
		select {
		case err := <-done:
			if !indexing.WasPublished(err) {
				t.Fatalf("replacement lost published outcome: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("replacement did not finish")
		}
	}
	if points.stageCalls != 1 || points.activationCalls != 1 || !points.active["new"] || points.active["old"] {
		t.Fatalf("concurrent retry restaged publication: stages=%d activations=%d active=%v", points.stageCalls, points.activationCalls, points.active)
	}
}

func (p *replacementRetryPoints) SetPayload(_ context.Context, request *qdrant.SetPayloadPoints, _ ...grpc.CallOption) (*qdrant.PointsOperationResponse, error) {
	active := request.Payload["active"].GetBoolValue()
	if active {
		p.activationCalls++
		if p.activationCalls == 2 {
			return nil, errors.New("activation unavailable on retry")
		}
	} else if p.deactivationErr != nil {
		return nil, p.deactivationErr
	}
	for sha := range p.active {
		if replacementMatchesVersion(request.PointsSelector.GetFilter(), sha) {
			p.active[sha] = active
		}
	}
	return &qdrant.PointsOperationResponse{}, nil
}

func TestReplacementDeactivationFailureReportsActualPublication(t *testing.T) {
	for _, empty := range []bool{false, true} {
		points := &replacementRetryPoints{active: map[string]bool{"old": true}, deactivationErr: errors.New("deactivation unavailable")}
		store := &dependencies{points: points, activeAlias: "docs__active"}
		var chunks []indexing.IndexedChunk
		if !empty {
			chunks = []indexing.IndexedChunk{{FilePath: "a.md", SourceSHA: "new", Vector: []float32{1}}}
		}
		err := store.ReplaceDocument(context.Background(), "a.md", "new", chunks)
		if !errors.Is(err, points.deactivationErr) || indexing.WasPublished(err) != !empty || !points.active["old"] {
			t.Fatalf("empty=%v publication outcome=%v active=%v", empty, err, points.active)
		}
	}
}

func TestReplacementRejectsChangingAnImmutablePublishedVersion(t *testing.T) {
	points := &replacementRetryPoints{active: map[string]bool{"new": true}}
	store := &dependencies{points: points, activeAlias: "docs__active"}
	chunks := []indexing.IndexedChunk{
		{FilePath: "a.md", SourceSHA: "new", ChunkIndex: 0},
		{FilePath: "a.md", SourceSHA: "new", ChunkIndex: 1},
	}
	err := store.ReplaceDocument(context.Background(), "a.md", "new", chunks)
	if err == nil || points.stageCalls != 0 || points.activationCalls != 0 || !points.active["new"] {
		t.Fatalf("changed chunk plan overwrote the published version: err=%v active=%v", err, points.active)
	}
}

func replacementMatchesVersion(filter *qdrant.Filter, sha string) bool {
	for _, condition := range filter.Must {
		if field := condition.GetField(); field.GetKey() == "source_sha" && field.GetMatch().GetKeyword() != sha {
			return false
		}
	}
	for _, condition := range filter.MustNot {
		if field := condition.GetField(); field.GetKey() == "source_sha" && field.GetMatch().GetKeyword() == sha {
			return false
		}
	}
	return true
}

func (p *replacementRetryPoints) Delete(context.Context, *qdrant.DeletePoints, ...grpc.CallOption) (*qdrant.PointsOperationResponse, error) {
	return nil, errors.New("cleanup unavailable")
}

func TestReplacementRetryPreservesPublishedVersion(t *testing.T) {
	points := &replacementRetryPoints{active: map[string]bool{"old": true}}
	store := &dependencies{points: points, activeAlias: "docs__active"}
	chunks := []indexing.IndexedChunk{{FilePath: "a.md", SourceSHA: "new", Text: "new", Vector: []float32{1}}}
	for attempt := 0; attempt < 2; attempt++ {
		if err := store.ReplaceDocument(context.Background(), "a.md", "new", chunks); err == nil || !indexing.WasPublished(err) {
			t.Fatalf("cleanup failure did not expose the published outcome: %v", err)
		}
		if points.active["old"] || !points.active["new"] {
			t.Fatalf("attempt %d removed the visible document: %v", attempt+1, points.active)
		}
	}
	if points.stageCalls != 1 || points.activationCalls != 1 {
		t.Fatalf("published version was restaged: stage=%d activation=%d", points.stageCalls, points.activationCalls)
	}
}

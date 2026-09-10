package history

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	qdrant "github.com/qdrant/go-client/qdrant"

	"nadir/internal/qdrantutil"
)

// Session is one persisted chat conversation.
type Session struct {
	ID        string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
	TurnCount int
}

// TurnResult is a snapshot of one retrieved chunk as it was shown to the
// user — captured at write time rather than referenced by pointer, since
// source documents can be re-ingested or deleted after the fact.
type TurnResult struct {
	FilePath  string
	Header    string
	LineStart int
	Score     float32
	Text      string
	SourceSHA string
}

// Turn is one question/answer exchange within a session.
type Turn struct {
	ID             string
	SessionID      string
	Sequence       int
	CreatedAt      time.Time
	Query          string
	RewrittenQuery string
	AttachedFiles  []string
	TopK           int
	Generate       bool
	Results        []TurnResult
	Count          int
	ElapsedMS      int64
	FromCache      bool
	Prompt         string
	Answer         string
	HasAnswer      bool
	Model          string
	// Error is set when the search stage itself failed.
	Error string
	// GenerateError is set when search succeeded but generation failed —
	// distinct from Error so a partial (search-only) result isn't confused
	// with a fully failed turn.
	GenerateError string
	Failed        bool
}

// TruncateSession replaces the tail of an existing conversation in place.
// The turn at beforeSequence is removed along with all later turns; the
// caller appends the replacement after retrieval completes.
func (d *dependencies) TruncateSession(ctx context.Context, sessionID string, beforeSequence int) error {
	if beforeSequence < 0 {
		return fmt.Errorf("history: invalid edit position: %d", beforeSequence)
	}

	unlock := d.lockWrites()
	defer unlock()

	session, err := d.GetSession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("history: edit session: %w", err)
	}
	if beforeSequence > session.TurnCount {
		return fmt.Errorf("history: edit position %d exceeds %d turns", beforeSequence, session.TurnCount)
	}

	wait := true
	sequence := float64(beforeSequence)
	if _, err := d.points.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: d.name,
		Wait:           &wait,
		Points: qdrant.NewPointsSelectorFilter(&qdrant.Filter{Must: []*qdrant.Condition{
			matchKeyword("doc_type", docTypeTurn),
			matchKeyword("session_id", sessionID),
			qdrant.NewRange("sequence", &qdrant.Range{Gte: &sequence}),
		}}),
	}); err != nil {
		return fmt.Errorf("history: delete edited turns: %w", err)
	}

	now := time.Now().UTC()
	if _, err := d.points.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName: d.name,
		Wait:           &wait,
		Payload: map[string]*qdrant.Value{
			"updated_at": qdrantutil.IntValue(now.UnixMilli()),
			"turn_count": qdrantutil.IntValue(int64(beforeSequence)),
		},
		PointsSelector: qdrant.NewPointsSelector(qdrant.NewIDUUID(sessionID)),
	}); err != nil {
		return fmt.Errorf("history: update edited session: %w", err)
	}
	return nil
}

// CreateSession creates a persisted chat session.
func (d *dependencies) CreateSession(ctx context.Context, title string) (Session, error) {
	title = truncateTitle(title)
	now := time.Now().UTC()
	id := uuid.NewString()

	vec, err := d.embedder.Embed(ctx, title)
	if err != nil {
		return Session{}, fmt.Errorf("history: embed session title: %w", err)
	}

	_, err = d.points.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: d.name,
		Points: []*qdrant.PointStruct{{
			Id:      qdrant.NewIDUUID(id),
			Vectors: qdrant.NewVectors(vec...),
			Payload: sessionPayload(title, now, now, 0),
		}},
	})
	if err != nil {
		return Session{}, fmt.Errorf("history: create session: %w", err)
	}
	return Session{ID: id, Title: title, CreatedAt: now, UpdatedAt: now}, nil
}

// AppendTurn creates the session on the fly (firstTurnTitle) if it doesn't
// exist yet. Session turn_count/updated_at are updated via SetPayload so the
// session's vector never needs re-supplying.
func (d *dependencies) AppendTurn(ctx context.Context, sessionID string, turn Turn, firstTurnTitle string) error {
	unlock := d.lockWrites()
	defer unlock()
	now := time.Now().UTC()

	session, err := d.GetSession(ctx, sessionID)
	if err != nil {
		session = Session{ID: sessionID, Title: truncateTitle(firstTurnTitle), CreatedAt: now}

		vec, embErr := d.embedder.Embed(ctx, session.Title)
		if embErr != nil {
			return fmt.Errorf("history: embed session title: %w", embErr)
		}
		if _, err := d.points.Upsert(ctx, &qdrant.UpsertPoints{
			CollectionName: d.name,
			Points: []*qdrant.PointStruct{{
				Id:      qdrant.NewIDUUID(sessionID),
				Vectors: qdrant.NewVectors(vec...),
				Payload: sessionPayload(session.Title, now, now, 0),
			}},
		}); err != nil {
			return fmt.Errorf("history: create session on append: %w", err)
		}
	}

	turnID := uuid.NewString()
	vec, err := d.embedder.Embed(ctx, turn.Query)
	if err != nil {
		return fmt.Errorf("history: embed turn query: %w", err)
	}

	payload, err := turnPayload(sessionID, session.TurnCount, now, turn)
	if err != nil {
		return err
	}

	if _, err := d.points.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: d.name,
		Points: []*qdrant.PointStruct{{
			Id:      qdrant.NewIDUUID(turnID),
			Vectors: qdrant.NewVectors(vec...),
			Payload: payload,
		}},
	}); err != nil {
		return fmt.Errorf("history: create turn: %w", err)
	}

	wait := true
	if _, err := d.points.SetPayload(ctx, &qdrant.SetPayloadPoints{
		CollectionName: d.name,
		Wait:           &wait,
		Payload: map[string]*qdrant.Value{
			"updated_at": qdrantutil.IntValue(now.UnixMilli()),
			"turn_count": qdrantutil.IntValue(int64(session.TurnCount + 1)),
		},
		PointsSelector: qdrant.NewPointsSelector(qdrant.NewIDUUID(sessionID)),
	}); err != nil {
		return fmt.Errorf("history: update session: %w", err)
	}
	return nil
}

func (d *dependencies) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = defaultListLimit
	}
	l := uint32(limit)
	resp, err := d.points.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: d.name,
		Filter:         docTypeFilter(docTypeSession),
		Limit:          &l,
		WithPayload:    qdrant.NewWithPayload(true),
		WithVectors:    qdrant.NewWithVectors(false),
		OrderBy: &qdrant.OrderBy{
			Key:       "updated_at",
			Direction: qdrant.Direction_Desc.Enum(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("history: list sessions: %w", err)
	}

	out := make([]Session, len(resp.Result))
	for i, pt := range resp.Result {
		out[i] = sessionFromPayload(qdrantutil.PointIDString(pt.Id), pt.Payload)
	}
	return out, nil
}

func (d *dependencies) GetSession(ctx context.Context, sessionID string) (Session, error) {
	resp, err := d.points.Get(ctx, &qdrant.GetPoints{
		CollectionName: d.name,
		Ids:            []*qdrant.PointId{qdrant.NewIDUUID(sessionID)},
		WithPayload:    qdrant.NewWithPayload(true),
	})
	if err != nil {
		return Session{}, fmt.Errorf("history: get session: %w", err)
	}
	if len(resp.Result) == 0 {
		return Session{}, fmt.Errorf("history: session not found: %s", sessionID)
	}
	return sessionFromPayload(sessionID, resp.Result[0].Payload), nil
}

func (d *dependencies) ListTurns(ctx context.Context, sessionID string) ([]Turn, error) {
	const pageSize = uint32(500)
	var offset *qdrant.PointId
	var out []Turn
	for {
		resp, err := d.points.Scroll(ctx, &qdrant.ScrollPoints{
			CollectionName: d.name,
			Filter: &qdrant.Filter{
				Must: []*qdrant.Condition{
					matchKeyword("doc_type", docTypeTurn),
					matchKeyword("session_id", sessionID),
				},
			},
			Limit:       new(uint32(pageSize)),
			Offset:      offset,
			WithPayload: qdrant.NewWithPayload(true),
			WithVectors: qdrant.NewWithVectors(false),
			OrderBy: &qdrant.OrderBy{
				Key:       "sequence",
				Direction: qdrant.Direction_Asc.Enum(),
			},
		})
		if err != nil {
			return nil, fmt.Errorf("history: list turns: %w", err)
		}
		for _, pt := range resp.Result {
			turn, err := turnFromPayload(qdrantutil.PointIDString(pt.Id), pt.Payload)
			if err != nil {
				return nil, err
			}
			out = append(out, turn)
		}
		if resp.NextPageOffset == nil {
			break
		}
		offset = resp.NextPageOffset
	}
	return out, nil
}

// DeleteSession removes a session and all of its turns. Turns go first so
// a failure can't leave orphaned turns pointing at a missing session; the
// turn side is a filtered delete (session_id index) rather than fetching
// IDs up front.
func (d *dependencies) DeleteSession(ctx context.Context, sessionID string) error {
	unlock := d.lockWrites()
	defer unlock()
	wait := true
	if _, err := d.points.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: d.name,
		Wait:           &wait,
		Points: qdrant.NewPointsSelectorFilter(&qdrant.Filter{
			Must: []*qdrant.Condition{
				matchKeyword("doc_type", docTypeTurn),
				matchKeyword("session_id", sessionID),
			},
		}),
	}); err != nil {
		return fmt.Errorf("history: delete turns: %w", err)
	}
	if _, err := d.points.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: d.name,
		Wait:           &wait,
		Points:         qdrant.NewPointsSelector(qdrant.NewIDUUID(sessionID)),
	}); err != nil {
		return fmt.Errorf("history: delete session: %w", err)
	}
	return nil
}

// DeleteAllSessions permanently removes every persisted chat session and
// turn, while leaving the document corpus and semantic cache untouched.
func (d *dependencies) DeleteAllSessions(ctx context.Context) error {
	unlock := d.lockWrites()
	defer unlock()

	wait := true
	for _, docType := range []string{docTypeTurn, docTypeSession} {
		if _, err := d.points.Delete(ctx, &qdrant.DeletePoints{
			CollectionName: d.name,
			Wait:           &wait,
			Points:         qdrant.NewPointsSelectorFilter(docTypeFilter(docType)),
		}); err != nil {
			return fmt.Errorf("history: delete all %s records: %w", docType, err)
		}
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

func docTypeFilter(docType string) *qdrant.Filter {
	return &qdrant.Filter{Must: []*qdrant.Condition{matchKeyword("doc_type", docType)}}
}

func matchKeyword(key, value string) *qdrant.Condition {
	return &qdrant.Condition{
		ConditionOneOf: &qdrant.Condition_Field{
			Field: &qdrant.FieldCondition{
				Key:   key,
				Match: &qdrant.Match{MatchValue: &qdrant.Match_Keyword{Keyword: value}},
			},
		},
	}
}

func sessionPayload(title string, createdAt, updatedAt time.Time, turnCount int) map[string]*qdrant.Value {
	return map[string]*qdrant.Value{
		"doc_type":   qdrantutil.StringValue(docTypeSession),
		"title":      qdrantutil.StringValue(title),
		"created_at": qdrantutil.IntValue(createdAt.UnixMilli()),
		"updated_at": qdrantutil.IntValue(updatedAt.UnixMilli()),
		"turn_count": qdrantutil.IntValue(int64(turnCount)),
	}
}

func turnPayload(sessionID string, sequence int, now time.Time, turn Turn) (map[string]*qdrant.Value, error) {
	resultsJSON, err := json.Marshal(turn.Results)
	if err != nil {
		return nil, fmt.Errorf("history: marshal results: %w", err)
	}
	attachedJSON, err := json.Marshal(turn.AttachedFiles)
	if err != nil {
		return nil, fmt.Errorf("history: marshal attached files: %w", err)
	}
	return map[string]*qdrant.Value{
		"doc_type":        qdrantutil.StringValue(docTypeTurn),
		"session_id":      qdrantutil.StringValue(sessionID),
		"sequence":        qdrantutil.IntValue(int64(sequence)),
		"created_at":      qdrantutil.IntValue(now.UnixMilli()),
		"query":           qdrantutil.StringValue(turn.Query),
		"rewritten_query": qdrantutil.StringValue(turn.RewrittenQuery),
		"attached_files":  qdrantutil.StringValue(string(attachedJSON)),
		"top_k":           qdrantutil.IntValue(int64(turn.TopK)),
		"generate":        qdrantutil.BoolValue(turn.Generate),
		"results_json":    qdrantutil.StringValue(string(resultsJSON)),
		"count":           qdrantutil.IntValue(int64(turn.Count)),
		"elapsed_ms":      qdrantutil.IntValue(turn.ElapsedMS),
		"from_cache":      qdrantutil.BoolValue(turn.FromCache),
		"prompt":          qdrantutil.StringValue(turn.Prompt),
		"answer":          qdrantutil.StringValue(turn.Answer),
		"has_answer":      qdrantutil.BoolValue(turn.HasAnswer),
		"model":           qdrantutil.StringValue(turn.Model),
		"error":           qdrantutil.StringValue(turn.Error),
		"generate_error":  qdrantutil.StringValue(turn.GenerateError),
		"failed":          qdrantutil.BoolValue(turn.Failed),
	}, nil
}

func sessionFromPayload(id string, p map[string]*qdrant.Value) Session {
	return Session{
		ID:        id,
		Title:     qdrantutil.StringFromPayload(p, "title"),
		CreatedAt: time.UnixMilli(qdrantutil.IntFromPayload(p, "created_at")).UTC(),
		UpdatedAt: time.UnixMilli(qdrantutil.IntFromPayload(p, "updated_at")).UTC(),
		TurnCount: int(qdrantutil.IntFromPayload(p, "turn_count")),
	}
}

func turnFromPayload(id string, p map[string]*qdrant.Value) (Turn, error) {
	var results []TurnResult
	if raw := qdrantutil.StringFromPayload(p, "results_json"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &results); err != nil {
			return Turn{}, fmt.Errorf("history: decode results: %w", err)
		}
	}
	var attached []string
	if raw := qdrantutil.StringFromPayload(p, "attached_files"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &attached); err != nil {
			return Turn{}, fmt.Errorf("history: decode attached files: %w", err)
		}
	}
	return Turn{
		ID:             id,
		SessionID:      qdrantutil.StringFromPayload(p, "session_id"),
		Sequence:       int(qdrantutil.IntFromPayload(p, "sequence")),
		CreatedAt:      time.UnixMilli(qdrantutil.IntFromPayload(p, "created_at")).UTC(),
		Query:          qdrantutil.StringFromPayload(p, "query"),
		RewrittenQuery: qdrantutil.StringFromPayload(p, "rewritten_query"),
		AttachedFiles:  attached,
		TopK:           int(qdrantutil.IntFromPayload(p, "top_k")),
		Generate:       qdrantutil.BoolFromPayload(p, "generate"),
		Results:        results,
		Count:          int(qdrantutil.IntFromPayload(p, "count")),
		ElapsedMS:      qdrantutil.IntFromPayload(p, "elapsed_ms"),
		FromCache:      qdrantutil.BoolFromPayload(p, "from_cache"),
		Prompt:         qdrantutil.StringFromPayload(p, "prompt"),
		Answer:         qdrantutil.StringFromPayload(p, "answer"),
		HasAnswer:      qdrantutil.BoolFromPayload(p, "has_answer"),
		Model:          qdrantutil.StringFromPayload(p, "model"),
		Error:          qdrantutil.StringFromPayload(p, "error"),
		GenerateError:  qdrantutil.StringFromPayload(p, "generate_error"),
		Failed:         qdrantutil.BoolFromPayload(p, "failed"),
	}, nil
}

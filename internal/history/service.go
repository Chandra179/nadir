package history

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	qdrant "github.com/qdrant/go-client/qdrant"
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

// History persists chat sessions and their turns — see interface.go.
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
			"updated_at": intVal(now.UnixMilli()),
			"turn_count": intVal(int64(session.TurnCount + 1)),
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
		out[i] = sessionFromPayload(pointIDString(pt.Id), pt.Payload)
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
	l := uint32(10000)
	resp, err := d.points.Scroll(ctx, &qdrant.ScrollPoints{
		CollectionName: d.name,
		Filter: &qdrant.Filter{
			Must: []*qdrant.Condition{
				matchKeyword("doc_type", docTypeTurn),
				matchKeyword("session_id", sessionID),
			},
		},
		Limit:       &l,
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

	out := make([]Turn, len(resp.Result))
	for i, pt := range resp.Result {
		turn, err := turnFromPayload(pointIDString(pt.Id), pt.Payload)
		if err != nil {
			return nil, err
		}
		out[i] = turn
	}
	return out, nil
}

// DeleteSession removes a session and all of its turns. Turns go first so
// a failure can't leave orphaned turns pointing at a missing session; the
// turn side is a filtered delete (session_id index) rather than fetching
// IDs up front.
func (d *dependencies) DeleteSession(ctx context.Context, sessionID string) error {
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
		"doc_type":   strVal(docTypeSession),
		"title":      strVal(title),
		"created_at": intVal(createdAt.UnixMilli()),
		"updated_at": intVal(updatedAt.UnixMilli()),
		"turn_count": intVal(int64(turnCount)),
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
		"doc_type":        strVal(docTypeTurn),
		"session_id":      strVal(sessionID),
		"sequence":        intVal(int64(sequence)),
		"created_at":      intVal(now.UnixMilli()),
		"query":           strVal(turn.Query),
		"rewritten_query": strVal(turn.RewrittenQuery),
		"attached_files":  strVal(string(attachedJSON)),
		"top_k":           intVal(int64(turn.TopK)),
		"generate":        boolVal(turn.Generate),
		"results_json":    strVal(string(resultsJSON)),
		"count":           intVal(int64(turn.Count)),
		"elapsed_ms":      intVal(turn.ElapsedMS),
		"from_cache":      boolVal(turn.FromCache),
		"prompt":          strVal(turn.Prompt),
		"answer":          strVal(turn.Answer),
		"has_answer":      boolVal(turn.HasAnswer),
		"model":           strVal(turn.Model),
		"error":           strVal(turn.Error),
		"generate_error":  strVal(turn.GenerateError),
		"failed":          boolVal(turn.Failed),
	}, nil
}

func sessionFromPayload(id string, p map[string]*qdrant.Value) Session {
	return Session{
		ID:        id,
		Title:     pbStr(p, "title"),
		CreatedAt: time.UnixMilli(pbInt(p, "created_at")).UTC(),
		UpdatedAt: time.UnixMilli(pbInt(p, "updated_at")).UTC(),
		TurnCount: int(pbInt(p, "turn_count")),
	}
}

func turnFromPayload(id string, p map[string]*qdrant.Value) (Turn, error) {
	var results []TurnResult
	if raw := pbStr(p, "results_json"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &results); err != nil {
			return Turn{}, fmt.Errorf("history: decode results: %w", err)
		}
	}
	var attached []string
	if raw := pbStr(p, "attached_files"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &attached); err != nil {
			return Turn{}, fmt.Errorf("history: decode attached files: %w", err)
		}
	}
	return Turn{
		ID:             id,
		SessionID:      pbStr(p, "session_id"),
		Sequence:       int(pbInt(p, "sequence")),
		CreatedAt:      time.UnixMilli(pbInt(p, "created_at")).UTC(),
		Query:          pbStr(p, "query"),
		RewrittenQuery: pbStr(p, "rewritten_query"),
		AttachedFiles:  attached,
		TopK:           int(pbInt(p, "top_k")),
		Generate:       pbBool(p, "generate"),
		Results:        results,
		Count:          int(pbInt(p, "count")),
		ElapsedMS:      pbInt(p, "elapsed_ms"),
		FromCache:      pbBool(p, "from_cache"),
		Prompt:         pbStr(p, "prompt"),
		Answer:         pbStr(p, "answer"),
		HasAnswer:      pbBool(p, "has_answer"),
		Model:          pbStr(p, "model"),
		Error:          pbStr(p, "error"),
		GenerateError:  pbStr(p, "generate_error"),
		Failed:         pbBool(p, "failed"),
	}, nil
}

func pointIDString(id *qdrant.PointId) string {
	if id == nil {
		return ""
	}
	if uid, ok := id.PointIdOptions.(*qdrant.PointId_Uuid); ok {
		return uid.Uuid
	}
	if num, ok := id.PointIdOptions.(*qdrant.PointId_Num); ok {
		return fmt.Sprintf("%d", num.Num)
	}
	return ""
}

func strVal(s string) *qdrant.Value {
	return &qdrant.Value{Kind: &qdrant.Value_StringValue{StringValue: s}}
}

func intVal(n int64) *qdrant.Value {
	return &qdrant.Value{Kind: &qdrant.Value_IntegerValue{IntegerValue: n}}
}

func boolVal(b bool) *qdrant.Value {
	return &qdrant.Value{Kind: &qdrant.Value_BoolValue{BoolValue: b}}
}

func pbStr(p map[string]*qdrant.Value, key string) string {
	if v, ok := p[key]; ok {
		if s, ok := v.Kind.(*qdrant.Value_StringValue); ok {
			return s.StringValue
		}
	}
	return ""
}

func pbInt(p map[string]*qdrant.Value, key string) int64 {
	if v, ok := p[key]; ok {
		if n, ok := v.Kind.(*qdrant.Value_IntegerValue); ok {
			return n.IntegerValue
		}
	}
	return 0
}

func pbBool(p map[string]*qdrant.Value, key string) bool {
	if v, ok := p[key]; ok {
		if b, ok := v.Kind.(*qdrant.Value_BoolValue); ok {
			return b.BoolValue
		}
	}
	return false
}

package qdrantutil

import (
	"math"
	"strings"
	"testing"

	qdrant "github.com/qdrant/go-client/qdrant"
)

func TestPayloadCodecsAndPointIDs(t *testing.T) {
	payload := map[string]*qdrant.Value{
		"name":  StringValue("nadir"),
		"count": IntValue(3),
		"ready": BoolValue(true),
	}
	if StringFromPayload(payload, "name") != "nadir" || IntFromPayload(payload, "count") != 3 || !BoolFromPayload(payload, "ready") {
		t.Fatalf("payload codecs returned unexpected values: %#v", payload)
	}
	if StringFromPayload(payload, "missing") != "" || IntFromPayload(payload, "missing") != 0 || BoolFromPayload(payload, "missing") {
		t.Fatal("missing payload values should return zero values")
	}
	if got := PointIDString(qdrant.NewIDUUID("id")); got != "id" {
		t.Fatalf("PointIDString(uuid) = %q, want id", got)
	}
	if got := PointIDString(qdrant.NewIDNum(7)); got != "7" {
		t.Fatalf("PointIDString(number) = %q, want 7", got)
	}
}

func TestPayloadDefaultsForAbsentNullAndWrongTypes(t *testing.T) {
	for _, payload := range []map[string]*qdrant.Value{
		nil,
		{},
		{"field": nil},
		{"field": {}},
		{"field": &qdrant.Value{Kind: &qdrant.Value_ListValue{}}},
	} {
		if StringFromPayload(payload, "field") != "" || IntFromPayload(payload, "field") != 0 || BoolFromPayload(payload, "field") {
			t.Fatalf("payload should decode to zero values: %#v", payload)
		}
	}
}

func TestPointIDStringBoundaryValues(t *testing.T) {
	for _, tc := range []struct {
		id   *qdrant.PointId
		want string
	}{
		{nil, ""},
		{&qdrant.PointId{}, ""},
		{qdrant.NewIDNum(0), "0"},
		{qdrant.NewIDNum(math.MaxUint64), "18446744073709551615"},
	} {
		if got := PointIDString(tc.id); got != tc.want {
			t.Fatalf("PointIDString(%v) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestPartialCollectionSchema(t *testing.T) {
	for _, info := range []*qdrant.CollectionInfo{
		nil,
		{},
		{Config: &qdrant.CollectionConfig{}},
		{Config: &qdrant.CollectionConfig{Params: &qdrant.CollectionParams{}}},
		{Config: &qdrant.CollectionConfig{Params: &qdrant.CollectionParams{VectorsConfig: &qdrant.VectorsConfig{}}}},
	} {
		if err := ValidateDenseCollection("documents", info, 3); err == nil {
			t.Fatalf("incomplete dense schema should be rejected: %v", info)
		}
		if HasSparseVector(info, "bm25") {
			t.Fatalf("incomplete sparse schema should be absent: %v", info)
		}
	}
}

func TestValidateDenseCollectionAndSparseSchema(t *testing.T) {
	info := &qdrant.CollectionInfo{Config: &qdrant.CollectionConfig{Params: &qdrant.CollectionParams{
		VectorsConfig:       qdrant.NewVectorsConfig(&qdrant.VectorParams{Size: 3, Distance: qdrant.Distance_Cosine}),
		SparseVectorsConfig: qdrant.NewSparseVectorsConfig(map[string]*qdrant.SparseVectorParams{"bm25": {}}),
	}}}
	if err := ValidateDenseCollection("documents", info, 3); err != nil {
		t.Fatal(err)
	}
	if !HasSparseVector(info, "bm25") || HasSparseVector(info, "missing") {
		t.Fatal("HasSparseVector() returned incorrect schema result")
	}
	if err := ValidateDenseCollection("documents", info, 4); err == nil || !strings.Contains(err.Error(), "dimensions") {
		t.Fatal("dimension mismatch should be reported")
	}
	if err := ValidateDenseCollection("documents", nil, 3); err == nil {
		t.Fatal("nil collection info should be rejected")
	}
}

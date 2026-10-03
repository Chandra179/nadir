package qdrantutil

import (
	"strconv"

	qdrant "github.com/qdrant/go-client/qdrant"
)

// StringValue wraps a string as a Qdrant payload value.
func StringValue(value string) *qdrant.Value {
	return &qdrant.Value{Kind: &qdrant.Value_StringValue{StringValue: value}}
}

// IntValue wraps an integer as a Qdrant payload value.
func IntValue(value int64) *qdrant.Value {
	return &qdrant.Value{Kind: &qdrant.Value_IntegerValue{IntegerValue: value}}
}

// BoolValue wraps a boolean as a Qdrant payload value.
func BoolValue(value bool) *qdrant.Value {
	return &qdrant.Value{Kind: &qdrant.Value_BoolValue{BoolValue: value}}
}

// StringFromPayload reads a string field from a Qdrant payload.
func StringFromPayload(payload map[string]*qdrant.Value, key string) string {
	return payload[key].GetStringValue()
}

// IntFromPayload reads an integer field from a Qdrant payload.
func IntFromPayload(payload map[string]*qdrant.Value, key string) int64 {
	return payload[key].GetIntegerValue()
}

// BoolFromPayload reads a boolean field from a Qdrant payload.
func BoolFromPayload(payload map[string]*qdrant.Value, key string) bool {
	return payload[key].GetBoolValue()
}

// PointIDString returns the textual form of a Qdrant point identifier.
func PointIDString(id *qdrant.PointId) string {
	if uid, ok := id.GetPointIdOptions().(*qdrant.PointId_Uuid); ok {
		return uid.Uuid
	}
	if number, ok := id.GetPointIdOptions().(*qdrant.PointId_Num); ok {
		return strconv.FormatUint(number.Num, 10)
	}
	return ""
}

package qdrantutil

import (
	"fmt"

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
	if value, ok := payload[key]; ok {
		if stringValue, ok := value.Kind.(*qdrant.Value_StringValue); ok {
			return stringValue.StringValue
		}
	}
	return ""
}

// IntFromPayload reads an integer field from a Qdrant payload.
func IntFromPayload(payload map[string]*qdrant.Value, key string) int64 {
	if value, ok := payload[key]; ok {
		if integerValue, ok := value.Kind.(*qdrant.Value_IntegerValue); ok {
			return integerValue.IntegerValue
		}
	}
	return 0
}

// BoolFromPayload reads a boolean field from a Qdrant payload.
func BoolFromPayload(payload map[string]*qdrant.Value, key string) bool {
	if value, ok := payload[key]; ok {
		if boolValue, ok := value.Kind.(*qdrant.Value_BoolValue); ok {
			return boolValue.BoolValue
		}
	}
	return false
}

// PointIDString returns the textual form of a Qdrant point identifier.
func PointIDString(id *qdrant.PointId) string {
	if id == nil {
		return ""
	}
	if uid, ok := id.PointIdOptions.(*qdrant.PointId_Uuid); ok {
		return uid.Uuid
	}
	if number, ok := id.PointIdOptions.(*qdrant.PointId_Num); ok {
		return fmt.Sprintf("%d", number.Num)
	}
	return ""
}

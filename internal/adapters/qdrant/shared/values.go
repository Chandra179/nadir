package qdrantutil

import (
	"fmt"

	qdrant "github.com/qdrant/go-client/qdrant"
)

func StringValue(value string) *qdrant.Value {
	return &qdrant.Value{Kind: &qdrant.Value_StringValue{StringValue: value}}
}

func IntValue(value int64) *qdrant.Value {
	return &qdrant.Value{Kind: &qdrant.Value_IntegerValue{IntegerValue: value}}
}

func BoolValue(value bool) *qdrant.Value {
	return &qdrant.Value{Kind: &qdrant.Value_BoolValue{BoolValue: value}}
}

func StringFromPayload(payload map[string]*qdrant.Value, key string) string {
	if value, ok := payload[key]; ok {
		if stringValue, ok := value.Kind.(*qdrant.Value_StringValue); ok {
			return stringValue.StringValue
		}
	}
	return ""
}

func IntFromPayload(payload map[string]*qdrant.Value, key string) int64 {
	if value, ok := payload[key]; ok {
		if integerValue, ok := value.Kind.(*qdrant.Value_IntegerValue); ok {
			return integerValue.IntegerValue
		}
	}
	return 0
}

func BoolFromPayload(payload map[string]*qdrant.Value, key string) bool {
	if value, ok := payload[key]; ok {
		if boolValue, ok := value.Kind.(*qdrant.Value_BoolValue); ok {
			return boolValue.BoolValue
		}
	}
	return false
}

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

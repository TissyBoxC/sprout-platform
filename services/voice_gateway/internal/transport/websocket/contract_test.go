package websocket

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// contractSchemaPath locates the published control-frame schema relative to
// this package so the test proves the wire contract, not a local copy of it.
const contractSchemaPath = "../../../../../packages/contracts/schemas/voice_control.schema.json"

// controlFrameSchema is the subset of the schema this test asserts on.
type controlFrameSchema struct {
	AdditionalProperties *bool `json:"additionalProperties"`
	Properties           map[string]struct {
		Type any `json:"type"`
	} `json:"properties"`
	Required []string `json:"required"`
}

func loadControlFrameSchema(t *testing.T) controlFrameSchema {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(contractSchemaPath))
	if err != nil {
		t.Fatalf("read control frame schema: %v", err)
	}
	var schema controlFrameSchema
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parse control frame schema: %v", err)
	}
	return schema
}

// controlFrameJSONFields returns every property name ControlFrame serializes.
func controlFrameJSONFields() []string {
	controlType := reflect.TypeOf(ControlFrame{})
	fields := make([]string, 0, controlType.NumField())
	for index := 0; index < controlType.NumField(); index++ {
		tag := controlType.Field(index).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		fields = append(fields, name)
	}
	return fields
}

// TestControlFrameFieldsMatchContract prevents the schema and the encoder from
// drifting apart: the schema forbids unknown properties, so a field the frame
// sends but the schema omits makes every strict client reject a valid frame.
func TestControlFrameFieldsMatchContract(t *testing.T) {
	schema := loadControlFrameSchema(t)

	for _, field := range controlFrameJSONFields() {
		if _, ok := schema.Properties[field]; !ok {
			t.Errorf("ControlFrame sends %q but the contract does not declare it", field)
		}
	}

	declared := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		declared = append(declared, name)
	}
	serialized := make(map[string]struct{}, len(controlFrameJSONFields()))
	for _, field := range controlFrameJSONFields() {
		serialized[field] = struct{}{}
	}
	sort.Strings(declared)
	for _, name := range declared {
		if _, ok := serialized[name]; !ok {
			t.Errorf("contract declares %q but ControlFrame never sends it", name)
		}
	}
}

// TestControlFrameContractForbidsUnknownProperties pins the strictness the
// field comparison above relies on.
func TestControlFrameContractForbidsUnknownProperties(t *testing.T) {
	schema := loadControlFrameSchema(t)
	if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		t.Fatal("control frame schema must forbid unknown properties")
	}
}

// TestContentCategoryIsDeclared guards the specific field whose absence broke
// strict clients: the gateway validates it but the schema did not allow it.
func TestContentCategoryIsDeclared(t *testing.T) {
	schema := loadControlFrameSchema(t)
	if _, ok := schema.Properties["content_category"]; !ok {
		t.Fatal("contract must declare content_category")
	}

	required := make(map[string]struct{}, len(schema.Required))
	for _, name := range schema.Required {
		required[name] = struct{}{}
	}
	// Older firmware omits it, so requiring it would break every deployed unit.
	if _, ok := required["content_category"]; ok {
		t.Fatal("content_category must stay optional for pre-existing firmware")
	}
}

// TestControlFrameEncodesContentCategory proves the field reaches the wire.
func TestControlFrameEncodesContentCategory(t *testing.T) {
	encoded, err := json.Marshal(ControlFrame{
		SchemaVersion:   "1.0.0",
		Type:            "session_start",
		SessionID:       "session_1",
		DeviceID:        "device_a",
		SentAt:          "2026-10-20T00:00:00Z",
		ContentCategory: "story",
	})
	if err != nil {
		t.Fatalf("marshal control frame: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal control frame: %v", err)
	}
	if decoded["content_category"] != "story" {
		t.Fatalf("content_category = %v, want story", decoded["content_category"])
	}
}

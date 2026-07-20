package session

import (
	"encoding/json"
	"testing"
)

func TestCodexErrorDecodesStringAndObject(t *testing.T) {
	t.Parallel()

	// Object form: {"error": {"message": "boom"}}
	var withObject struct {
		Error *codexError `json:"error"`
	}
	if err := json.Unmarshal([]byte(`{"error":{"message":"object boom"}}`), &withObject); err != nil {
		t.Fatalf("object form failed: %v", err)
	}
	if withObject.Error == nil || withObject.Error.Message != "object boom" {
		t.Fatalf("object form: got %+v", withObject.Error)
	}

	// Bare string form: {"error": "boom"} — this is what mcpServer notifications send.
	var withString struct {
		Error *codexError `json:"error"`
	}
	if err := json.Unmarshal([]byte(`{"error":"string boom"}`), &withString); err != nil {
		t.Fatalf("string form failed: %v", err)
	}
	if withString.Error == nil || withString.Error.Message != "string boom" {
		t.Fatalf("string form: got %+v", withString.Error)
	}

	// Absent / null leaves the pointer nil.
	var absent struct {
		Error *codexError `json:"error"`
	}
	if err := json.Unmarshal([]byte(`{"error":null}`), &absent); err != nil {
		t.Fatalf("null form failed: %v", err)
	}
	if absent.Error != nil {
		t.Fatalf("null form: expected nil, got %+v", absent.Error)
	}
}

func TestCodexParserHandlesStringError(t *testing.T) {
	t.Parallel()
	// A Codex event whose top-level error is a bare string must not fail to parse.
	_, err := codexParser{}.Parse([]byte(`{"type":"error","error":"startup failed"}`))
	if err != nil {
		t.Fatalf("string error should parse: %v", err)
	}
}

package structured

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSubmitValidatesAndReturnsArguments(t *testing.T) {
	tool := Submit("submit", "submit result", json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`))
	if tool.Name() != "submit" || tool.Description() != "submit result" || !tool.ReadOnly() {
		t.Fatalf("unexpected tool metadata: %s %s", tool.Name(), tool.Description())
	}
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"value":"ok"}`))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(result.Output), &got); err != nil || got["value"] != "ok" {
		t.Fatalf("Output=%q err=%v", result.Output, err)
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"value":3}`)); err == nil {
		t.Fatal("invalid arguments accepted")
	}
}

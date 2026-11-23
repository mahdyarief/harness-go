package schema_test

import (
	"encoding/json"
	"errors"
	"testing"

	"hsg/internal/harness/schema"
)

func compile(t *testing.T, raw string) *schema.Schema {
	t.Helper()
	s, err := schema.Compile(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return s
}

func TestValidArgumentsPass(t *testing.T) {
	s := compile(t, `{"type":"object","properties":{"q":{"type":"string","minLength":1},"limit":{"type":"integer","minimum":1,"maximum":5}},"required":["q"],"additionalProperties":false}`)
	if got := s.Validate(json.RawMessage(`{"q":"cari","limit":3}`)); len(got) != 0 {
		t.Fatalf("violations=%+v; want none", got)
	}
}

func TestMissingRequiredIsReported(t *testing.T) {
	s := compile(t, `{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)
	got := s.Validate(json.RawMessage(`{}`))
	if len(got) != 1 || got[0].Path != ".q" {
		t.Fatalf("violations=%+v; want one at .q", got)
	}
}

func TestUndeclaredKeyIsReported(t *testing.T) {
	s := compile(t, `{"type":"object","properties":{"q":{"type":"string"}},"additionalProperties":false}`)
	got := s.Validate(json.RawMessage(`{"q":"x","extra":1}`))
	if len(got) != 1 || got[0].Path != ".extra" {
		t.Fatalf("violations=%+v; want one at .extra", got)
	}
}

func TestTypeEnumAndRange(t *testing.T) {
	s := compile(t, `{"type":"object","properties":{"label":{"type":"string","enum":["blue","not_blue"]},"count":{"type":"integer","minimum":2},"items":{"type":"array","items":{"type":"string"}}},"required":["label"]}`)
	got := s.Validate(json.RawMessage(`{"label":"red","count":1,"items":["ok",3]}`))
	paths := map[string]bool{}
	for _, v := range got {
		paths[v.Path] = true
	}
	for _, want := range []string{".label", ".count", ".items[1]"} {
		if !paths[want] {
			t.Fatalf("violations=%+v; missing path %q", got, want)
		}
	}
}

func TestInvalidJSONArguments(t *testing.T) {
	s := compile(t, `{"type":"object"}`)
	got := s.Validate(json.RawMessage(`not json`))
	if len(got) != 1 {
		t.Fatalf("violations=%+v; want one", got)
	}
}

func TestCompileRejectsNonObjectRoot(t *testing.T) {
	if _, err := schema.Compile(json.RawMessage(`{"type":"string"}`)); err == nil {
		t.Fatal("want error for non-object root")
	}
	if _, err := schema.Compile(json.RawMessage(`{`)); err == nil {
		t.Fatal("want error for malformed JSON")
	}
}

func TestNestedObjectAndTypedEnum(t *testing.T) {
	s := compile(t, `{"type":"object","properties":{"options":{"type":"object","properties":{"mode":{"type":"string","enum":["fast"]}},"required":["mode"],"additionalProperties":false},"count":{"type":"integer"},"enabled":{"type":"boolean","enum":[true]}}}`)
	got := s.Validate(json.RawMessage(`{"options":{"extra":1},"count":1.5,"enabled":false}`))
	paths := map[string]bool{}
	for _, v := range got {
		paths[v.Path] = true
	}
	if enabled := compile(t, `{"type":"object","properties":{"enabled":{"type":"boolean","enum":[true]}}}`).Validate(json.RawMessage(`{"enabled":false}`)); len(enabled) != 1 {
		t.Fatalf("boolean enum violations=%+v; want one", enabled)
	}
	for _, want := range []string{".options.mode", ".options.extra", ".count"} {
		if !paths[want] {
			t.Fatalf("violations=%+v; missing path %q", got, want)
		}
	}
}

func TestTrailingJSONIsRejected(t *testing.T) {
	s := compile(t, `{"type":"object"}`)
	if got := s.Validate(json.RawMessage(` {} {}`)); len(got) != 1 {
		t.Fatalf("violations=%+v; want trailing data rejected", got)
	}
}

func TestIntegerExponentPasses(t *testing.T) {
	s := compile(t, `{"type":"object","properties":{"count":{"type":"integer"}}}`)
	if got := s.Validate(json.RawMessage(`{"count":1e3}`)); len(got) != 0 {
		t.Fatalf("violations=%+v; want integral exponent accepted", got)
	}
}

func TestF5KeywordsPassAndFail(t *testing.T) {
	s := compile(t, `{"type":"object","properties":{"fixed":{"const":"v1"},"code":{"type":"string","pattern":"^[A-Z]+$"},"values":{"type":"array","minItems":1,"maxItems":2,"items":{"type":"integer","minimum":1,"maximum":3}}}}`)
	for _, args := range []string{
		`{"fixed":"v1","code":"ABC","values":[1,3]}`,
		`{"fixed":"v1","code":"Z","values":[2]}`,
	} {
		if got := s.Validate(json.RawMessage(args)); len(got) != 0 {
			t.Errorf("valid args %s: violations=%+v", args, got)
		}
	}
	got := s.Validate(json.RawMessage(`{"fixed":"v2","code":"bad-code","values":[]}`))
	paths := map[string]bool{}
	for _, v := range got {
		paths[v.Path] = true
	}
	for _, want := range []string{".fixed", ".code", ".values"} {
		if !paths[want] {
			t.Errorf("violations=%+v; missing path %q", got, want)
		}
	}
	tooMany := s.Validate(json.RawMessage(`{"fixed":"v1","code":"A","values":[1,2,3]}`))
	if len(tooMany) == 0 || tooMany[0].Path != ".values" {
		t.Errorf("violations=%+v; want maxItems path .values", tooMany)
	}
}

func TestCompileRejectsUnsupportedKeyword(t *testing.T) {
	for _, keyword := range []string{"format", "description"} {
		_, err := schema.Compile(json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","` + keyword + `":"value"}}}`))
		if err == nil || !errors.Is(err, schema.ErrUnsupported) {
			t.Errorf("keyword=%q err=%v; want ErrUnsupported", keyword, err)
		}
	}
}

func TestCompileIsCached(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","pattern":"^a+$"}}}`)
	first, err := schema.Compile(raw)
	if err != nil {
		t.Fatal(err)
	}
	second, err := schema.Compile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("identical schema recompiled instead of reusing the cached value")
	}
}

func TestEnumPreservesJSONTypes(t *testing.T) {
	for _, tc := range []struct {
		schema string
		args   string
		valid  bool
	}{
		{`{"type":"object","properties":{"v":{"enum":[1]}}}`, `{"v":"1"}`, false},
		{`{"type":"object","properties":{"v":{"enum":["true"]}}}`, `{"v":true}`, false},
		{`{"type":"object","properties":{"v":{"enum":[1]}}}`, `{"v":1.0}`, true},
		{`{"type":"object","properties":{"v":{"enum":[9007199254740992]}}}`, `{"v":9007199254740993}`, false},
	} {
		got := compile(t, tc.schema).Validate(json.RawMessage(tc.args))
		if (len(got) == 0) != tc.valid {
			t.Errorf("schema=%s args=%s violations=%+v; valid=%t", tc.schema, tc.args, got, tc.valid)
		}
	}
}

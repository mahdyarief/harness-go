package kb

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestQueryExcerpt(t *testing.T) {
	for _, tc := range []struct {
		name, content, query, evidence string
		budget                         int
	}{
		{"late evidence", strings.Repeat("prefix ", 1000) + "ICD10 ZXQ482 evidence", "ZXQ482", "ZXQ482", 240},
		{"unicode", strings.Repeat("界", 2000) + " İCD10 diagnosis", "İCD10", "İCD10", 200},
		{"phrase", strings.Repeat("intro ", 1000) + "exact phrase 4817", "\"exact phrase\" 4817", "4817", 200},
		{"no matches", strings.Repeat("世界", 2000), "missing", "", 100},
		{"tiny", strings.Repeat("界", 50), "界", "", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := QueryExcerpt(tc.content, tc.query, tc.budget)
			if len(got) > tc.budget || !utf8.ValidString(got) || !strings.Contains(got, tc.evidence) {
				t.Fatalf("excerpt %q (%d bytes)", got, len(got))
			}
		})
	}
}

func TestMarshalPassagesBudget(t *testing.T) {
	for _, text := range []string{strings.Repeat("界<>&\"\n\x00", 1500), strings.Repeat("x", 20000), ""} {
		passages := make([]Passage, 8)
		for i := range passages {
			passages[i] = Passage{"document", strings.Repeat("<", 500), strings.Repeat("界", 200), "baris 90", text + "ZXQ482", true}
		}
		encoded, err := MarshalPassages("ZXQ482", passages, 5)
		if err != nil || len(encoded) > 7800 || !json.Valid([]byte(encoded)) {
			t.Fatalf("payload: %d bytes %v", len(encoded), err)
		}
		var payload passagePayload
		if err = json.Unmarshal([]byte(encoded), &payload); err != nil {
			t.Fatal(err)
		}
		if !payload.Untrusted || len(payload.Passages) == 0 || len(payload.Passages) > 5 {
			t.Fatalf("payload %+v", payload)
		}
		first := payload.Passages[0]
		if first.DocumentID != "document" || first.Locator != "baris 90" || !first.Global || !strings.Contains(first.Content, "ZXQ482") {
			t.Fatalf("provenance/evidence lost: %+v", first)
		}
	}
	encoded, err := MarshalPassages("", nil, 5)
	if err != nil || !strings.Contains(encoded, `"passages":[]`) {
		t.Fatalf("empty payload %s %v", encoded, err)
	}
}

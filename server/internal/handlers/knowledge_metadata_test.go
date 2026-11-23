package handlers

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"hsg/internal/dtos"
	"hsg/internal/models"
)

func TestHybridCandidatesKeepMetadata(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	hits := []models.KnowledgeHit{{
		ChunkID:    uuid.New(),
		DocumentID: uuid.New(),
		Filename:   "prosedur.pdf",
		Format:     "pdf",
		Content:    "isi",
		Attributes: json.RawMessage(`{"regulasi":"PER-2/2024"}`),
		ValidFrom:  &from,
		ValidTo:    &to,
		Version:    "v2",
	}}
	back := fromCandidates(toCandidates(hits))
	if len(back) != 1 {
		t.Fatalf("hit = %d, mau 1", len(back))
	}
	got := back[0]
	if got.Filename != "prosedur.pdf" || got.Format != "pdf" || got.Version != "v2" {
		t.Fatalf("metadata hilang: %+v", got)
	}
	if string(got.Attributes) != `{"regulasi":"PER-2/2024"}` {
		t.Fatalf("attributes = %s", got.Attributes)
	}
	if got.ValidFrom == nil || got.ValidTo == nil || !got.ValidFrom.Equal(from) || !got.ValidTo.Equal(to) {
		t.Fatalf("window hilang: %v %v", got.ValidFrom, got.ValidTo)
	}
}

func TestParseAttributesJSON(t *testing.T) {
	got, err := parseAttributesJSON(`{"tier":"gold","score":3,"active":true,"note":null}`)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]any
	dec := json.NewDecoder(strings.NewReader(string(got)))
	dec.UseNumber()
	if err := dec.Decode(&values); err != nil {
		t.Fatal(err)
	}
	if values["tier"] != "gold" || values["score"] != json.Number("3") || values["active"] != true {
		t.Fatalf("atribut tidak utuh: %v", values)
	}
	if _, ok := values["note"]; !ok {
		t.Fatal("null harus tetap disimpan")
	}
}

func TestParseAttributesJSONRejects(t *testing.T) {
	cases := map[string]string{
		"bukan objek":    `[1,2]`,
		"nilai objek":    `{"a":{"nested":true}}`,
		"terlalu banyak": `{"a":1,"b":2,"c":3,"d":4,"e":5,"f":6,"g":7,"h":8,"i":9,"j":10,"k":11}`,
	}
	for name, raw := range cases {
		if _, err := parseAttributesJSON(raw); err == nil {
			t.Errorf("%s: mau error", name)
		}
	}
	if got, err := parseAttributesJSON("   "); err != nil || got != nil {
		t.Errorf("kosong harus nil tanpa error, dapat %v %v", got, err)
	}
	if got, err := parseAttributesJSON(`{}`); err != nil || string(got) != "{}" {
		t.Errorf("objek kosong = %s %v", got, err)
	}
}

func TestParseOptionalDate(t *testing.T) {
	at, err := parseOptionalDate("2026-03-01", "valid_from")
	if err != nil || at == nil || !at.Equal(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("tanggal = %v err=%v", at, err)
	}
	if at, err := parseOptionalDate("", "valid_from"); err != nil || at != nil {
		t.Fatalf("kosong harus nil, dapat %v %v", at, err)
	}
	if _, err := parseOptionalDate("03-01-2026", "valid_from"); err == nil {
		t.Fatal("format salah harus error")
	}
}

func TestMetadataInputFromBody(t *testing.T) {
	from, to, version := "2026-01-01", "2026-12-31", "v2"
	body := dtos.KnowledgeMetadataBody{
		Attributes: json.RawMessage(`{"tier":"gold"}`),
		ValidFrom:  &from,
		ValidTo:    &to,
		Version:    &version,
	}
	in, err := metadataInputFromBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if !in.SetWindow || in.ValidFrom == nil || in.ValidTo == nil {
		t.Fatalf("window tidak diset: %+v", in)
	}
	if in.Version == nil || *in.Version != "v2" {
		t.Fatalf("version tidak diset")
	}

	onlyFrom := "2026-01-01"
	if _, err := metadataInputFromBody(dtos.KnowledgeMetadataBody{ValidFrom: &onlyFrom}); err == nil {
		t.Fatal("valid_from tanpa valid_to harus error")
	}
	if _, err := metadataInputFromBody(dtos.KnowledgeMetadataBody{
		ValidFrom: &to, ValidTo: &from,
	}); err == nil {
		t.Fatal("valid_from > valid_to harus error")
	}
	empty := ""
	in, err = metadataInputFromBody(dtos.KnowledgeMetadataBody{ValidFrom: &empty, ValidTo: &empty})
	if err != nil || !in.SetWindow || in.ValidFrom != nil || in.ValidTo != nil {
		t.Fatalf("tanggal kosong harus membersihkan window: %+v %v", in, err)
	}
	cleared, err := metadataInputFromBody(dtos.KnowledgeMetadataBody{Attributes: json.RawMessage(`{}`)})
	if err != nil || string(cleared.Attributes) != "{}" {
		t.Fatalf("attributes kosong harus {}: %s %v", cleared.Attributes, err)
	}
}

package repositories

import (
	"strings"
	"testing"
)

// scanKnowledgeDocument memindai kolom secara posisional. Kedua konstanta
// SELECT harus mengikuti urutan yang sama — perbedaan satu kolom membuat
// UUID (uploaded_by) dipindai ke field int (chunk_count) dan mengakibatkan 500
// pada list dokumen.
func TestKnowledgeColumnsMatchScannerOrder(t *testing.T) {
	// Urutan yang diharapkan oleh scanKnowledgeDocument, tanpa prefix alias.
	expected := []string{
		"id", "tenant_id", "category_id", "category_name", "title", "filename",
		"format", "size_bytes", "char_count", "chunk_count", "uploaded_by",
		"created_at", "updated_at", "extraction_metadata", "attributes",
		"valid_from", "valid_to", "version", "status",
	}
	for name, cols := range map[string]string{
		"knowledgeColumns":     knowledgeColumns,
		"knowledgeListColumns": knowledgeListColumns,
	} {
		fields := strings.Split(cols, ",")
		if len(fields) != len(expected) {
			t.Fatalf("%s: %d kolom, scanner butuh %d", name, len(fields), len(expected))
		}
		for i, want := range expected {
			if normalizeColumn(fields[i]) != want {
				t.Errorf("%s: kolom[%d] = %q, scanner mengharapkan %q", name, i, fields[i], want)
			}
		}
	}
}

// normalizeColumn membuang alias tabel, kecuali kategori: di knowledgeColumns
// nama kategori datang dari subquery, di knowledgeListColumns lewat c.name.
func normalizeColumn(field string) string {
	field = strings.TrimSpace(field)
	switch {
	case strings.Contains(field, "name") && !strings.Contains(field, "filename"):
		return "category_name"
	case !strings.HasPrefix(field, "d."):
		return field
	}
	return strings.TrimPrefix(field, "d.")
}

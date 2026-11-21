package kb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func metadataExtract(t *testing.T, f Format, data []byte) ExtractionMetadata {
	t.Helper()
	r := NewRegistry(NewMarkdownExtractor(), NewTextExtractor(), NewCSVExtractor(), NewDOCXExtractor(), NewXLSXExtractor())
	old, err := r.Extract(context.Background(), f, data)
	if err != nil {
		t.Fatal(err)
	}
	segs, m, err := r.ExtractWithMetadata(context.Background(), f, data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old, segs) {
		t.Fatal("metadata changed extracted segments")
	}
	if m.ExtractorVersion == "" || m.Warnings == nil || m.Coverage == nil {
		t.Fatalf("invalid metadata: %+v", m)
	}
	return m
}

func metadataWarnings(t *testing.T, m ExtractionMetadata, want ...string) {
	t.Helper()
	got := slices.Clone(m.Warnings)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("warnings %v, want %v", got, want)
	}
	status := "complete"
	if len(want) > 0 {
		status = "limited"
	}
	if m.Status != status {
		t.Fatalf("status %s, want %s", m.Status, status)
	}
}

func metadataCoverage(t *testing.T, m ExtractionMetadata, unit, scope string, processed int, total *int) {
	t.Helper()
	for _, c := range m.Coverage {
		if c.Unit == unit && c.Scope == scope {
			if c.Processed != processed || !reflect.DeepEqual(c.Total, total) {
				t.Fatalf("coverage %+v, want %d/%v", c, processed, total)
			}
			return
		}
	}
	t.Fatalf("missing %s/%s coverage: %+v", unit, scope, m)
}

func metadataInt(n int) *int { return &n }

func TestMetadataMarkdownText(t *testing.T) {
	for _, tc := range []struct {
		name     string
		f        Format
		text     string
		warnings []string
	}{
		{"normal", FormatMarkdown, "# Heading\nbody", nil},
		{"front matter", FormatMarkdown, "---\ntitle: ignored\n---\nbody", []string{"front_matter_omitted"}},
		{"unclosed front matter", FormatMarkdown, "---\nbody", nil},
		{"fenced heading", FormatMarkdown, "```\n# " + strings.Repeat("a", 1001) + "\n```", nil},
		{"heading boundary", FormatMarkdown, "# " + strings.Repeat("a", 1000) + "\nbody", nil},
		{"heading cap", FormatMarkdown, "# " + strings.Repeat("a", 1001) + "\nbody", []string{"heading_truncated"}},
		{"normalization", FormatMarkdown, "text\xff\x00", []string{"text_normalized"}},
		{"text normalization", FormatText, "text\xff\x00", []string{"text_normalized"}},
		{"plain text", FormatText, "---\n# plain\r\ntext", nil},
	} {
		t.Run(tc.name, func(t *testing.T) { metadataWarnings(t, metadataExtract(t, tc.f, []byte(tc.text)), tc.warnings...) })
	}
}

func TestMetadataCSV(t *testing.T) {
	m := metadataExtract(t, FormatCSV, []byte("name,note\nA,\"line1\nline2\"\n\nB,ok\n"))
	metadataWarnings(t, m)
	metadataCoverage(t, m, "rows", "document", 2, metadataInt(2))
}

func TestMetadataDOCX(t *testing.T) {
	for _, tc := range []struct {
		name, inner string
		extra       []docxTestEntry
		warnings    []string
	}{
		{"normal", docxP("body"), nil, nil},
		{"parts", docxP("body"), []docxTestEntry{{"word/header1.xml", "header"}, {"word/footer1.xml", "footer"}, {"word/footnotes.xml", "notes"}, {"word/comments.xml", "comment"}, {"word/media/a.png", "image"}, {"word/embeddings/a.bin", "object"}}, []string{"unsupported_parts"}},
		{"text box", docxP("body") + `<w:txbxContent>` + docxP("skipped") + `</w:txbxContent>`, nil, []string{"unsupported_content"}},
		{"fallback", docxP("body") + `<Fallback>` + docxP("skipped") + `</Fallback>`, nil, []string{"unsupported_content"}},
		{"empty fallback", docxP("body") + `<Fallback/>`, nil, nil},
		{"drawing", docxP("body") + `<w:drawing/>`, nil, []string{"unsupported_content"}},
		{"heading boundary", docxH("Heading1", strings.Repeat("a", docxMaxHeadingRunes)) + docxP("body"), nil, nil},
		{"heading cap", docxH("Heading1", strings.Repeat("a", docxMaxHeadingRunes+1)) + docxP("body"), nil, []string{"heading_truncated"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metadataWarnings(t, metadataExtract(t, FormatDOCX, docxTestFile(t, tc.inner, tc.extra...)), tc.warnings...)
		})
	}
}

func TestMetadataXLSXLimits(t *testing.T) {
	for _, extra := range []int{0, 1} {
		t.Run(fmt.Sprint(extra), func(t *testing.T) {
			data := xlsxTestBuild(t, func(f *excelize.File) {
				headers := make([]any, xlsxMaxCols+extra)
				for i := range headers {
					headers[i] = "H"
				}
				headers[0] = strings.Repeat("h", xlsxMaxHeaderRunes+extra)
				cells := make([]any, len(headers))
				cells[0] = strings.Repeat("c", xlsxMaxCellRunes+extra)
				xlsxTestSet(t, f, "Sheet1", [][]any{headers, cells})
			})
			m := metadataExtract(t, FormatXLSX, data)
			if extra == 0 {
				metadataWarnings(t, m)
			} else {
				metadataWarnings(t, m, "column_limit", "header_truncated", "cell_truncated")
			}
			metadataCoverage(t, m, "sheets", "visible", 1, metadataInt(1))
			metadataCoverage(t, m, "rows", "Sheet1", 1, metadataInt(1))
		})
	}
}

func TestMetadataXLSXRowCap(t *testing.T) {
	for _, extra := range []int{0, 1} {
		data := xlsxTestBuild(t, func(f *excelize.File) {
			// Sparse rows still consume the budget, including leading empties.
			f.SetCellStr("Sheet1", "A3", "header")
			f.SetCellStr("Sheet1", "A4", "body")
			f.SetCellStr("Sheet1", fmt.Sprintf("A%d", xlsxMaxRowsPerSheet+extra), "last")
		})
		m := metadataExtract(t, FormatXLSX, data)
		if extra == 0 {
			metadataWarnings(t, m)
			metadataCoverage(t, m, "rows", "Sheet1", xlsxMaxRowsPerSheet-1, metadataInt(xlsxMaxRowsPerSheet-1))
		} else {
			metadataWarnings(t, m, "row_limit")
			metadataCoverage(t, m, "rows", "Sheet1", xlsxMaxRowsPerSheet-1, nil)
			b, _ := json.Marshal(m)
			if !strings.Contains(string(b), `"total":null`) {
				t.Fatal(string(b))
			}
		}
	}
}

func TestMetadataXLSXSheetCap(t *testing.T) {
	for _, extra := range []int{0, 1} {
		data := xlsxTestBuild(t, func(f *excelize.File) {
			f.SetCellStr("Sheet1", "A1", "header")
			f.SetCellStr("Sheet1", "A2", "body")
			for i := 1; i < xlsxMaxSheets+extra; i++ {
				f.NewSheet(fmt.Sprintf("S%d", i))
			}
			f.NewSheet("Hidden")
			f.SetSheetVisible("Hidden", false)
		})
		m := metadataExtract(t, FormatXLSX, data)
		want := []string{"hidden_sheets"}
		if extra > 0 {
			want = append(want, "sheet_limit")
		}
		metadataWarnings(t, m, want...)
		metadataCoverage(t, m, "sheets", "visible", xlsxMaxSheets, metadataInt(xlsxMaxSheets+extra))
	}
}

func TestMetadataXLSXObjects(t *testing.T) {
	data := xlsxTestBuild(t, func(f *excelize.File) { xlsxTestSet(t, f, "Sheet1", [][]any{{"header"}, {"body"}}) })
	entries := xlsxTestEntries(t, data)
	entries = append(entries, xlsxTestZipEntry{"xl/media/image.png", []byte("image")}, xlsxTestZipEntry{"xl/embeddings/object.bin", []byte("object")})
	metadataWarnings(t, metadataExtract(t, FormatXLSX, xlsxTestZip(t, entries...)), "images_or_objects")
}

type metadataProbe struct {
	calls *int
	fail  bool
}

func (metadataProbe) Format() Format { return FormatText }
func (metadataProbe) Extract(context.Context, []byte) ([]Segment, error) {
	panic("legacy method must not be called")
}
func (e metadataProbe) ExtractWithMetadata(context.Context, []byte) ([]Segment, ExtractionMetadata, error) {
	*e.calls++
	m := newExtractionMetadata()
	m.warn("text_normalized")
	if e.fail {
		return []Segment{{Text: "partial"}}, m, ErrCorrupt
	}
	return []Segment{{Text: "ok"}}, m, nil
}

func TestMetadataRegistryContract(t *testing.T) {
	for _, fail := range []bool{false, true} {
		calls := 0
		r := NewRegistry(metadataProbe{&calls, fail})
		segs, m, err := r.ExtractWithMetadata(context.Background(), FormatText, nil)
		if calls != 1 {
			t.Fatalf("calls %d", calls)
		}
		if fail {
			if !errors.Is(err, ErrCorrupt) || segs != nil || !reflect.DeepEqual(m, ExtractionMetadata{}) {
				t.Fatalf("partial result: %v %+v %v", segs, m, err)
			}
		} else if err != nil || len(segs) != 1 {
			t.Fatalf("%v %v", segs, err)
		}
	}
	r := NewRegistry(segmentLimitExtractor{segments: []Segment{{Text: "ok"}}})
	_, m, err := r.ExtractWithMetadata(context.Background(), FormatText, nil)
	if err != nil {
		t.Fatal(err)
	}
	metadataWarnings(t, m, "metadata_unavailable")
	for _, n := range []int{MaxSegments, MaxSegments + 1} {
		r := NewRegistry(segmentLimitExtractor{segments: make([]Segment, n)})
		segs, m, err := r.ExtractWithMetadata(context.Background(), FormatText, nil)
		if n == MaxSegments {
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		if !errors.Is(err, ErrTooLarge) || segs != nil || !reflect.DeepEqual(m, ExtractionMetadata{}) {
			t.Fatal("partial overflow")
		}
	}
}

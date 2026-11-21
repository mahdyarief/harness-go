package kb

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"runtime"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func xlsxTestBuild(t testing.TB, build func(f *excelize.File)) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	build(f)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func xlsxTestSet(t testing.TB, f *excelize.File, sheet string, rows [][]any) {
	t.Helper()
	for i, row := range rows {
		cell, err := excelize.CoordinatesToCellName(1, i+1)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.SetSheetRow(sheet, cell, &row); err != nil {
			t.Fatal(err)
		}
	}
}

type xlsxTestZipEntry struct {
	name string
	body []byte
}

// xlsxTestEntries copies every part of a real workbook so callers can append
// extra parts (media, embeddings) without hand-building a valid package.
func xlsxTestEntries(t testing.TB, workbook []byte) []xlsxTestZipEntry {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(workbook), int64(len(workbook)))
	if err != nil {
		t.Fatal(err)
	}
	entries := make([]xlsxTestZipEntry, 0, len(zr.File))
	for _, zf := range zr.File {
		rc, err := zf.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, xlsxTestZipEntry{name: zf.Name, body: body})
	}
	return entries
}

func xlsxTestZip(t testing.TB, entries ...xlsxTestZipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, entry := range entries {
		w, err := zw.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func xlsxTestExtract(t *testing.T, data []byte) ([]Segment, error) {
	t.Helper()
	return NewXLSXExtractor().Extract(context.Background(), data)
}

func TestXLSXFormat(t *testing.T) {
	if NewXLSXExtractor().Format() != FormatXLSX {
		t.Fatal("wrong format")
	}
}

func TestXLSXHeaderRenderingAndEmptyCells(t *testing.T) {
	data := xlsxTestBuild(t, func(f *excelize.File) {
		xlsxTestSet(t, f, "Sheet1", [][]any{
			{"Nama", "Kota", "Catatan"},
			{"Budi", "Bandung", nil},
			{nil, nil, nil},
			{"Siti", nil, "Pelanggan setia 😀"},
			{nil, nil, "hanya catatan"},
		})
	})
	segs, err := xlsxTestExtract(t, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 {
		t.Fatalf("got %#v", segs)
	}
	s := segs[0]
	want := "Nama: Budi; Kota: Bandung\nNama: Siti; Catatan: Pelanggan setia 😀\nCatatan: hanya catatan"
	if s.Text != want {
		t.Fatalf("got %q\nwant %q", s.Text, want)
	}
	if len(s.Heading) != 1 || s.Heading[0] != "Sheet1" {
		t.Fatalf("heading %#v", s.Heading)
	}
	if s.Locator != "sheet Sheet1, baris 2-5" {
		t.Fatalf("locator %q", s.Locator)
	}
}

func TestXLSXMissingHeaderUsesColumnLetter(t *testing.T) {
	data := xlsxTestBuild(t, func(f *excelize.File) {
		xlsxTestSet(t, f, "Sheet1", [][]any{{"Nama", nil}, {"Budi", "x"}})
	})
	segs, err := xlsxTestExtract(t, data)
	if err != nil {
		t.Fatal(err)
	}
	if segs[0].Text != "Nama: Budi; B: x" {
		t.Fatalf("got %q", segs[0].Text)
	}
}

func TestXLSXRowGroupingAndLocator(t *testing.T) {
	const dataRows = xlsxRowsPerSegment*2 + 5
	data := xlsxTestBuild(t, func(f *excelize.File) {
		rows := [][]any{{"No"}}
		for i := 1; i <= dataRows; i++ {
			rows = append(rows, []any{i})
		}
		xlsxTestSet(t, f, "Sheet1", rows)
	})
	segs, err := xlsxTestExtract(t, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 3 {
		t.Fatalf("want 3 segments, got %d", len(segs))
	}
	wantLoc := []string{
		fmt.Sprintf("sheet Sheet1, baris 2-%d", 1+xlsxRowsPerSegment),
		fmt.Sprintf("sheet Sheet1, baris %d-%d", 2+xlsxRowsPerSegment, 1+2*xlsxRowsPerSegment),
		fmt.Sprintf("sheet Sheet1, baris %d-%d", 2+2*xlsxRowsPerSegment, 1+dataRows),
	}
	for i, s := range segs {
		if s.Locator != wantLoc[i] {
			t.Errorf("segment %d locator %q want %q", i, s.Locator, wantLoc[i])
		}
	}
	if got := strings.Count(segs[0].Text, "\n") + 1; got != xlsxRowsPerSegment {
		t.Errorf("first segment has %d rows", got)
	}
}

func TestXLSXMultipleAndHiddenSheets(t *testing.T) {
	data := xlsxTestBuild(t, func(f *excelize.File) {
		xlsxTestSet(t, f, "Sheet1", [][]any{{"A"}, {"satu"}})
		for _, name := range []string{"Penjualan", "Rahasia"} {
			if _, err := f.NewSheet(name); err != nil {
				t.Fatal(err)
			}
		}
		xlsxTestSet(t, f, "Penjualan", [][]any{{"Produk"}, {"Kopi"}})
		xlsxTestSet(t, f, "Rahasia", [][]any{{"Kunci"}, {"jangan-bocor"}})
		if err := f.SetSheetVisible("Rahasia", false); err != nil {
			t.Fatal(err)
		}
	})
	segs, err := xlsxTestExtract(t, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 2 {
		t.Fatalf("got %#v", segs)
	}
	if segs[0].Heading[0] != "Sheet1" || segs[1].Heading[0] != "Penjualan" {
		t.Fatalf("order/headings: %#v", segs)
	}
	for _, s := range segs {
		if strings.Contains(s.Text, "jangan-bocor") {
			t.Fatal("hidden sheet leaked")
		}
	}
}

func TestXLSXOnlyHiddenSheetIsEmpty(t *testing.T) {
	data := xlsxTestBuild(t, func(f *excelize.File) {
		idx, err := f.NewSheet("Dua")
		if err != nil {
			t.Fatal(err)
		}
		xlsxTestSet(t, f, "Sheet1", [][]any{{"A"}, {"x"}})
		// excelize refuses to hide the active sheet, so move focus first.
		f.SetActiveSheet(idx)
		if err := f.SetSheetVisible("Sheet1", false); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := xlsxTestExtract(t, data); !errors.Is(err, ErrEmpty) {
		t.Fatalf("want ErrEmpty, got %v", err)
	}
}

func TestXLSXFormattedValueNotRawOrFormula(t *testing.T) {
	data := xlsxTestBuild(t, func(f *excelize.File) {
		xlsxTestSet(t, f, "Sheet1", [][]any{{"Rasio", "Total"}, {0.256, 0}})
		pct := "0.0%"
		style, err := f.NewStyle(&excelize.Style{CustomNumFmt: &pct})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.SetCellStyle("Sheet1", "A2", "A2", style); err != nil {
			t.Fatal(err)
		}
		if err := f.SetCellFormula("Sheet1", "B2", "1+2"); err != nil {
			t.Fatal(err)
		}
		if err := f.SetCellValue("Sheet1", "B2", 3); err != nil {
			t.Fatal(err)
		}
		if err := f.SetCellFormula("Sheet1", "B2", "1+2"); err != nil {
			t.Fatal(err)
		}
	})
	segs, err := xlsxTestExtract(t, data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(segs[0].Text, "Rasio: 25.6%") {
		t.Fatalf("number format not applied: %q", segs[0].Text)
	}
	if strings.Contains(segs[0].Text, "1+2") {
		t.Fatalf("raw formula leaked: %q", segs[0].Text)
	}
}

func TestXLSXRowLimit(t *testing.T) {
	over := xlsxMaxRowsPerSheet + 500
	data := xlsxTestBuild(t, func(f *excelize.File) {
		sw, err := f.NewStreamWriter("Sheet1")
		if err != nil {
			t.Fatal(err)
		}
		if err := sw.SetRow("A1", []any{"No"}); err != nil {
			t.Fatal(err)
		}
		for i := 2; i <= over; i++ {
			cell, _ := excelize.CoordinatesToCellName(1, i)
			if err := sw.SetRow(cell, []any{i}); err != nil {
				t.Fatal(err)
			}
		}
		if err := sw.Flush(); err != nil {
			t.Fatal(err)
		}
	})
	segs, err := xlsxTestExtract(t, data)
	if err != nil {
		t.Fatal(err)
	}
	joined := make([]string, len(segs))
	for i, s := range segs {
		joined[i] = s.Text
	}
	all := strings.Join(joined, "\n")
	if !strings.Contains(all, fmt.Sprintf("No: %d", xlsxMaxRowsPerSheet)) {
		t.Fatal("rows up to the limit should be kept")
	}
	if strings.Contains(all, fmt.Sprintf("No: %d\n", xlsxMaxRowsPerSheet+1)) || strings.HasSuffix(all, fmt.Sprintf("No: %d", over)) {
		t.Fatal("rows past the limit must be dropped")
	}
}

func TestXLSXColumnLimit(t *testing.T) {
	data := xlsxTestBuild(t, func(f *excelize.File) {
		hdr := make([]any, xlsxMaxCols+20)
		row := make([]any, xlsxMaxCols+20)
		for i := range hdr {
			hdr[i] = fmt.Sprintf("H%d", i+1)
			row[i] = fmt.Sprintf("v%d", i+1)
		}
		xlsxTestSet(t, f, "Sheet1", [][]any{hdr, row})
	})
	segs, err := xlsxTestExtract(t, data)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(segs[0].Text, fmt.Sprintf("H%d: v%d", xlsxMaxCols, xlsxMaxCols)) ||
		strings.Contains(segs[0].Text, fmt.Sprintf("H%d:", xlsxMaxCols+1)) {
		t.Fatalf("column cap not applied: %.200q", segs[0].Text)
	}
}

func TestXLSXLongCellTruncated(t *testing.T) {
	data := xlsxTestBuild(t, func(f *excelize.File) {
		xlsxTestSet(t, f, "Sheet1", [][]any{{"Isi"}, {strings.Repeat("x", xlsxMaxCellRunes*3)}})
	})
	segs, err := xlsxTestExtract(t, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs[0].Text) > xlsxMaxCellRunes+20 {
		t.Fatalf("cell not truncated: %d bytes", len(segs[0].Text))
	}
}

func TestXLSXExtractedCharsLimit(t *testing.T) {
	// 50 sheets x 20,000 rows is far past the cap in rows alone; use a few wide
	// cells per row to cross it quickly.
	cell := strings.Repeat("kata ", 400) // 2000 chars
	data := xlsxTestBuild(t, func(f *excelize.File) {
		rows := [][]any{{"A", "B", "C"}}
		for i := 0; i < 400; i++ {
			rows = append(rows, []any{fmt.Sprint(i) + cell, cell, cell})
		}
		xlsxTestSet(t, f, "Sheet1", rows)
		for s := 2; s <= 8; s++ {
			name := fmt.Sprintf("S%d", s)
			if _, err := f.NewSheet(name); err != nil {
				t.Fatal(err)
			}
			xlsxTestSet(t, f, name, rows)
		}
	})
	if _, err := xlsxTestExtract(t, data); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestXLSXHeaderOnlyIsEmpty(t *testing.T) {
	data := xlsxTestBuild(t, func(f *excelize.File) {
		xlsxTestSet(t, f, "Sheet1", [][]any{{"A", "B"}})
	})
	if _, err := xlsxTestExtract(t, data); !errors.Is(err, ErrEmpty) {
		t.Fatalf("want ErrEmpty, got %v", err)
	}
}

func TestXLSXEmptyInputs(t *testing.T) {
	for _, in := range [][]byte{nil, {}} {
		if _, err := xlsxTestExtract(t, in); !errors.Is(err, ErrEmpty) {
			t.Fatalf("want ErrEmpty, got %v", err)
		}
	}
	blank := xlsxTestBuild(t, func(f *excelize.File) {})
	if _, err := xlsxTestExtract(t, blank); !errors.Is(err, ErrEmpty) {
		t.Fatalf("blank workbook: want ErrEmpty, got %v", err)
	}
}

func TestXLSXNotAWorkbook(t *testing.T) {
	plainZip := docxTestZip(t, docxTestEntry{"hello.txt", "halo"})
	for name, in := range map[string][]byte{
		"text":      []byte("bukan xlsx sama sekali"),
		"pk-prefix": []byte("PK\x03\x04junk"),
		"plain-zip": plainZip,
		"docx":      docxTestFile(t, docxP("hai")),
	} {
		if _, err := xlsxTestExtract(t, in); !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrEmpty) {
			t.Fatalf("%s: want ErrCorrupt/ErrEmpty, got %v", name, err)
		}
	}
	// A plain text file and a truncated zip must be ErrCorrupt specifically.
	if _, err := xlsxTestExtract(t, []byte("bukan xlsx sama sekali")); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
}

func TestXLSXTraversalPathRejected(t *testing.T) {
	good := xlsxTestBuild(t, func(f *excelize.File) { xlsxTestSet(t, f, "Sheet1", [][]any{{"A"}, {"x"}}) })
	zr, err := zip.NewReader(bytes.NewReader(good), int64(len(good)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, zf := range zr.File {
		w, _ := zw.Create(zf.Name)
		rc, _ := zf.Open()
		_, _ = buf.Len(), w
		var tmp bytes.Buffer
		tmp.ReadFrom(rc)
		rc.Close()
		w.Write(tmp.Bytes())
	}
	w, _ := zw.Create("../evil.xml")
	w.Write([]byte("x"))
	zw.Close()
	if _, err := xlsxTestExtract(t, buf.Bytes()); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
}

func TestXLSXBombIsTooLargeWithoutBigAllocation(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("xl/worksheets/sheet1.xml")
	chunk := bytes.Repeat([]byte(" "), 1<<20)
	for i := 0; i < MaxDecompressedBytes>>20+8; i++ {
		w.Write(chunk)
	}
	zw.Close()
	if buf.Len() > MaxUploadBytes {
		t.Fatalf("fixture is not a small bomb: %d bytes", buf.Len())
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := xlsxTestExtract(t, buf.Bytes())
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 96<<20 {
		t.Fatalf("allocated %d MiB while rejecting a bomb", grown>>20)
	}
}

func TestXLSXCanceledContext(t *testing.T) {
	data := xlsxTestBuild(t, func(f *excelize.File) { xlsxTestSet(t, f, "Sheet1", [][]any{{"A"}, {"x"}}) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewXLSXExtractor().Extract(ctx, data); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// xlsxTestReplaceParts preserves a real workbook so rejection is due to the
// unused hostile part, not an unrelated excelize parse failure.
func xlsxTestReplaceParts(t *testing.T, workbook []byte, parts map[string]string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(workbook), int64(len(workbook)))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, entry := range zr.File {
		if _, replace := parts[entry.Name]; replace {
			continue
		}
		if err := zw.Copy(entry); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range parts {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestXLSXPreflightUnusedSharedStringsAndRuns(t *testing.T) {
	good := xlsxTestBuild(t, func(f *excelize.File) {
		xlsxTestSet(t, f, "Sheet1", [][]any{{"Header"}, {"value"}})
	})
	const ns = ` xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"`
	cases := map[string]string{
		// The advertised counts lie; none of the extra strings are referenced.
		"strings": `<sst` + ns + ` count="2" uniqueCount="2"><si><t>Header</t></si><si><t>value</t></si>` +
			strings.Repeat(`<si/>`, xlsxMaxSharedStrings-1) + `</sst>`,
		"runs-per-string": `<sst` + ns + `><si><t>Header</t></si><si><t>value</t></si><si>` +
			strings.Repeat(`<r><t/></r>`, xlsxMaxRunsPerString+1) + `</si></sst>`,
		"total-runs": `<sst` + ns + `><si><t>Header</t></si><si><t>value</t></si>` +
			strings.Repeat(`<si>`+strings.Repeat(`<r/>`, 100)+`</si>`, xlsxMaxRichRuns/100+1) + `</sst>`,
		"individual-string": `<sst` + ns + `><si><t>` + strings.Repeat("a", xlsxMaxStringBytes+1) + `</t></si></sst>`,
		// Splitting text across runs, CDATA and comments must not reset length.
		"fragmented-string": `<sst` + ns + `><si>` + strings.Repeat(`<r><t><![CDATA[`+
			strings.Repeat("a", 1024)+`]]><!--split--></t></r>`, xlsxMaxStringBytes/1024+1) + `</si></sst>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			data := xlsxTestReplaceParts(t, good, map[string]string{"xl/sharedStrings.xml": body})
			segs, err := xlsxTestExtract(t, data)
			if !errors.Is(err, ErrTooLarge) || segs != nil {
				t.Fatalf("want ErrTooLarge and no partial result, got %v, %v", segs, err)
			}
		})
	}
}

func TestXLSXPreflightAllXMLParts(t *testing.T) {
	good := xlsxTestBuild(t, func(f *excelize.File) {
		xlsxTestSet(t, f, "Sheet1", [][]any{{"Header"}, {"value"}})
	})
	for _, part := range []string{"xl/styles.xml", "xl/workbook.xml", "xl/worksheets/sheet1.xml",
		"xl/_rels/workbook.xml.rels", "_rels/.rels", "custom/unused.XML", "custom/unused.RELS"} {
		t.Run(part, func(t *testing.T) {
			body := strings.Repeat("<x>", xlsxMaxXMLDepth+1) + strings.Repeat("</x>", xlsxMaxXMLDepth+1)
			data := xlsxTestReplaceParts(t, good, map[string]string{part: body})
			segs, err := xlsxTestExtract(t, data)
			if !errors.Is(err, ErrTooLarge) || segs != nil {
				t.Fatalf("want ErrTooLarge and no partial result, got %v, %v", segs, err)
			}
		})
	}
}

func TestXLSXPreflightXMLBudgets(t *testing.T) {
	var attrs strings.Builder
	for i := 0; i < xlsxMaxAttrsPerElement; i++ {
		fmt.Fprintf(&attrs, ` a%d="x"`, i)
	}
	cases := map[string]string{
		"elements":               `<x>` + strings.Repeat(`<y/>`, xlsxMaxXMLElements) + `</x>`,
		"tokens":                 `<x>` + strings.Repeat(`<!--x-->`, xlsxMaxXMLTokens) + `</x>`,
		"attributes-per-element": `<x` + attrs.String() + ` extra="x"/>`,
		"attribute-length":       `<x a="` + strings.Repeat("a", xlsxMaxAttributeBytes+1) + `"/>`,
		"attribute-name":         `<x ` + strings.Repeat("a", xlsxMaxAttributeBytes+1) + `="x"/>`,
		"attributes-total": `<x>` + strings.Repeat(`<y`+attrs.String()+`/>`,
			xlsxMaxXMLAttributes/xlsxMaxAttrsPerElement+1) + `</x>`,
		"fragmented-metadata":     `<x>` + strings.Repeat(`a<!--split-->`, xlsxMaxStringBytes+1) + `</x>`,
		"oversized-start-token":   `<x a="` + strings.Repeat("a", xlsxMaxXMLTokenBytes+1) + `"/>`,
		"oversized-comment-token": `<!--` + strings.Repeat("a", xlsxMaxXMLTokenBytes+1) + `-->`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			data := docxTestZip(t, docxTestEntry{"custom/data.xml", body})
			if err := xlsxScanZip(context.Background(), data); !errors.Is(err, ErrTooLarge) {
				t.Fatalf("want ErrTooLarge, got %v", err)
			}
		})
	}
}

func TestXLSXPreflightBudgetsSpanEntries(t *testing.T) {
	// Both parts individually fit. Workbook-wide counts must still reject them.
	for _, tc := range []struct {
		name, item string
		count      int
	}{
		{"strings", `<si/>`, xlsxMaxSharedStrings},
		{"runs", `<r/>`, xlsxMaxRichRuns},
		{"elements", `<y/>`, xlsxMaxXMLElements},
		{"tokens", `<!--x-->`, xlsxMaxXMLTokens},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `<x>` + strings.Repeat(tc.item, tc.count/2+1) + `</x>`
			data := docxTestZip(t, docxTestEntry{"a.xml", body}, docxTestEntry{"b.rels", body})
			if err := xlsxScanZip(context.Background(), data); !errors.Is(err, ErrTooLarge) {
				t.Fatalf("want workbook-wide ErrTooLarge, got %v", err)
			}
		})
	}
}

// Cancel after the preflight has already consumed data, not just at entry.
type xlsxCancelReader struct {
	r      io.Reader
	cancel context.CancelFunc
}

func (r xlsxCancelReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.cancel()
	return n, err
}

func TestXLSXPreflightCancellationWhileStreaming(t *testing.T) {
	for _, isXML := range []bool{false, true} {
		t.Run(fmt.Sprint(isXML), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			remaining := int64(MaxDecompressedBytes)
			r := &xlsxPreflightReader{ctx: ctx, remaining: &remaining, xml: isXML,
				r: xlsxCancelReader{strings.NewReader(`<x>` + strings.Repeat(`<y/>`, 10000) + `</x>`), cancel}}
			var err error
			if isXML {
				err = xlsxScanXML(r, &xlsxXMLBudget{})
			} else {
				_, err = io.Copy(io.Discard, r)
			}
			if !errors.Is(err, context.Canceled) || remaining == MaxDecompressedBytes {
				t.Fatalf("want cancellation after reading, got remaining=%d, err=%v", remaining, err)
			}
		})
	}
}

func TestXLSXPreflightRealDecompressedBytes(t *testing.T) {
	// Many tiny valid tokens avoid the per-token cap. Binary parts use the same
	// real-byte budget, even though there is no XML parser for them.
	for _, part := range []string{"custom/data.xml", "xl/media/image.bin"} {
		t.Run(part, func(t *testing.T) {
			body := `<x>` + strings.Repeat("<!--"+strings.Repeat("a", 4096)+"-->", MaxDecompressedBytes/4096) + `</x>`
			data := docxTestZip(t, docxTestEntry{part, body})
			if len(data) >= MaxUploadBytes {
				t.Fatal("fixture should be a small compressed upload")
			}
			if err := xlsxScanZip(context.Background(), data); !errors.Is(err, ErrTooLarge) {
				t.Fatalf("want ErrTooLarge, got %v", err)
			}
		})
	}
}

func TestXLSXPreflightNormalRichWorkbook(t *testing.T) {
	data := xlsxTestBuild(t, func(f *excelize.File) {
		xlsxTestSet(t, f, "Sheet1", [][]any{{"Header"}})
		if err := f.SetCellRichText("Sheet1", "A2", []excelize.RichTextRun{
			{Text: "Hello ", Font: &excelize.Font{Bold: true}}, {Text: "world"},
		}); err != nil {
			t.Fatal(err)
		}
	})
	segs, err := xlsxTestExtract(t, data)
	if err != nil || len(segs) != 1 || segs[0].Text != "Header: Hello world" {
		t.Fatalf("normal workbook failed: %v, %v", segs, err)
	}
}

func TestXLSXRandomBytesDoNotPanic(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	valid := xlsxTestBuild(t, func(f *excelize.File) {
		xlsxTestSet(t, f, "Sheet1", [][]any{{"Nama", "Kota"}, {"Budi", "Bandung"}})
	})
	for i := 0; i < 150; i++ {
		var in []byte
		if i%2 == 0 {
			in = make([]byte, rng.Intn(2048))
			rng.Read(in)
		} else {
			in = append([]byte(nil), valid...)
			for j := 0; j < 1+rng.Intn(8); j++ {
				in[rng.Intn(len(in))] = byte(rng.Intn(256))
			}
		}
		_, err := xlsxTestExtract(t, in)
		if err != nil && !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrEmpty) && !errors.Is(err, ErrTooLarge) {
			t.Fatalf("iteration %d: unexpected error %v", i, err)
		}
	}
}

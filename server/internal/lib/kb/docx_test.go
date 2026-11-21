package kb

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"math/rand"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const docxTestNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`

type docxTestEntry struct {
	name string
	body string
}

func docxTestZip(t testing.TB, entries ...docxTestEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func docxTestBody(inner string) string {
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document ` + docxTestNS + `><w:body>` + inner + `</w:body></w:document>`
}

func docxTestFile(t testing.TB, inner string, extra ...docxTestEntry) []byte {
	t.Helper()
	entries := []docxTestEntry{
		{"[Content_Types].xml", `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`},
		{"word/document.xml", docxTestBody(inner)},
	}
	return docxTestZip(t, append(entries, extra...)...)
}

func docxP(text string) string {
	return `<w:p><w:r><w:t xml:space="preserve">` + text + `</w:t></w:r></w:p>`
}

func docxH(style, text string) string {
	return `<w:p><w:pPr><w:pStyle w:val="` + style + `"/></w:pPr><w:r><w:t>` + text + `</w:t></w:r></w:p>`
}

func docxTestExtract(t *testing.T, data []byte) ([]Segment, error) {
	t.Helper()
	return NewDOCXExtractor().Extract(context.Background(), data)
}

func TestDOCXFormat(t *testing.T) {
	if NewDOCXExtractor().Format() != FormatDOCX {
		t.Fatal("wrong format")
	}
}

func TestDOCXHeadingPaths(t *testing.T) {
	inner := docxP("Pembuka tanpa judul") +
		docxH("Heading1", "Bab Satu") + docxP("Isi bab satu.") +
		docxH("Heading2", "Bagian 1.1") + docxP("Isi 1.1.") +
		docxH("Heading2", "Bagian 1.2") + docxP("Isi 1.2.") +
		docxH("Heading1", "Bab Dua") + docxP("Isi bab dua.") +
		docxH("Heading3", "Loncat Tiga") + docxP("Isi tiga.")
	segs, err := docxTestExtract(t, docxTestFile(t, inner))
	if err != nil {
		t.Fatal(err)
	}
	want := []Segment{
		{Text: "Pembuka tanpa judul"},
		{Heading: []string{"Bab Satu"}, Text: "Isi bab satu."},
		{Heading: []string{"Bab Satu", "Bagian 1.1"}, Text: "Isi 1.1."},
		{Heading: []string{"Bab Satu", "Bagian 1.2"}, Text: "Isi 1.2."},
		{Heading: []string{"Bab Dua"}, Text: "Isi bab dua."},
		{Heading: []string{"Bab Dua", "Loncat Tiga"}, Text: "Isi tiga."},
	}
	if !reflect.DeepEqual(segs, want) {
		t.Fatalf("got %#v\nwant %#v", segs, want)
	}
}

func TestDOCXTitleAndStylesMapping(t *testing.T) {
	// A localised style id ("Judul1") mapped through styles.xml, plus Title.
	styles := `<w:styles ` + docxTestNS + `>` +
		`<w:style w:type="paragraph" w:styleId="Judul1"><w:name w:val="heading 1"/></w:style>` +
		`<w:style w:type="paragraph" w:styleId="X9"><w:name w:val="Custom"/><w:pPr><w:outlineLvl w:val="1"/></w:pPr></w:style>` +
		`</w:styles>`
	inner := docxH("Title", "Laporan") + docxH("Judul1", "Ringkasan") + docxP("Teks.") +
		docxH("X9", "Rincian") + docxP("Detail.")
	data := docxTestFile(t, inner, docxTestEntry{"word/styles.xml", styles})
	segs, err := docxTestExtract(t, data)
	if err != nil {
		t.Fatal(err)
	}
	want := []Segment{
		{Heading: []string{"Laporan", "Ringkasan"}, Text: "Teks."},
		{Heading: []string{"Laporan", "Ringkasan", "Rincian"}, Text: "Detail."},
	}
	// Title is level 0, so Ringkasan (1) nests under it; X9 (outline 2) under that.
	if !reflect.DeepEqual(segs, want) {
		t.Fatalf("got %#v\nwant %#v", segs, want)
	}
}

func TestDOCXBrokenStylesFallsBackToNames(t *testing.T) {
	data := docxTestFile(t, docxH("Heading1", "Judul")+docxP("Isi."),
		docxTestEntry{"word/styles.xml", "<<<not xml"})
	segs, err := docxTestExtract(t, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || !reflect.DeepEqual(segs[0].Heading, []string{"Judul"}) {
		t.Fatalf("got %#v", segs)
	}
}

func TestDOCXTable(t *testing.T) {
	cell := func(s string) string { return `<w:tc>` + docxP(s) + `</w:tc>` }
	tbl := `<w:tbl>` +
		`<w:tr>` + cell("Nama") + cell("Harga") + `</w:tr>` +
		`<w:tr>` + cell("Kopi") + cell("Rp 15.000") + `</w:tr>` +
		`<w:tr>` + cell("") + cell("") + `</w:tr>` +
		`</w:tbl>`
	segs, err := docxTestExtract(t, docxTestFile(t, docxH("Heading1", "Menu")+tbl+docxP("Catatan.")))
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 {
		t.Fatalf("got %#v", segs)
	}
	want := "Nama | Harga\nKopi | Rp 15.000\nCatatan."
	if segs[0].Text != want {
		t.Fatalf("got %q want %q", segs[0].Text, want)
	}
}

func TestDOCXNestedTableStaysOneLine(t *testing.T) {
	inner := `<w:tbl><w:tr><w:tc>` + docxP("luar") +
		`<w:tbl><w:tr><w:tc>` + docxP("dalam") + `</w:tc></w:tr></w:tbl></w:tc>` +
		`<w:tc>` + docxP("B") + `</w:tc></w:tr></w:tbl>`
	segs, err := docxTestExtract(t, docxTestFile(t, inner))
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || segs[0].Text != "luar dalam | B" {
		t.Fatalf("got %#v", segs)
	}
}

func TestDOCXTabAndBreak(t *testing.T) {
	inner := `<w:p><w:r><w:t>a</w:t><w:tab/><w:t>b</w:t><w:br/><w:t>c</w:t></w:r></w:p>`
	segs, err := docxTestExtract(t, docxTestFile(t, inner))
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || segs[0].Text != "a\tb\nc" {
		t.Fatalf("got %#v", segs)
	}
}

func TestDOCXUnicode(t *testing.T) {
	text := "Selamat pagi, Pak Budi! Rp 1.500.000 😀🇮🇩 ñandú"
	segs, err := docxTestExtract(t, docxTestFile(t, docxP(text)))
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || segs[0].Text != text {
		t.Fatalf("got %#v", segs)
	}
}

func TestDOCXPredefinedEntitiesDecode(t *testing.T) {
	segs, err := docxTestExtract(t, docxTestFile(t, docxP("A &amp; B &lt;c&gt; &#x263A;")))
	if err != nil {
		t.Fatal(err)
	}
	if segs[0].Text != "A & B <c> \u263a" {
		t.Fatalf("got %q", segs[0].Text)
	}
}

func TestDOCXDoctypeAndExternalEntityRejected(t *testing.T) {
	doc := `<?xml version="1.0"?><!DOCTYPE d [<!ENTITY xxe SYSTEM "file:///etc/passwd"><!ENTITY big "BOOM">]>` +
		`<w:document ` + docxTestNS + `><w:body>` + docxP("&xxe; &big;") + `</w:body></w:document>`
	data := docxTestZip(t, docxTestEntry{"word/document.xml", doc})
	segs, err := docxTestExtract(t, data)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
	if len(segs) != 0 {
		t.Fatalf("no text may leak, got %#v", segs)
	}
}

func TestDOCXUndeclaredEntityNotExpanded(t *testing.T) {
	data := docxTestFile(t, docxP("x &custom; y"))
	_, err := docxTestExtract(t, data)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
}

func TestDOCXBombIsTooLargeWithoutBigAllocation(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	head := `<w:document ` + docxTestNS + `><w:body><w:p><w:r><w:t>`
	if _, err := w.Write([]byte(head)); err != nil {
		t.Fatal(err)
	}
	chunk := bytes.Repeat([]byte("a"), 1<<20)
	for i := 0; i < MaxDecompressedBytes>>20+8; i++ {
		if _, err := w.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if buf.Len() > MaxUploadBytes {
		t.Fatalf("fixture is not a small bomb: %d bytes", buf.Len())
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err = docxTestExtract(t, buf.Bytes())
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 96<<20 {
		t.Fatalf("allocated %d MiB while rejecting a bomb", grown>>20)
	}
}

func TestDOCXBombSplitAcrossEntriesSharesBudget(t *testing.T) {
	// styles.xml is read first and spends the shared budget; document.xml then
	// pushes the total past the limit even though each entry alone is allowed.
	half := MaxDecompressedBytes/2 + 1<<20
	pad := func(open, close string) string {
		return open + strings.Repeat(" ", half) + close
	}
	data := docxTestZip(t,
		docxTestEntry{"word/styles.xml", pad(`<w:styles `+docxTestNS+`>`, `</w:styles>`)},
		docxTestEntry{"word/document.xml", pad(`<w:document `+docxTestNS+`><w:body>`+docxP("hai"), `</w:body></w:document>`)},
	)
	_, err := docxTestExtract(t, data)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestDOCXExtractedCharsLimit(t *testing.T) {
	para := docxP(strings.Repeat("kata ", 2000)) // 10k chars
	inner := strings.Repeat(para, MaxExtractedChars/10_000+10)
	_, err := docxTestExtract(t, docxTestFile(t, inner))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestDOCXMissingDocumentXML(t *testing.T) {
	data := docxTestZip(t, docxTestEntry{"[Content_Types].xml", "<Types/>"}, docxTestEntry{"word/other.xml", "<a/>"})
	if _, err := docxTestExtract(t, data); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
}

func TestDOCXNotAZip(t *testing.T) {
	for _, in := range [][]byte{[]byte("plain text, not a zip"), []byte("PK\x03\x04garbage"), {0x50, 0x4b}} {
		if _, err := docxTestExtract(t, in); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("%q: want ErrCorrupt, got %v", in, err)
		}
	}
}

func TestDOCXTraversalPathRejected(t *testing.T) {
	for _, name := range []string{"../evil.xml", "word/../../evil.xml", `..\evil.xml`} {
		data := docxTestFile(t, docxP("hai"), docxTestEntry{name, "x"})
		if _, err := docxTestExtract(t, data); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("%q: want ErrCorrupt, got %v", name, err)
		}
	}
}

func TestDOCXCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewDOCXExtractor().Extract(ctx, docxTestFile(t, docxP("hai")))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestDOCXCancelDuringParse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	inner := strings.Repeat(docxP("hai"), 5000)
	data := docxTestFile(t, inner)
	cancel()
	if _, err := NewDOCXExtractor().Extract(ctx, data); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestDOCXEmptyInputs(t *testing.T) {
	if _, err := docxTestExtract(t, nil); !errors.Is(err, ErrEmpty) {
		t.Fatalf("nil: want ErrEmpty, got %v", err)
	}
	if _, err := docxTestExtract(t, []byte{}); !errors.Is(err, ErrEmpty) {
		t.Fatalf("empty: want ErrEmpty, got %v", err)
	}
	if _, err := docxTestExtract(t, docxTestFile(t, `<w:p/>`+docxP("   "))); !errors.Is(err, ErrEmpty) {
		t.Fatalf("blank doc: want ErrEmpty, got %v", err)
	}
}

func TestDOCXHeadingOnlyDocumentKeepsText(t *testing.T) {
	segs, err := docxTestExtract(t, docxTestFile(t, docxH("Heading1", "Hanya Judul")))
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || segs[0].Text != "Hanya Judul" {
		t.Fatalf("got %#v", segs)
	}
}

func TestDOCXDeepNestingRejected(t *testing.T) {
	inner := strings.Repeat("<w:sdt>", docxMaxDepth+10) + docxP("dalam") + strings.Repeat("</w:sdt>", docxMaxDepth+10)
	if _, err := docxTestExtract(t, docxTestFile(t, inner)); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
}

func TestDOCXMalformedXML(t *testing.T) {
	data := docxTestZip(t, docxTestEntry{"word/document.xml", `<w:document ` + docxTestNS + `><w:body><w:p><w:r><w:t>terpotong`})
	if _, err := docxTestExtract(t, data); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
}

func TestDOCXIgnoresTrackedParagraphStyleChange(t *testing.T) {
	inner := `<w:p><w:pPr><w:pPrChange w:id="1"><w:pPr><w:pStyle w:val="Heading1"/></w:pPr></w:pPrChange></w:pPr>` +
		`<w:r><w:t>biasa</w:t></w:r></w:p>`
	segs, err := docxTestExtract(t, docxTestFile(t, inner))
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 || len(segs[0].Heading) != 0 || segs[0].Text != "biasa" {
		t.Fatalf("got %#v", segs)
	}
}

func TestDOCXRandomBytesDoNotPanic(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	valid := docxTestFile(t, docxH("Heading1", "Judul")+docxP("Isi"))
	for i := 0; i < 300; i++ {
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
		_, err := docxTestExtract(t, in)
		if err != nil && !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrEmpty) && !errors.Is(err, ErrTooLarge) {
			t.Fatalf("iteration %d: unexpected error %v", i, err)
		}
	}
}

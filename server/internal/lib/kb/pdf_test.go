package kb

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"
)

//go:embed testdata/sample.pdf
var pdfSample []byte

//go:embed testdata/notext.pdf
var pdfNoText []byte

//go:embed testdata/manypages.pdf
var pdfManyPages []byte

//go:embed testdata/mixed.pdf
var pdfMixed []byte

func TestPDFFormat(t *testing.T) {
	if NewPDFExtractor().Format() != FormatPDF {
		t.Fatal("wrong format")
	}
}

func TestPDFExtractsTextInReadingOrder(t *testing.T) {
	segs, err := NewPDFExtractor().Extract(context.Background(), pdfSample)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 {
		t.Fatalf("want 1 segment (one page), got %d", len(segs))
	}
	seg := segs[0]
	if seg.Locator != "halaman 1" {
		t.Fatalf("locator = %q", seg.Locator)
	}
	want := strings.Join([]string{
		"Panduan Uji PDF",
		"Ini paragraf pertama untuk pengujian ekstraksi teks dari berkas PDF.",
		"Bagian Kedua",
		"Tabel di bawah berisi data singkat.",
		"Nama Nilai",
		"Alfa 10",
		"Beta 20",
		"• Butir daftar satu",
		"• Butir daftar dua",
		"Penutup paragraf dengan angka 12345 dan simbol %.",
		"1",
	}, "\n")
	if seg.Text != want {
		t.Fatalf("got:\n%q\nwant:\n%q", seg.Text, want)
	}
}

func TestPDFNoReplacementCodepoints(t *testing.T) {
	segs, err := NewPDFExtractor().Extract(context.Background(), pdfSample)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range segs {
		if strings.ContainsRune(s.Text, '�') {
			t.Fatalf("text contains replacement codepoint: %q", s.Text)
		}
	}
}

func TestPDFMetadataReportsCoverage(t *testing.T) {
	segs, meta, err := pdfExtractor{}.ExtractWithMetadata(context.Background(), pdfSample)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Status != "complete" {
		t.Fatalf("status = %q", meta.Status)
	}
	if meta.ExtractorVersion != extractorVersion {
		t.Fatalf("version = %q", meta.ExtractorVersion)
	}
	var pageCov *ExtractionCoverage
	for i := range meta.Coverage {
		if meta.Coverage[i].Unit == "page" {
			pageCov = &meta.Coverage[i]
		}
	}
	if pageCov == nil || pageCov.Total == nil || *pageCov.Total != 1 || pageCov.Processed != 1 {
		t.Fatalf("page coverage = %+v", pageCov)
	}
	if len(segs) != 1 {
		t.Fatalf("segments = %d", len(segs))
	}
}

func TestPDFEmptyInputs(t *testing.T) {
	if _, err := NewPDFExtractor().Extract(context.Background(), nil); !errors.Is(err, ErrEmpty) {
		t.Fatalf("nil: want ErrEmpty, got %v", err)
	}
	if _, err := NewPDFExtractor().Extract(context.Background(), []byte{}); !errors.Is(err, ErrEmpty) {
		t.Fatalf("empty: want ErrEmpty, got %v", err)
	}
}

func TestPDFCorruptIsRejected(t *testing.T) {
	for _, in := range [][]byte{
		[]byte("plain text, not a pdf"),
		[]byte("%PDF-1.4\ngarbage without trailer"),
		{0x25, 0x50, 0x44},
	} {
		if _, err := NewPDFExtractor().Extract(context.Background(), in); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("%q: want ErrCorrupt, got %v", in, err)
		}
	}
}

func TestPDFScannedImageIsEmpty(t *testing.T) {
	// A valid PDF structure with no text layer yields no segments.
	if _, err := NewPDFExtractor().Extract(context.Background(), pdfNoText); !errors.Is(err, ErrEmpty) {
		t.Fatalf("want ErrEmpty, got %v", err)
	}
}

func TestPDFMixedDocumentWarnsOnScannedPage(t *testing.T) {
	segs, meta, err := pdfExtractor{}.ExtractWithMetadata(context.Background(), pdfMixed)
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) != 1 {
		t.Fatalf("segments = %d, want 1 (only the text page)", len(segs))
	}
	hasWarn := false
	for _, w := range meta.Warnings {
		if w == "pdf_page_without_text" {
			hasWarn = true
		}
	}
	if !hasWarn {
		t.Fatalf("warnings = %v, want pdf_page_without_text", meta.Warnings)
	}
	if meta.Status != "limited" {
		t.Fatalf("status = %q, want limited", meta.Status)
	}
}

func TestPDFTruncatedIsCorruptOrEmpty(t *testing.T) {
	if len(pdfSample) < 100 {
		t.Fatal("fixture too small")
	}
	for _, n := range []int{10, 50, len(pdfSample) - 100} {
		_, err := NewPDFExtractor().Extract(context.Background(), pdfSample[:n])
		if err == nil || (!errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrEmpty)) {
			t.Fatalf("n=%d: want ErrCorrupt or ErrEmpty, got %v", n, err)
		}
	}
}

func TestPDFOversizeUploadRejected(t *testing.T) {
	big := make([]byte, MaxUploadBytes+1)
	copy(big, "%PDF-1.4\n")
	if _, err := NewPDFExtractor().Extract(context.Background(), big); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestPDFTooManyPagesRejected(t *testing.T) {
	// A page tree claiming more pages than allowed must be rejected before
	// any page is walked.
	if _, err := NewPDFExtractor().Extract(context.Background(), pdfManyPages); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestPDFCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewPDFExtractor().Extract(ctx, pdfSample); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestPDFRandomBytesDoNotPanic(t *testing.T) {
	for i := 0; i < 200; i++ {
		in := make([]byte, 1+i%2048)
		for j := range in {
			in[j] = byte(i*31 + j)
		}
		_, err := NewPDFExtractor().Extract(context.Background(), in)
		if err != nil && !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrEmpty) && !errors.Is(err, ErrTooLarge) {
			t.Fatalf("iteration %d: unexpected error %v", i, err)
		}
	}
}

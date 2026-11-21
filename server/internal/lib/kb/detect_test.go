package kb

import (
	"archive/zip"
	"bytes"
	"errors"
	"math/rand"
	"testing"
)

func detectTestZip(t testing.TB, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("<x/>")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDetectFormat(t *testing.T) {
	bom := []byte{0xEF, 0xBB, 0xBF}
	tests := []struct {
		name     string
		filename string
		data     []byte
		want     Format
		wantErr  error
	}{
		{"markdown", "a.md", []byte("# Judul\nisi"), FormatMarkdown, nil},
		{"markdown long ext", "a.MARKDOWN", []byte("# Judul"), FormatMarkdown, nil},
		{"txt", "catatan.txt", []byte("halo dunia"), FormatText, nil},
		{"csv", "data.csv", []byte("a,b\n1,2"), FormatCSV, nil},
		{"bom stripped", "a.txt", append(append([]byte{}, bom...), "halo"...), FormatText, nil},
		{"indonesian and emoji", "a.txt", []byte("Selamat pagi, apa kabar? 😀"), FormatText, nil},
		{"pdf", "a.pdf", []byte("%PDF-1.7\n..."), FormatPDF, nil},
		{"pdf disguised as md", "a.md", []byte("%PDF-1.4 x"), FormatPDF, nil},
		{"docx", "a.docx", detectTestZip(t, "[Content_Types].xml", "word/document.xml"), FormatDOCX, nil},
		{"xlsx", "a.xlsx", detectTestZip(t, "xl/workbook.xml"), FormatXLSX, nil},
		{"docx disguised as txt", "a.txt", detectTestZip(t, "word/document.xml"), FormatDOCX, nil},
		{"zip unknown entries", "a.zip", detectTestZip(t, "readme.txt"), "", ErrUnsupportedFormat},
		{"corrupt zip", "a.docx", []byte("PK\x03\x04garbage-garbage"), "", ErrCorrupt},
		{"empty", "a.txt", nil, "", ErrEmpty},
		{"only bom", "a.txt", bom, "", ErrEmpty},
		{"nul byte", "a.txt", []byte("ab\x00cd"), "", ErrUnsupportedFormat},
		{"invalid utf8", "a.txt", []byte{'a', 0xff, 0xfe, 'b'}, "", ErrUnsupportedFormat},
		{"unknown extension", "a.json", []byte("{}"), "", ErrUnsupportedFormat},
		{"no extension", "README", []byte("hello"), "", ErrUnsupportedFormat},
		{"too large", "a.txt", bytes.Repeat([]byte("a"), MaxUploadBytes+1), "", ErrTooLarge},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DetectFormat(tc.filename, tc.data)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("format = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDetectFormatRandomBytesNoPanic(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	for i := 0; i < 500; i++ {
		b := make([]byte, 1+r.Intn(300))
		r.Read(b)
		if i%3 == 0 {
			b = append([]byte("PK\x03\x04"), b...)
		}
		_, _ = DetectFormat("x.md", b)
	}
}

func FuzzDetectFormat(f *testing.F) {
	f.Add("a.md", []byte("# hi"))
	f.Add("a.docx", []byte("PK\x03\x04"))
	f.Add("a.pdf", []byte("%PDF-1.4"))
	f.Fuzz(func(t *testing.T, name string, data []byte) {
		_, _ = DetectFormat(name, data)
	})
}

package kb

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func TestCSVExtractorBasic(t *testing.T) {
	ex := NewCSVExtractor()
	if ex.Format() != FormatCSV {
		t.Fatalf("format = %q", ex.Format())
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"comma", "Nama,Kota\nBudi,Jakarta\nSiti,Bandung\n", "Nama: Budi; Kota: Jakarta\nNama: Siti; Kota: Bandung"},
		{"semicolon", "Nama;Kota\nBudi;Jakarta", "Nama: Budi; Kota: Jakarta"},
		{"tab", "Nama\tKota\nBudi\tJakarta", "Nama: Budi; Kota: Jakarta"},
		{"crlf", "Nama,Kota\r\nBudi,Jakarta\r\n", "Nama: Budi; Kota: Jakarta"},
		{"bom", "\uFEFFNama,Kota\nBudi,Jakarta", "Nama: Budi; Kota: Jakarta"},
		{"non-ascii", "Nama,Catatan\nÉmile,Sudah makan nasi goreng 😀", "Nama: Émile; Catatan: Sudah makan nasi goreng 😀"},
		{"quoted delimiter", "Nama,Alamat\nBudi,\"Jl. A, No. 1\"", "Nama: Budi; Alamat: Jl. A, No. 1"},
		{"lazy quotes", "A,B\nx,y\"z", "A: x; B: y\"z"},
		{"ragged rows", "A,B\n1\n1,2,3", "A: 1\nA: 1; B: 2; Kolom 3: 3"},
		{"empty cells skipped", "A,B,C\n1,,3", "A: 1; C: 3"},
		{"empty header name", "A,\n1,2", "A: 1; Kolom 2: 2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ex.Extract(context.Background(), []byte(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("segments = %d, want 1: %#v", len(got), got)
			}
			if got[0].Text != tc.want {
				t.Fatalf("text = %q, want %q", got[0].Text, tc.want)
			}
		})
	}
}

func TestCSVExtractorGroupsRows(t *testing.T) {
	var b strings.Builder
	b.WriteString("id,nama\n")
	for i := 1; i <= 85; i++ {
		fmt.Fprintf(&b, "%d,n%d\n", i, i)
	}
	got, err := NewCSVExtractor().Extract(context.Background(), []byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	wantLoc := []string{"baris 2-41", "baris 42-81", "baris 82-86"}
	if len(got) != len(wantLoc) {
		t.Fatalf("segments = %d, want %d", len(got), len(wantLoc))
	}
	for i, s := range got {
		if s.Locator != wantLoc[i] {
			t.Fatalf("segment %d locator = %q, want %q", i, s.Locator, wantLoc[i])
		}
	}
	if !strings.HasPrefix(got[1].Text, "id: 41; nama: n41") {
		t.Fatalf("second segment starts %q", got[1].Text[:20])
	}
}

func TestCSVExtractorErrors(t *testing.T) {
	ex := NewCSVExtractor()
	bg := context.Background()
	for _, in := range []string{"", "  \n\n", "\uFEFF"} {
		if _, err := ex.Extract(bg, []byte(in)); !errors.Is(err, ErrEmpty) {
			t.Fatalf("input %q: err = %v, want ErrEmpty", in, err)
		}
	}
	// header only: nothing to index
	if _, err := ex.Extract(bg, []byte("A,B\n")); !errors.Is(err, ErrEmpty) {
		t.Fatalf("header only: err = %v, want ErrEmpty", err)
	}
	if _, err := ex.Extract(bg, []byte(strings.Repeat("a", MaxExtractedChars+1))); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("huge: err = %v, want ErrTooLarge", err)
	}

	wide := strings.Repeat("x,", csvMaxCols) + "x\n1\n"
	if _, err := ex.Extract(bg, []byte(wide)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("wide: err = %v, want ErrTooLarge", err)
	}
	tall := "a\n" + strings.Repeat("1\n", csvMaxRows+1)
	if _, err := ex.Extract(bg, []byte(tall)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("tall: err = %v, want ErrTooLarge", err)
	}
	atLimit := "a\n" + strings.Repeat("1\n", csvMaxRows)
	if _, err := ex.Extract(bg, []byte(atLimit)); err != nil {
		t.Fatalf("at row limit: %v", err)
	}
	// An unterminated quote in strict mode would fail; lazy mode must not panic.
	if _, err := ex.Extract(bg, []byte("a,b\n\"unterminated,1\n")); err != nil && !errors.Is(err, ErrCorrupt) {
		t.Fatalf("unterminated: %v", err)
	}

	canceled, cancel := context.WithCancel(bg)
	cancel()
	if _, err := ex.Extract(canceled, []byte("a\n1")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: err = %v", err)
	}
}

func TestCSVExtractorRandomBytesNoPanic(t *testing.T) {
	ex := NewCSVExtractor()
	r := rand.New(rand.NewSource(11))
	alphabet := []byte("ab,;\t\"\n\r 1")
	for i := 0; i < 500; i++ {
		b := make([]byte, r.Intn(300))
		if i%2 == 0 {
			r.Read(b)
		} else {
			for j := range b {
				b[j] = alphabet[r.Intn(len(alphabet))]
			}
		}
		_, _ = ex.Extract(context.Background(), b)
	}
}

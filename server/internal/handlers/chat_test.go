package handlers

import (
	"strings"
	"testing"
)

func TestDeriveSessionTitle(t *testing.T) {
	cases := map[string]struct {
		content string
		want    string
	}{
		"plain":            {"Halo, apa itu validasi klaim?", "Halo, apa itu validasi klaim?"},
		"collapses spaces": {"  Halo   dunia  ", "Halo dunia"},
		"first line only":  {"Ringkas ini\nlalu detail panjang", "Ringkas ini"},
		"carriage return":  {"Ringkas ini\r\nlalu detail", "Ringkas ini"},
		"empty":            {"   \n  ", defaultSessionTitle},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := deriveSessionTitle(tc.content); got != tc.want {
				t.Fatalf("deriveSessionTitle(%q) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
}

func TestDeriveSessionTitleTruncates(t *testing.T) {
	got := deriveSessionTitle(strings.Repeat("a", 100))
	runes := []rune(got)
	if len(runes) != 61 || runes[60] != '…' {
		t.Fatalf("got %d runes ending %q, want 60 runes + ellipsis", len(runes), string(runes[len(runes)-1]))
	}
}

func TestDeriveSessionTitleRespectsRuneBoundaries(t *testing.T) {
	got := deriveSessionTitle(strings.Repeat("é", 100))
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != 61 {
		t.Fatalf("multibyte truncation produced %q", got)
	}
}

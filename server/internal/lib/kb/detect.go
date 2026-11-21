package kb

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// detectMaxZipEntries bounds how many container entries DetectFormat will
// look at. Real docx/xlsx files hold a few dozen parts.
const detectMaxZipEntries = 4096

var (
	detectPDFMagic = []byte("%PDF-")
	detectZipMagic = []byte("PK\x03\x04")
	detectBOM      = []byte{0xEF, 0xBB, 0xBF}
)

// DetectFormat decides the file kind from its content. The extension is used
// only to tell md/txt/csv apart among valid UTF-8 text; it never overrides
// what the bytes say.
func DetectFormat(filename string, data []byte) (Format, error) {
	if len(data) == 0 {
		return "", ErrEmpty
	}
	if len(data) > MaxUploadBytes {
		return "", ErrTooLarge
	}
	if bytes.HasPrefix(data, detectPDFMagic) {
		return FormatPDF, nil
	}
	if bytes.HasPrefix(data, detectZipMagic) {
		return detectZip(data)
	}

	text := bytes.TrimPrefix(data, detectBOM)
	if len(text) == 0 {
		return "", ErrEmpty
	}
	if bytes.IndexByte(text, 0) >= 0 || !utf8.Valid(text) {
		return "", ErrUnsupportedFormat
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".md", ".markdown":
		return FormatMarkdown, nil
	case ".csv":
		return FormatCSV, nil
	case ".txt":
		return FormatText, nil
	}
	return "", ErrUnsupportedFormat
}

func detectZip(data []byte) (Format, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", ErrCorrupt
	}
	for i, f := range zr.File {
		if i >= detectMaxZipEntries {
			break
		}
		switch f.Name {
		case "word/document.xml":
			return FormatDOCX, nil
		case "xl/workbook.xml":
			return FormatXLSX, nil
		}
	}
	return "", ErrUnsupportedFormat
}

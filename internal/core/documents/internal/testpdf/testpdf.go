// Package testpdf writes the PDF and PNG fixtures shared by the document
// package tests.
//
// It is the Go home of Rust's `documents::test_support` module. The PDF is
// written by hand, with a correct cross-reference table, so it has no test
// dependency beyond the pure-Go reader that parses it.
package testpdf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
)

// TextPDF writes a single-page PDF whose text layer contains lines. The page is
// PDF 1.5 with a Helvetica Type1 font using WinAnsiEncoding and a 595x842
// MediaBox, matching Rust's test_support::text_pdf.
func TextPDF(path string, lines []string) {
	var content bytes.Buffer
	content.WriteString("BT\n/F1 12 Tf\n50 700 Td\n")
	// The same content stream as Rust's test_support::text_pdf: each line is
	// shown, then the next line starts 16 points lower with a relative Td.
	for _, line := range lines {
		content.WriteString("(")
		content.WriteString(escapeText(line))
		content.WriteString(") Tj\n0 -16 Td\n")
	}
	content.WriteString("ET\n")

	writePDF(path, content.Bytes())
}

// EmptyPDF writes a single-page PDF without any text.
func EmptyPDF(path string) {
	writePDF(path, nil)
}

// CraftedPNG writes a PNG that is only a header declaring width x height, with
// no pixel data. The header is enough for the limits reader, which never
// decodes pixels.
func CraftedPNG(path string, width, height uint32) {
	var bytesBuffer bytes.Buffer
	bytesBuffer.WriteString("\x89PNG\r\n\x1a\n")
	bytesBuffer.Write([]byte{0, 0, 0, 13})
	bytesBuffer.WriteString("IHDR")
	var dimensions [8]byte
	binary.BigEndian.PutUint32(dimensions[0:4], width)
	binary.BigEndian.PutUint32(dimensions[4:8], height)
	bytesBuffer.Write(dimensions[:])
	bytesBuffer.Write([]byte{8, 2, 0, 0, 0})
	bytesBuffer.Write([]byte{0, 0, 0, 0})
	bytesBuffer.Write([]byte{0, 0, 0, 0})
	bytesBuffer.WriteString("IEND")

	if err := os.WriteFile(path, bytesBuffer.Bytes(), 0o600); err != nil {
		panic(fmt.Sprintf("write test PNG: %v", err))
	}
}

func escapeText(text string) string {
	escaped := make([]byte, 0, len(text))
	for index := 0; index < len(text); index++ {
		switch text[index] {
		case '\\':
			escaped = append(escaped, '\\', '\\')
		case '(':
			escaped = append(escaped, '\\', '(')
		case ')':
			escaped = append(escaped, '\\', ')')
		default:
			escaped = append(escaped, text[index])
		}
	}
	return string(escaped)
}

func writePDF(path string, content []byte) {
	var document bytes.Buffer
	document.WriteString("%PDF-1.5\n%\xE2\xE3\xCF\xD3\n")

	offsets := make([]int, 7)
	writeObject := func(id int, body string) {
		offsets[id] = document.Len()
		fmt.Fprintf(&document, "%d 0 obj\n%s\nendobj\n", id, body)
	}

	writeObject(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObject(2, "<< /Type /Pages /Kids [6 0 R] /Count 1 >>")
	writeObject(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	writeObject(4, "<< /Font << /F1 3 0 R >> >>")
	offsets[5] = document.Len()
	fmt.Fprintf(&document, "5 0 obj\n<< /Length %d >>\nstream\n", len(content))
	document.Write(content)
	document.WriteString("endstream\nendobj\n")
	writeObject(6, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] "+
		"/Contents 5 0 R /Resources 4 0 R >>")

	startXref := document.Len()
	document.WriteString("xref\n0 7\n")
	document.WriteString("0000000000 65535 f \n")
	for id := 1; id <= 6; id++ {
		fmt.Fprintf(&document, "%010d 00000 n \n", offsets[id])
	}
	document.WriteString("trailer\n<< /Size 7 /Root 1 0 R >>\nstartxref\n")
	fmt.Fprintf(&document, "%d\n%%%%EOF\n", startXref)

	if err := os.WriteFile(path, document.Bytes(), 0o600); err != nil {
		panic(fmt.Sprintf("write test PDF: %v", err))
	}
}

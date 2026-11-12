package pdf

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strconv"
)

// Finish finishes the document and returns its bytes.
func (b *Builder) Finish() []byte {
	pageCount := len(b.pages)
	pageIDs := make([]int, pageCount)
	contentIDs := make([]int, pageCount)
	for index := 0; index < pageCount; index++ {
		pageIDs[index] = 10 + index*2
		contentIDs[index] = 11 + index*2
	}

	const (
		catalogID = 1
		pagesID   = 2
		regularID = 3
		boldID    = 4
	)

	offsets := make(map[int]int)
	out := make([]byte, 0, 1024)
	out = append(out, "%PDF-1.7\n%\x80\x80\x80\x80\n\n"...)

	writeObject := func(id int, body []byte) {
		offsets[id] = len(out)
		out = strconv.AppendInt(out, int64(id), 10)
		out = append(out, " 0 obj\n"...)
		out = append(out, body...)
		out = append(out, "\nendobj\n\n"...)
	}

	writeObject(catalogID, []byte("<<\n  /Type /Catalog\n  /Pages 2 0 R\n>>"))

	var kids bytes.Buffer
	kids.WriteString("<<\n  /Type /Pages\n  /Kids [")
	for index, pageID := range pageIDs {
		if index != 0 {
			kids.WriteByte(' ')
		}
		fmt.Fprintf(&kids, "%d 0 R", pageID)
	}
	kids.WriteString("]\n  /Count ")
	kids.WriteString(strconv.Itoa(pageCount))
	kids.WriteString("\n>>")
	writeObject(pagesID, kids.Bytes())

	for index, content := range b.pages {
		var page bytes.Buffer
		page.WriteString("<<\n  /Type /Page\n  /Parent 2 0 R\n  /MediaBox [")
		page.WriteString(numberString(0.0))
		page.WriteByte(' ')
		page.WriteString(numberString(0.0))
		page.WriteByte(' ')
		page.WriteString(numberString(PageWidth))
		page.WriteByte(' ')
		page.WriteString(numberString(PageHeight))
		page.WriteString("]\n  /Contents ")
		page.WriteString(strconv.Itoa(contentIDs[index]))
		page.WriteString(" 0 R\n  /Resources <<\n    /Font <<\n      /F1 3 0 R\n      /F2 4 0 R\n    >>\n  >>\n>>")
		writeObject(pageIDs[index], page.Bytes())

		var stream bytes.Buffer
		stream.WriteString("<<\n  /Length ")
		stream.WriteString(strconv.Itoa(len(content)))
		stream.WriteString("\n>>\nstream\n")
		stream.Write(content)
		stream.WriteString("\nendstream")
		writeObject(contentIDs[index], stream.Bytes())
	}

	writeObject(regularID, []byte("<<\n  /Type /Font\n  /Subtype /Type1\n  /BaseFont /Helvetica\n  /Encoding /WinAnsiEncoding\n>>"))
	writeObject(boldID, []byte("<<\n  /Type /Font\n  /Subtype /Type1\n  /BaseFont /Helvetica-Bold\n  /Encoding /WinAnsiEncoding\n>>"))

	ids := make([]int, 0, len(offsets))
	for id := range offsets {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	xrefOffset := len(out)
	xrefLength := 1
	if len(ids) != 0 {
		xrefLength = ids[len(ids)-1] + 1
	}

	out = append(out, "xref\n0 "...)
	out = strconv.AppendInt(out, int64(xrefLength), 10)
	out = append(out, '\n')

	if len(ids) == 0 {
		out = appendXrefEntry(out, 0, 65535, false)
	}

	written := 0
	for index, objectID := range ids {
		if written > objectID {
			panic("duplicate indirect reference id")
		}

		for freeID := written; freeID < objectID; freeID++ {
			next := freeID + 1
			if next == objectID {
				for _, usedID := range ids[index:] {
					if next < usedID {
						break
					}
					next = usedID + 1
				}
			}

			generation := 0
			if freeID == 0 {
				generation = 65535
			}
			out = appendXrefEntry(out, next%xrefLength, generation, false)
			written++
		}

		out = appendXrefEntry(out, offsets[objectID], 0, true)
		written++
	}

	out = append(out, "trailer\n<<\n  /Size "...)
	out = strconv.AppendInt(out, int64(xrefLength), 10)
	out = append(out, "\n  /Root 1 0 R\n>>\nstartxref\n"...)
	out = strconv.AppendInt(out, int64(xrefOffset), 10)
	out = append(out, "\n%%EOF"...)

	return out
}

func appendXrefEntry(out []byte, offset, generation int, occupied bool) []byte {
	kind := "f"
	if occupied {
		kind = "n"
	}
	return fmt.Appendf(out, "%010d %05d %s\r\n", offset, generation, kind)
}

// numberString formats a float32 exactly like pdf-writer's primitive writer.
func numberString(value float32) string {
	return string(appendNumber(nil, value))
}

func appendNumber(out []byte, value float32) []byte {
	if float32(toI32(value)) == value {
		return strconv.AppendInt(out, int64(toI32(value)), 10)
	}
	return appendDecimal(out, value)
}

func appendDecimal(out []byte, value float32) []byte {
	absolute := float32(math.Abs(float64(value)))
	if value == 0.0 || (absolute > 1e-6 && absolute < 1e12) {
		start := len(out)
		out = strconv.AppendFloat(out, float64(value), 'f', -1, 32)
		if bytes.IndexByte(out[start:], '.') < 0 {
			out = append(out, '.', '0')
		}
		return out
	}
	return strconv.AppendFloat(out, float64(value), 'f', -1, 32)
}

// toI32 reproduces Rust's saturating `f32 as i32` cast.
func toI32(value float32) int32 {
	switch {
	case math.IsNaN(float64(value)):
		return 0
	case value >= 2147483648.0:
		return math.MaxInt32
	case value <= -2147483648.0:
		return math.MinInt32
	default:
		return int32(value)
	}
}

// literalString serialises bytes the way pdf-writer's Str does.
func literalString(bytes []byte) string {
	ascii := true
	for _, byte := range bytes {
		if byte >= 0x80 {
			ascii = false
			break
		}
	}

	if !ascii {
		out := make([]byte, 0, 2+2*len(bytes))
		out = append(out, '<')
		for _, byte := range bytes {
			out = append(out, hexDigit(byte>>4), hexDigit(byte&0xf))
		}
		out = append(out, '>')
		return string(out)
	}

	out := make([]byte, 0, 2+len(bytes))
	out = append(out, '(')
	balanced := false
	balancedKnown := false
	for _, byte := range bytes {
		switch byte {
		case '(', ')':
			if !balancedKnown {
				balancedKnown = true
				balanced = byte != ')' && isBalanced(bytes)
			}
			if !balanced {
				out = append(out, '\\')
			}
			out = append(out, byte)
		case '\\':
			out = append(out, '\\', '\\')
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		case 0x08:
			out = append(out, '\\', 'b')
		case 0x0c:
			out = append(out, '\\', 'f')
		default:
			if byte >= ' ' && byte <= '~' {
				out = append(out, byte)
			} else {
				out = append(out, '\\', octalDigit(byte>>6), octalDigit((byte>>3)&7), octalDigit(byte&7))
			}
		}
	}
	out = append(out, ')')
	return string(out)
}

func isBalanced(bytes []byte) bool {
	depth := 0
	for _, byte := range bytes {
		switch byte {
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return false
			}
			depth--
		}
	}
	return depth == 0
}

func hexDigit(value byte) byte {
	if value < 10 {
		return '0' + value
	}
	return 'A' + (value - 10)
}

func octalDigit(value byte) byte {
	return '0' + value
}

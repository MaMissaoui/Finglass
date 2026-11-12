package imports

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"strconv"
	"testing"
	"unicode/utf16"

	"golang.org/x/text/encoding/charmap"

	"github.com/MaMissaoui/Finglass/internal/core/civil"
	"github.com/MaMissaoui/Finglass/internal/core/decimal"
)

type civilDate = civil.Date

func dateValue(date civil.Date) *civil.Date {
	return &date
}

func sameDatePointer(left, right *civil.Date) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func intToString(value int) string {
	return strconv.Itoa(value)
}

func dec(t *testing.T, value string) decimal.Decimal {
	t.Helper()
	parsed, err := decimal.Parse(value)
	if err != nil {
		t.Fatalf("decimal.Parse(%q): %v", value, err)
	}
	return parsed
}

func cp1252(t *testing.T, text string) []byte {
	t.Helper()
	encoded, err := charmap.Windows1252.NewEncoder().Bytes([]byte(text))
	if err != nil {
		t.Fatalf("encoding windows-1252: %v", err)
	}
	return encoded
}

func utf16le(text string) []byte {
	units := utf16.Encode([]rune(text))
	result := make([]byte, 0, len(units)*2)
	for _, unit := range units {
		result = binary.LittleEndian.AppendUint16(result, unit)
	}
	return result
}

func utf16be(text string) []byte {
	units := utf16.Encode([]rune(text))
	result := make([]byte, 0, len(units)*2)
	for _, unit := range units {
		result = binary.BigEndian.AppendUint16(result, unit)
	}
	return result
}

type zipEntry struct {
	name   string
	data   []byte
	repeat int
}

func writeZip(entries []zipEntry) []byte {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		file, err := writer.Create(entry.name)
		if err != nil {
			panic(err)
		}
		times := entry.repeat
		if times == 0 {
			times = 1
		}
		for range times {
			if _, err := file.Write(entry.data); err != nil {
				panic(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	return buffer.Bytes()
}

func storedEntry(name string, content string) []byte {
	nameBytes := []byte(name)
	contentBytes := []byte(content)
	checksum := crc32.ChecksumIEEE(contentBytes)
	var result []byte
	result = binary.LittleEndian.AppendUint32(result, 0x0403_4b50)
	result = binary.LittleEndian.AppendUint16(result, 20)
	result = binary.LittleEndian.AppendUint16(result, 0)
	result = binary.LittleEndian.AppendUint16(result, 0)
	result = binary.LittleEndian.AppendUint16(result, 0)
	result = binary.LittleEndian.AppendUint16(result, 0)
	result = binary.LittleEndian.AppendUint32(result, checksum)
	result = append(result, lowBytes(uint64(len(contentBytes)), 4)...)
	result = append(result, lowBytes(uint64(len(contentBytes)), 4)...)
	result = append(result, lowBytes(uint64(len(nameBytes)), 2)...)
	result = binary.LittleEndian.AppendUint16(result, 0)
	result = append(result, nameBytes...)
	result = append(result, contentBytes...)
	return result
}

// lowBytes returns the low count little-endian bytes of value.
func lowBytes(value uint64, count int) []byte {
	var buffer [8]byte
	binary.LittleEndian.PutUint64(buffer[:], value)
	return buffer[:count]
}

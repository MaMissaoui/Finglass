package limits

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// imageHeaderBytes is the number of bytes `imagesize::image_type` reads before
// it looks for a signature.
const imageHeaderBytes = 12

// maxJPEGMarkers bounds the JPEG marker walk. The Rust crate loops forever on a
// crafted marker length below two; this guard turns that hang into ok=false
// without changing any valid or truncated image.
const maxJPEGMarkers = 1 << 20

// ImageDimensions reads width and height from an image file's header without
// decoding any pixels.
//
// It returns ok=false for an unsupported format or a malformed or truncated
// header. Detection order and the readers match `imagesize` 0.15 for JPEG, PNG,
// GIF, TIFF, WebP, BMP and PNM.
func ImageDimensions(path string) (width, height uint32, ok bool) {
	// #nosec G304 -- the caller only passes document paths it selected; this
	// never executes anything and only reads a bounded header.
	file, err := os.Open(path)
	if err != nil {
		return 0, 0, false
	}
	defer func() { _ = file.Close() }()

	return imageDimensions(file)
}

func imageDimensions(file *os.File) (uint32, uint32, bool) {
	var header [imageHeaderBytes]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		return 0, 0, false
	}

	switch {
	case matchesJPEG(header[:]):
		return jpegDimensions(file)
	case matchesPNG(header[:]):
		return pngDimensions(file)
	case matchesGIF(header[:]):
		return gifDimensions(file)
	case matchesTIFF(header[:]):
		return tiffDimensions(file)
	case matchesWebP(header[:]):
		return webPDimensions(file)
	case matchesBMP(header[:]):
		return bmpDimensions(file)
	case matchesPNM(header[:]):
		return pnmDimensions(file)
	default:
		return 0, 0, false
	}
}

func fileName(file string) string {
	name := filepath.Base(file)
	if name == "." || name == string(filepath.Separator) {
		return ""
	}
	return name
}

func matchesJPEG(header []byte) bool {
	return len(header) >= 3 && header[0] == 0xFF && header[1] == 0xD8 && header[2] == 0xFF
}

func matchesPNG(header []byte) bool {
	return len(header) >= 4 && header[0] == 0x89 && header[1] == 'P' && header[2] == 'N' && header[3] == 'G'
}

func matchesGIF(header []byte) bool {
	return len(header) >= 4 && string(header[:4]) == "GIF8"
}

func matchesTIFF(header []byte) bool {
	if len(header) < 4 {
		return false
	}
	isTypeMarker := func(value byte) bool { return value == 0x2A || value == 0x2B }
	return (header[0] == 'I' && header[1] == 'I' && isTypeMarker(header[2]) && header[3] == 0) ||
		(header[0] == 'M' && header[1] == 'M' && header[2] == 0 && isTypeMarker(header[3]))
}

func matchesWebP(header []byte) bool {
	return len(header) >= 12 && string(header[:4]) == "RIFF" && string(header[8:12]) == "WEBP"
}

func matchesBMP(header []byte) bool {
	return len(header) >= 2 && header[0] == 0x42 && header[1] == 0x4D
}

func matchesPNM(header []byte) bool {
	return len(header) >= 2 && header[0] == 'P' && header[1] >= '1' && header[1] <= '6'
}

type byteOrder int

const (
	littleEndian byteOrder = iota
	bigEndian
)

func readU16(reader io.Reader, order byteOrder) (uint16, error) {
	var buffer [2]byte
	if _, err := io.ReadFull(reader, buffer[:]); err != nil {
		return 0, err
	}
	if order == littleEndian {
		return binary.LittleEndian.Uint16(buffer[:]), nil
	}
	return binary.BigEndian.Uint16(buffer[:]), nil
}

func readU24(reader io.Reader, order byteOrder) (uint32, error) {
	var buffer [3]byte
	if _, err := io.ReadFull(reader, buffer[:]); err != nil {
		return 0, err
	}
	if order == littleEndian {
		return uint32(buffer[2])<<16 | uint32(buffer[1])<<8 | uint32(buffer[0]), nil
	}
	return uint32(buffer[0])<<16 | uint32(buffer[1])<<8 | uint32(buffer[2]), nil
}

func readU32(reader io.Reader, order byteOrder) (uint32, error) {
	var buffer [4]byte
	if _, err := io.ReadFull(reader, buffer[:]); err != nil {
		return 0, err
	}
	if order == littleEndian {
		return binary.LittleEndian.Uint32(buffer[:]), nil
	}
	return binary.BigEndian.Uint32(buffer[:]), nil
}

func readU64(reader io.Reader, order byteOrder) (uint64, error) {
	var buffer [8]byte
	if _, err := io.ReadFull(reader, buffer[:]); err != nil {
		return 0, err
	}
	if order == littleEndian {
		return binary.LittleEndian.Uint64(buffer[:]), nil
	}
	return binary.BigEndian.Uint64(buffer[:]), nil
}

func u16FromBytes(buffer []byte, order byteOrder) uint16 {
	if order == littleEndian {
		return binary.LittleEndian.Uint16(buffer[:2])
	}
	return binary.BigEndian.Uint16(buffer[:2])
}

func u32FromBytes(buffer []byte, order byteOrder) uint32 {
	if order == littleEndian {
		return binary.LittleEndian.Uint32(buffer[:4])
	}
	return binary.BigEndian.Uint32(buffer[:4])
}

func jpegDimensions(file *os.File) (uint32, uint32, bool) {
	if _, err := file.Seek(2, io.SeekStart); err != nil {
		return 0, 0, false
	}

	depth := 0
	found := false
	for step := 0; step < maxJPEGMarkers; step++ {
		var marker [2]byte
		if _, err := io.ReadFull(file, marker[:]); err != nil {
			return 0, 0, false
		}
		if marker[0] != 0xFF {
			return 0, 0, false
		}

		page := marker[1]
		isSOF := (page >= 0xC0 && page <= 0xC3) ||
			(page >= 0xC5 && page <= 0xC7) ||
			(page >= 0xC9 && page <= 0xCB) ||
			(page >= 0xCD && page <= 0xCF)
		if isSOF {
			if depth == 0 {
				if _, err := file.Seek(3, io.SeekCurrent); err != nil {
					return 0, 0, false
				}
				found = true
				break
			}
		} else if page == 0xD8 {
			depth++
		} else if page == 0xD9 {
			depth--
			if depth < 0 {
				return 0, 0, false
			}
		}

		pageSize, err := readU16(file, bigEndian)
		if err != nil {
			return 0, 0, false
		}
		if _, err := file.Seek(int64(pageSize)-2, io.SeekCurrent); err != nil {
			return 0, 0, false
		}
	}

	if !found {
		return 0, 0, false
	}

	height, err := readU16(file, bigEndian)
	if err != nil {
		return 0, 0, false
	}
	width, err := readU16(file, bigEndian)
	if err != nil {
		return 0, 0, false
	}
	return uint32(width), uint32(height), true
}

func pngDimensions(file *os.File) (uint32, uint32, bool) {
	if _, err := file.Seek(0x10, io.SeekStart); err != nil {
		return 0, 0, false
	}
	width, err := readU32(file, bigEndian)
	if err != nil {
		return 0, 0, false
	}
	height, err := readU32(file, bigEndian)
	if err != nil {
		return 0, 0, false
	}
	return width, height, true
}

func gifDimensions(file *os.File) (uint32, uint32, bool) {
	if _, err := file.Seek(6, io.SeekStart); err != nil {
		return 0, 0, false
	}
	width, err := readU16(file, littleEndian)
	if err != nil {
		return 0, 0, false
	}
	height, err := readU16(file, littleEndian)
	if err != nil {
		return 0, 0, false
	}
	return uint32(width), uint32(height), true
}

func bmpDimensions(file *os.File) (uint32, uint32, bool) {
	if _, err := file.Seek(0x12, io.SeekStart); err != nil {
		return 0, 0, false
	}
	width, err := readU32(file, littleEndian)
	if err != nil {
		return 0, 0, false
	}
	height, err := readU32(file, littleEndian)
	if err != nil {
		return 0, 0, false
	}
	return width, height, true
}

func webPDimensions(file *os.File) (uint32, uint32, bool) {
	// image_type has already consumed the twelve-byte RIFF header, so the next
	// four bytes are the first chunk's FourCC, exactly as in Rust.
	var chunk [4]byte
	if _, err := io.ReadFull(file, chunk[:]); err != nil {
		return 0, 0, false
	}

	switch chunk[3] {
	case ' ':
		if _, err := file.Seek(0x1A, io.SeekStart); err != nil {
			return 0, 0, false
		}
		width, err := readU16(file, littleEndian)
		if err != nil {
			return 0, 0, false
		}
		height, err := readU16(file, littleEndian)
		if err != nil {
			return 0, 0, false
		}
		return uint32(width), uint32(height), true
	case 'L':
		if _, err := file.Seek(0x15, io.SeekStart); err != nil {
			return 0, 0, false
		}
		dims, err := readU32(file, littleEndian)
		if err != nil {
			return 0, 0, false
		}
		return (dims & 0x3FFF) + 1, ((dims >> 14) & 0x3FFF) + 1, true
	case 'X':
		if _, err := file.Seek(0x18, io.SeekStart); err != nil {
			return 0, 0, false
		}
		width, err := readU24(file, littleEndian)
		if err != nil {
			return 0, 0, false
		}
		height, err := readU24(file, littleEndian)
		if err != nil {
			return 0, 0, false
		}
		return width + 1, height + 1, true
	default:
		return 0, 0, false
	}
}

func tiffDimensions(file *os.File) (uint32, uint32, bool) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 0, 0, false
	}

	var marker [2]byte
	if _, err := io.ReadFull(file, marker[:]); err != nil {
		return 0, 0, false
	}
	var order byteOrder
	switch string(marker[:]) {
	case "II":
		order = littleEndian
	case "MM":
		order = bigEndian
	default:
		return 0, 0, false
	}

	typeMarker, err := readU16(file, order)
	if err != nil {
		return 0, 0, false
	}
	bigTIFF := false
	switch typeMarker {
	case 42:
	case 43:
		bigTIFF = true
	default:
		return 0, 0, false
	}

	if bigTIFF {
		offsetSize, err := readU16(file, order)
		if err != nil || offsetSize != 8 {
			return 0, 0, false
		}
		extraField, err := readU16(file, order)
		if err != nil || extraField != 0 {
			return 0, 0, false
		}
	}

	ifdOffset := uint64(0)
	if bigTIFF {
		ifdOffset, err = readU64(file, order)
	} else {
		var value uint32
		value, err = readU32(file, order)
		ifdOffset = uint64(value)
	}
	if err != nil || ifdOffset == 0 {
		return 0, 0, false
	}

	offset, err := strconv.ParseInt(strconv.FormatUint(ifdOffset, 10), 10, 64)
	if err != nil {
		return 0, 0, false
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return 0, 0, false
	}

	var ifdCount uint64
	if bigTIFF {
		ifdCount, err = readU64(file, order)
	} else {
		var value uint16
		value, err = readU16(file, order)
		ifdCount = uint64(value)
	}
	if err != nil {
		return 0, 0, false
	}

	var width, height *uint32
	ifdValueLength := 4
	if bigTIFF {
		ifdValueLength = 8
	}

	for index := uint64(0); index < ifdCount; index++ {
		tag, err := readU16(file, order)
		if err != nil {
			return 0, 0, false
		}
		kind, err := readU16(file, order)
		if err != nil {
			return 0, 0, false
		}
		if bigTIFF {
			if _, err := readU64(file, order); err != nil {
				return 0, 0, false
			}
		} else if _, err := readU32(file, order); err != nil {
			return 0, 0, false
		}

		var valueBytes int
		switch kind {
		case 1, 2, 6, 7:
			valueBytes = 1
		case 3, 8:
			valueBytes = 2
		case 4, 9, 11, 13:
			valueBytes = 4
		case 5, 10:
			valueBytes = 8
		case 12:
			valueBytes = 8
		case 16, 17, 18:
			if !bigTIFF {
				return 0, 0, false
			}
			valueBytes = 8
		default:
			return 0, 0, false
		}

		var valueBuffer [8]byte
		if _, err := io.ReadFull(file, valueBuffer[:ifdValueLength]); err != nil {
			return 0, 0, false
		}

		var value *uint32
		switch valueBytes {
		case 2:
			parsed := uint32(u16FromBytes(valueBuffer[:], order))
			value = &parsed
		case 4:
			parsed := u32FromBytes(valueBuffer[:], order)
			value = &parsed
		}

		switch tag {
		case 0x100:
			width = value
		case 0x101:
			height = value
		}

		if width != nil && height != nil {
			return *width, *height, true
		}
	}

	return 0, 0, false
}

func pnmDimensions(file *os.File) (uint32, uint32, bool) {
	if _, err := file.Seek(2, io.SeekStart); err != nil {
		return 0, 0, false
	}

	for {
		line, err := readUntilWhitespace(file, 1024)
		if err != nil {
			return 0, 0, false
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			if err := readUntilCapped(file, '\n', 1024); err != nil {
				return 0, 0, false
			}
			continue
		}
		if trimmed == "" {
			continue
		}

		rawWidth := line
		heightLine, err := readUntilWhitespace(file, 1024)
		if err != nil {
			return 0, 0, false
		}
		rawHeight := strings.TrimSpace(heightLine)

		width, widthOK := parseUnsigned(rawWidth)
		height, heightOK := parseUnsigned(rawHeight)
		if widthOK && heightOK {
			return narrowDimension(width, height)
		}
		break
	}

	return 0, 0, false
}

func narrowDimension(width, height uint64) (uint32, uint32, bool) {
	if width > uint64(^uint32(0)) || height > uint64(^uint32(0)) {
		return 0, 0, false
	}
	return uint32(width), uint32(height), true
}

func readUntilWhitespace(reader io.Reader, maxSize int) (string, error) {
	bytes := make([]byte, 0, maxSize)
	amountRead := 0
	seenNonWhitespace := false
	var one [1]byte

	for amountRead < maxSize {
		amountRead++
		if _, err := io.ReadFull(reader, one[:]); err != nil {
			return "", err
		}
		if isASCIIWhitespace(one[0]) {
			if seenNonWhitespace {
				break
			}
			continue
		}
		bytes = append(bytes, one[0])
		seenNonWhitespace = true
	}

	if amountRead >= maxSize {
		return "", errors.New("delimiter not found")
	}
	return string(bytes), nil
}

func readUntilCapped(reader io.Reader, delimiter byte, maxSize int) error {
	amountRead := 0
	var one [1]byte

	for amountRead < maxSize {
		if _, err := io.ReadFull(reader, one[:]); err != nil {
			return err
		}
		if one[0] == delimiter {
			return nil
		}
		amountRead++
	}

	return errors.New("delimiter not found")
}

func isASCIIWhitespace(value byte) bool {
	switch value {
	case ' ', '\t', '\n', '\r', 0x0C:
		return true
	default:
		return false
	}
}

func parseUnsigned(text string) (uint64, bool) {
	if text == "" {
		return 0, false
	}
	if text[0] == '+' {
		text = text[1:]
	}
	if text == "" {
		return 0, false
	}
	for index := 0; index < len(text); index++ {
		if text[index] < '0' || text[index] > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseUint(text, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

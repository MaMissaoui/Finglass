package pdftext

// These helpers make the narrowing conversions of pdf-extract explicit. They
// wrap on overflow on purpose; the comments point gosec at that.

func uint32FromInt64(value int64) uint32 {
	return uint32(value) // #nosec G115 -- wraps on overflow on purpose
}

func uint32FromInt(value int) uint32 {
	return uint32(value) // #nosec G115 -- wraps on overflow on purpose
}

func uint16FromUint32(value uint32) uint16 {
	return uint16(value) // #nosec G115 -- wraps on overflow on purpose
}

func byteFromUint32(value uint32) byte {
	return byte(value) // #nosec G115 -- wraps on overflow on purpose
}

func byteFromInt(value int) byte {
	return byte(value) // #nosec G115 -- wraps on overflow on purpose
}

// tableIndex indexes a 256-entry table as pdf-extract does: a negative or
// too-large code panics there, so it must panic here too.
func tableIndex(code int64) int {
	if code < 0 || code > 255 {
		panic("encoding code out of range")
	}
	return int(code)
}

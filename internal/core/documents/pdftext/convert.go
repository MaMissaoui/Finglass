package pdftext

// These helpers make Rust's `as` conversions explicit. Rust's casts wrap on
// overflow; comments point gosec at that intentional behaviour.

func uint32FromInt64(value int64) uint32 {
	return uint32(value) // #nosec G115 -- mirrors Rust's `as u32`
}

func uint32FromInt(value int) uint32 {
	return uint32(value) // #nosec G115 -- mirrors Rust's `as u32`
}

func uint16FromUint32(value uint32) uint16 {
	return uint16(value) // #nosec G115 -- mirrors Rust's `as u16`
}

func byteFromUint32(value uint32) byte {
	return byte(value) // #nosec G115 -- mirrors Rust's `as u8`
}

func byteFromInt(value int) byte {
	return byte(value) // #nosec G115 -- mirrors Rust's `as u8`
}

// tableIndex mirrors indexing a 256-entry Rust Vec/table with `code as usize`:
// a negative or too-large code panics there, so it must panic here too.
func tableIndex(code int64) int {
	if code < 0 || code > 255 {
		panic("encoding code out of range")
	}
	return int(code)
}

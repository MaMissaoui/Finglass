package pdftext

import "bytes"

// readType1Encoding ports type1-encoding-parser 0.1.1's get_encoding_map:
// scan a Type1 font program for `/Encoding N array ... def` and collect
// `<code> /name put` entries. The parser only needs the clear-text part of
// the program, so scanning stops at `eexec`.
func readType1Encoding(contents []byte) (result map[uint32][]byte) {
	defer func() {
		if recover() != nil {
			// A font program this small tokenizer cannot read falls back to
			// the next encoding rule, as the spec allows for complex input.
			result = nil
		}
	}()

	if index := bytes.Index(contents, []byte("eexec")); index >= 0 {
		contents = contents[:index]
	}
	if len(bytes.TrimSpace(contents)) == 0 {
		return nil
	}

	tokens := scanCMap(contents)
	result = make(map[uint32][]byte)
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if token.kind != cmapOperator || token.op != "array" {
			continue
		}
		if i < 2 || tokens[i-1].kind != cmapInteger || tokens[i-2].kind != cmapName {
			continue
		}
		if string(tokens[i-2].name) != "Encoding" {
			continue
		}
		i++
		for i < len(tokens) {
			current := tokens[i]
			if current.kind == cmapOperator {
				switch current.op {
				case "put":
					if i >= 2 && tokens[i-1].kind == cmapName && tokens[i-2].kind == cmapInteger {
						result[uint32FromInt64(tokens[i-2].integer)] = append([]byte(nil), tokens[i-1].name...)
					}
				case "def":
					i = len(tokens)
					continue
				}
			}
			i++
		}
		break
	}
	return result
}

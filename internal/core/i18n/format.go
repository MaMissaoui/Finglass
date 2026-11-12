package i18n

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Arg is a value passed to Format. It is either a string or an integer.
type Arg struct {
	text  string
	value int64
	isInt bool
}

// Str returns a textual argument, used by %s.
func Str(s string) Arg {
	return Arg{text: s}
}

// Int returns an integer argument, used by %d and plural markers.
func Int(n int64) Arg {
	return Arg{value: n, isInt: true}
}

// Format formats key in lang with args.
//
// This ports FinDesk's Strings.format: %s, %d, positional forms such as %1$s and %2$d,
// %%, and plural markers like {1:account|accounts}. A missing argument or a value of the
// wrong type leaves the placeholder unchanged instead of panicking.
func Format(lang Language, key Key, args ...Arg) string {
	return formatText(lang, Text(lang, key), args)
}

func formatText(lang Language, template string, args []Arg) string {
	withPlurals := applyPlurals(lang, template, args)
	return applyPlaceholders(withPlurals, args)
}

// applyPlurals replaces {n:singular|plural} markers, exactly like FinDesk's plural regex.
func applyPlurals(lang Language, template string, args []Arg) string {
	var out strings.Builder
	out.Grow(len(template))
	i := 0
	for i < len(template) {
		if template[i] == '{' {
			if marker, ok := pluralMarker(template, i); ok {
				chosen := marker.plural
				if idx := marker.index - 1; idx >= 0 && idx < len(args) && args[idx].isInt {
					if lang.IsSingular(args[idx].value) {
						chosen = marker.singular
					}
				}
				out.WriteString(chosen)
				i = marker.end
				continue
			}
		}
		ch, size := utf8.DecodeRuneInString(template[i:])
		out.WriteRune(ch)
		i += size
	}
	return out.String()
}

// pluralMarker is a parsed {n:singular|plural} marker: end byte, 1-based index and both forms.
type pluralMarkerValue struct {
	end      int
	index    int
	singular string
	plural   string
}

func pluralMarker(template string, start int) (pluralMarkerValue, bool) {
	var zero pluralMarkerValue
	i := start + 1
	digitsStart := i
	for i < len(template) && isASCIIDigit(template[i]) {
		i++
	}
	if i == digitsStart || i >= len(template) || template[i] != ':' {
		return zero, false
	}
	index, err := strconv.Atoi(template[digitsStart:i])
	if err != nil {
		return zero, false
	}
	singularStart := i + 1
	i = singularStart
	for i < len(template) && template[i] != '{' && template[i] != '}' && template[i] != '|' {
		i++
	}
	if i >= len(template) || template[i] != '|' {
		return zero, false
	}
	singular := template[singularStart:i]
	pluralStart := i + 1
	i = pluralStart
	for i < len(template) && template[i] != '{' && template[i] != '}' && template[i] != '|' {
		i++
	}
	if i >= len(template) || template[i] != '}' {
		return zero, false
	}
	return pluralMarkerValue{
		end:      i + 1,
		index:    index,
		singular: singular,
		plural:   template[pluralStart:i],
	}, true
}

func applyPlaceholders(template string, args []Arg) string {
	var out strings.Builder
	out.Grow(len(template))
	i := 0
	nextArg := 0
	for i < len(template) {
		if template[i] != '%' {
			ch, size := utf8.DecodeRuneInString(template[i:])
			out.WriteRune(ch)
			i += size
			continue
		}
		if i+1 < len(template) && template[i+1] == '%' {
			out.WriteByte('%')
			i += 2
			continue
		}
		if placeholder, ok := parsePlaceholder(template, i, &nextArg); ok {
			if placeholder.supported {
				if rendered, found := renderArg(placeholder.conversion, placeholder.index, args); found {
					out.WriteString(rendered)
				} else {
					out.WriteString(template[i:placeholder.end])
				}
			} else {
				out.WriteString(template[i:placeholder.end])
			}
			i = placeholder.end
			continue
		}
		out.WriteByte('%')
		i++
	}
	return out.String()
}

type placeholderValue struct {
	end        int
	conversion byte
	index      int
	supported  bool
}

// parsePlaceholder parses a % conversion. nextArg advances only for conversions without an
// explicit index.
func parsePlaceholder(template string, start int, nextArg *int) (placeholderValue, bool) {
	var zero placeholderValue
	i := start + 1
	indexStart := i
	for i < len(template) && isASCIIDigit(template[i]) {
		i++
	}
	explicit := -1
	if i > indexStart && i < len(template) && template[i] == '$' {
		index, err := strconv.Atoi(template[indexStart:i])
		if err != nil {
			return zero, false
		}
		explicit = index
		i++
	} else {
		i = indexStart
	}
	// flags
	for i < len(template) && isFlag(template[i]) {
		i++
	}
	// width
	for i < len(template) && isASCIIDigit(template[i]) {
		i++
	}
	// precision
	if i < len(template) && template[i] == '.' {
		i++
		for i < len(template) && isASCIIDigit(template[i]) {
			i++
		}
	}
	if i >= len(template) {
		return zero, false
	}
	conversion := template[i]
	end := i + 1
	switch conversion {
	case 's', 'd':
		var index int
		if explicit >= 0 {
			if explicit == 0 {
				return zero, false
			}
			index = explicit - 1
		} else {
			index = *nextArg
			(*nextArg)++
		}
		return placeholderValue{end: end, conversion: conversion, index: index, supported: true}, true
	default:
		return placeholderValue{end: end, supported: false}, true
	}
}

func renderArg(conversion byte, index int, args []Arg) (string, bool) {
	if index < 0 || index >= len(args) {
		return "", false
	}
	arg := args[index]
	switch conversion {
	case 's':
		if arg.isInt {
			return strconv.FormatInt(arg.value, 10), true
		}
		return arg.text, true
	case 'd':
		if arg.isInt {
			return strconv.FormatInt(arg.value, 10), true
		}
		return "", false
	default:
		return "", false
	}
}

func isASCIIDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isFlag(c byte) bool {
	switch c {
	case '-', '#', '+', ' ', '0', ',', '(':
		return true
	default:
		return false
	}
}

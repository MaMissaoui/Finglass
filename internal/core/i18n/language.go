package i18n

// Language is a supported UI language.
type Language int

const (
	// En is English.
	En Language = iota
	// De is German.
	De
	// Fr is French.
	Fr
)

// Languages lists every supported language.
var Languages = []Language{En, De, Fr}

// Code returns the ISO code stored in the settings.
func (l Language) Code() string {
	switch l {
	case En:
		return "en"
	case De:
		return "de"
	case Fr:
		return "fr"
	default:
		return ""
	}
}

// NativeName returns the language's own name, as shown in the language picker.
func (l Language) NativeName() string {
	switch l {
	case En:
		return "English"
	case De:
		return "Deutsch"
	case Fr:
		return "Français"
	default:
		return ""
	}
}

// LanguageFromCode parses a stored ISO code, ignoring ASCII case.
func LanguageFromCode(code string) (Language, bool) {
	switch {
	case equalASCIIFold(code, "en"):
		return En, true
	case equalASCIIFold(code, "de"):
		return De, true
	case equalASCIIFold(code, "fr"):
		return Fr, true
	default:
		return En, false
	}
}

// IsSingular reports whether count takes the singular. French also uses it for zero,
// the other languages only for one.
func (l Language) IsSingular(count int64) bool {
	if l == Fr {
		return count == 0 || count == 1
	}
	return count == 1
}

// equalASCIIFold reports whether a and b are equal, folding ASCII letters only.
func equalASCIIFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if lowerASCII(a[i]) != lowerASCII(b[i]) {
			return false
		}
	}
	return true
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

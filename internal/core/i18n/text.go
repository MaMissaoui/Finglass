package i18n

// Text returns the text of key in lang.
func Text(lang Language, key Key) string {
	if key < 0 || int(key) >= NumKeys {
		return ""
	}
	switch lang {
	case En:
		return enTexts[key]
	case De:
		return deTexts[key]
	case Fr:
		return frTexts[key]
	default:
		return ""
	}
}

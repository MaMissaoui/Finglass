package profiles

// NamePrefix starts every registry key that names a profile. It is exported
// for the migration reader, which shares the registry format.
const NamePrefix = namePrefix

// DefaultKey is the registry slug of the default profile. It is exported for
// the migration reader, which shares the registry format.
const DefaultKey = defaultKey

// ParseProperties parses the Java-style registry text.
//
// It is the same parser the registry uses; the migration reads FinDesk's
// profiles.properties through it instead of carrying a second parser.
func ParseProperties(text string) map[string]string {
	return parseProperties(text)
}

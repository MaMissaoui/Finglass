package profiles

import (
	"maps"
	"testing"
)

func TestParsePropertiesIsTheRegistryParser(t *testing.T) {
	text := "name.default=Personal\nname.work=M\\u00fcller\nactive=work\n"
	properties := ParseProperties(text)

	if properties["name.default"] != "Personal" {
		t.Errorf("default = %q", properties["name.default"])
	}
	if properties["name.work"] != "Müller" {
		t.Errorf("work = %q", properties["name.work"])
	}
	if properties["active"] != "work" {
		t.Errorf("active = %q", properties["active"])
	}

	if internal := parseProperties(text); !maps.Equal(properties, internal) {
		t.Errorf("ParseProperties and parseProperties disagree")
	}
}

func TestSharedRegistryConstants(t *testing.T) {
	if NamePrefix != "name." {
		t.Errorf("NamePrefix = %q", NamePrefix)
	}
	if DefaultKey != "default" {
		t.Errorf("DefaultKey = %q", DefaultKey)
	}
}

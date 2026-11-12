package xmltree

import (
	"testing"
)

func TestTreeOperationsFollowRoxmltree(t *testing.T) {
	document, err := Parse(`<root xmlns="urn:x" xmlns:n="urn:n" a="1" n:b="2">` +
		`<child>one<![CDATA[two]]>three<!--c-->four<?pi x?>five<text/>tail</child></root>`)
	if err != nil {
		t.Fatal(err)
	}
	root := document.RootElement()
	if root.TagName() != "root" || root.TagNamespace() != "urn:x" {
		t.Errorf("root = %q/%q", root.TagName(), root.TagNamespace())
	}
	if value, ok := root.Attribute("a"); !ok || value != "1" {
		t.Errorf("attribute a = %q, %v", value, ok)
	}
	if value, ok := root.Attribute("b"); !ok || value != "2" {
		t.Errorf("namespaced attribute b = %q, %v", value, ok)
	}
	if _, ok := root.Attribute("missing"); ok {
		t.Error("found a missing attribute")
	}
	children := root.Children()
	if len(children) != 1 || children[0].TagName() != "child" {
		t.Fatalf("children = %#v", children)
	}
	child := children[0]
	texts := []string{}
	for _, node := range child.Children() {
		if node.IsText() {
			value, _ := node.Text()
			texts = append(texts, value)
		}
	}
	if len(texts) != 4 || texts[0] != "onetwothree" || texts[1] != "four" || texts[2] != "five" || texts[3] != "tail" {
		t.Errorf("text nodes = %q", texts)
	}
	descendants := child.Descendants()
	if len(descendants) == 0 || descendants[0] != child {
		t.Error("descendants does not include the node itself first")
	}
	textElement := descendants[len(descendants)-2]
	if textElement.TagName() != "text" {
		t.Errorf("penultimate descendant = %q", textElement.TagName())
	}
	ancestors := textElement.Ancestors()
	if len(ancestors) < 3 || ancestors[0] != textElement || ancestors[1] != child || ancestors[2] != root {
		t.Error("ancestors are not self, parent, root")
	}
}

func TestAttributeWhitespaceIsNormalised(t *testing.T) {
	document, err := Parse("<a x=\"1\t2\r\n3\r4\"/>")
	if err != nil {
		t.Fatal(err)
	}
	value, _ := document.RootElement().Attribute("x")
	if value != "1 2 3 4" {
		t.Errorf("attribute x = %q, want %q", value, "1 2 3 4")
	}
	document, err = Parse("<a x=\"1&#9;2&#10;3&#13;4&#x9;5\"/>")
	if err != nil {
		t.Fatal(err)
	}
	value, _ = document.RootElement().Attribute("x")
	if value != "1\t2\n3\r4\t5" {
		t.Errorf("referenced whitespace = %q, want %q", value, "1\t2\n3\r4\t5")
	}
}

func TestTextNewlinesAreNormalisedAndEntitiesResolved(t *testing.T) {
	document, err := Parse("<a>x\r\ny\rz&lt;&amp;&#65;</a>")
	if err != nil {
		t.Fatal(err)
	}
	value, ok := document.RootElement().Text()
	if !ok || value != "x\ny\nz<&A" {
		t.Errorf("text = %q, %v", value, ok)
	}
	document, err = Parse("<a><![CDATA[x&#9;y<a>]]></a>")
	if err != nil {
		t.Fatal(err)
	}
	value, ok = document.RootElement().Text()
	if !ok || value != "x&#9;y<a>" {
		t.Errorf("CDATA text = %q, %v", value, ok)
	}
}

func TestNamespacePrefixMustBeDeclared(t *testing.T) {
	if _, err := Parse(`<p:a/>`); err == nil {
		t.Error("undeclared element prefix was accepted")
	}
	if _, err := Parse(`<a p:x="1"/>`); err == nil {
		t.Error("undeclared attribute prefix was accepted")
	}
	if _, err := Parse(`<p:a xmlns:p="urn:x"><p:b/></p:a>`); err != nil {
		t.Errorf("declared prefix rejected: %v", err)
	}
	if _, err := Parse(`<a xmlns="urn:x"><b/></a>`); err != nil {
		t.Errorf("default namespace rejected: %v", err)
	}
}

func TestMalformedInputsAreRejected(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"unclosed tag", "<a><b></a>"},
		{"two roots", "<a/><b/>"},
		{"unknown entity", "<a>&x;</a>"},
		{"undeclared prefix", "<p:a/>"},
		{"text after root", "<a/>x"},
		{"no root", "   "},
	}
	for _, test := range tests {
		if _, err := Parse(test.text); err == nil {
			t.Errorf("%s: parsed without error", test.name)
		}
	}
}

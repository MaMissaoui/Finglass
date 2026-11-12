// Package xmltree is a small read-only XML tree built from encoding/xml
// tokens. It provides exactly the operations the statement parsers use and
// follows roxmltree's text and namespace model.
package xmltree

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const xmlNamespaceURI = "http://www.w3.org/XML/1998/namespace"

// Document is a parsed XML document.
type Document struct {
	Root *Node
}

// Attribute is one attribute of an element. Namespace is the resolved URI, or
// empty for an attribute without a namespace.
type Attribute struct {
	Local     string
	Namespace string
	Value     string
}

// Node is an element or text node.
type Node struct {
	local      string
	namespace  string
	attributes []Attribute
	parent     *Node
	children   []*Node
	text       string
	isText     bool
}

// RootElement returns the document's root element.
func (d *Document) RootElement() *Node {
	return d.Root
}

// IsElement reports whether the node is an element.
func (n *Node) IsElement() bool {
	return !n.isText
}

// IsText reports whether the node is a text node.
func (n *Node) IsText() bool {
	return n.isText
}

// TagName returns the element's local name.
func (n *Node) TagName() string {
	return n.local
}

// TagNamespace returns the element's resolved namespace URI.
func (n *Node) TagNamespace() string {
	return n.namespace
}

// Attribute returns the value of the first attribute with the given local
// name, ignoring namespaces.
func (n *Node) Attribute(name string) (string, bool) {
	for _, attribute := range n.attributes {
		if attribute.Local == name {
			return attribute.Value, true
		}
	}
	return "", false
}

// Attributes returns the element's attributes in document order.
func (n *Node) Attributes() []Attribute {
	return append([]Attribute(nil), n.attributes...)
}

// Children returns the node's direct children in document order.
func (n *Node) Children() []*Node {
	return append([]*Node(nil), n.children...)
}

// Parent returns the node's parent, or nil for the root element.
func (n *Node) Parent() *Node {
	return n.parent
}

// Text returns the node's text. For a text node that is its own text; for an
// element, the text of its first child when that child is a text node.
func (n *Node) Text() (string, bool) {
	if n.isText {
		return n.text, true
	}
	if len(n.children) == 0 || !n.children[0].isText {
		return "", false
	}
	return n.children[0].text, true
}

// Descendants returns the node and every descendant in document order.
func (n *Node) Descendants() []*Node {
	nodes := make([]*Node, 0, 8)
	var walk func(*Node)
	walk = func(current *Node) {
		nodes = append(nodes, current)
		for _, child := range current.children {
			walk(child)
		}
	}
	walk(n)
	return nodes
}

// Ancestors returns the node and its ancestors, nearest first.
func (n *Node) Ancestors() []*Node {
	nodes := make([]*Node, 0, 4)
	for current := n; current != nil; current = current.parent {
		nodes = append(nodes, current)
	}
	return nodes
}

// Parse builds a tree from an XML document, rejecting the malformed inputs
// roxmltree rejects.
func Parse(text string) (*Document, error) {
	decoder := xml.NewDecoder(strings.NewReader(protectAttributeCharacterRefs(text)))
	decoder.Strict = true

	var root *Node
	var stack []*Node
	namespaces := []map[string]string{{"xml": xmlNamespaceURI}}
	seenRoot := false
	canMergeText := false

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			if len(stack) == 0 {
				if seenRoot {
					return nil, errors.New("multiple root elements")
				}
				seenRoot = true
			}
			current := cloneNamespaces(namespaces[len(namespaces)-1])
			for _, attribute := range value.Attr {
				switch {
				case attribute.Name.Space == "xmlns":
					current[attribute.Name.Local] = attribute.Value
				case attribute.Name.Space == "" && attribute.Name.Local == "xmlns":
					current[""] = attribute.Value
				}
			}
			if !namespaceBound(value.Name.Space, current) {
				return nil, fmt.Errorf("unbound namespace prefix %q", value.Name.Space)
			}
			attributes := make([]Attribute, 0, len(value.Attr))
			for _, attribute := range value.Attr {
				if attribute.Name.Space == "xmlns" ||
					(attribute.Name.Space == "" && attribute.Name.Local == "xmlns") {
					continue
				}
				if !namespaceBound(attribute.Name.Space, current) {
					return nil, fmt.Errorf("unbound namespace prefix %q", attribute.Name.Space)
				}
				attributes = append(attributes, Attribute{
					Local:     attribute.Name.Local,
					Namespace: attribute.Name.Space,
					Value:     restoreProtected(normalizeAttribute(attribute.Value)),
				})
			}
			node := &Node{
				local:      value.Name.Local,
				namespace:  value.Name.Space,
				attributes: attributes,
			}
			if len(stack) == 0 {
				root = node
			} else {
				parent := stack[len(stack)-1]
				node.parent = parent
				parent.children = append(parent.children, node)
			}
			stack = append(stack, node)
			namespaces = append(namespaces, current)
			canMergeText = false
		case xml.EndElement:
			stack = stack[:len(stack)-1]
			namespaces = namespaces[:len(namespaces)-1]
			canMergeText = false
		case xml.CharData:
			if len(stack) == 0 {
				if strings.Trim(string(value), " \t\r\n") != "" {
					return nil, errors.New("text after the root element")
				}
				continue
			}
			parent := stack[len(stack)-1]
			if canMergeText && len(parent.children) > 0 && parent.children[len(parent.children)-1].isText {
				parent.children[len(parent.children)-1].text += string(value)
			} else {
				parent.children = append(parent.children, &Node{
					parent: parent,
					text:   string(value),
					isText: true,
				})
			}
			canMergeText = true
		case xml.Comment, xml.ProcInst, xml.Directive:
			canMergeText = false
		}
	}
	if !seenRoot || root == nil {
		return nil, errors.New("missing root element")
	}
	return &Document{Root: root}, nil
}

func cloneNamespaces(source map[string]string) map[string]string {
	clone := make(map[string]string, len(source)+1)
	for prefix, uri := range source {
		clone[prefix] = uri
	}
	return clone
}

func normalizeAttribute(value string) string {
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(value)
}

const (
	protectedTab = '\uE009'
	protectedLF  = '\uE00A'
	protectedCR  = '\uE00D'
)

// protectAttributeCharacterRefs replaces numeric character references to tab,
// LF and CR with private-use sentinels, so normalising the attribute's literal
// whitespace does not also change the referenced characters.
func protectAttributeCharacterRefs(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	index := 0
	for index < len(text) {
		if text[index] != '<' {
			out.WriteByte(text[index])
			index++
			continue
		}
		if strings.HasPrefix(text[index:], "<![CDATA[") {
			end := strings.Index(text[index:], "]]>")
			if end < 0 {
				out.WriteString(text[index:])
				return out.String()
			}
			end += index + len("]]>")
			out.WriteString(text[index:end])
			index = end
			continue
		}
		if strings.HasPrefix(text[index:], "<!--") {
			end := strings.Index(text[index:], "-->")
			if end < 0 {
				out.WriteString(text[index:])
				return out.String()
			}
			end += index + len("-->")
			out.WriteString(text[index:end])
			index = end
			continue
		}
		if index+1 < len(text) && (text[index+1] == '/' || text[index+1] == '!' || text[index+1] == '?') {
			end := tagEnd(text, index)
			out.WriteString(text[index:end])
			index = end
			continue
		}
		end := tagEnd(text, index)
		out.WriteString(protectCharacterRefs(text[index:end]))
		index = end
	}
	return out.String()
}

func tagEnd(text string, start int) int {
	var quote byte
	for index := start + 1; index < len(text); index++ {
		character := text[index]
		if quote != 0 {
			if character == quote {
				quote = 0
			}
			continue
		}
		switch character {
		case '"', '\'':
			quote = character
		case '>':
			return index + 1
		}
	}
	return len(text)
}

func protectCharacterRefs(tag string) string {
	var out strings.Builder
	index := 0
	for index < len(tag) {
		if tag[index] == '&' && index+2 < len(tag) && tag[index+1] == '#' {
			end := strings.IndexByte(tag[index:], ';')
			if end >= 0 {
				reference := tag[index : index+end+1]
				if value, ok := characterRefValue(reference); ok {
					switch value {
					case 9:
						out.WriteRune(protectedTab)
						index += end + 1
						continue
					case 10:
						out.WriteRune(protectedLF)
						index += end + 1
						continue
					case 13:
						out.WriteRune(protectedCR)
						index += end + 1
						continue
					}
				}
			}
		}
		out.WriteByte(tag[index])
		index++
	}
	return out.String()
}

func characterRefValue(reference string) (uint64, bool) {
	if len(reference) < 4 || reference[0] != '&' || reference[1] != '#' || reference[len(reference)-1] != ';' {
		return 0, false
	}
	digits := reference[2 : len(reference)-1]
	base := 10
	if len(digits) > 1 && (digits[0] == 'x' || digits[0] == 'X') {
		base = 16
		digits = digits[1:]
	}
	if digits == "" {
		return 0, false
	}
	value, err := strconv.ParseUint(digits, base, 32)
	if err != nil {
		return 0, false
	}
	return value, true
}

func restoreProtected(value string) string {
	return strings.NewReplacer(
		string(protectedTab), "\t",
		string(protectedLF), "\n",
		string(protectedCR), "\r",
	).Replace(value)
}

func namespaceBound(space string, namespaces map[string]string) bool {
	if space == "" || space == xmlNamespaceURI {
		return true
	}
	if defaultNamespace, ok := namespaces[""]; ok && defaultNamespace == space {
		return true
	}
	for _, uri := range namespaces {
		if uri == space {
			return true
		}
	}
	return false
}

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// The spec is read as yaml.Node rather than into structs or maps, because
// order matters here and a Go map loses it: paths and operations become
// requests in the order the spec lists them, and example bodies keep the
// field order their authors chose.

type specDoc struct {
	root *yaml.Node
}

func parseSpec(content []byte) (*specDoc, error) {
	var file yaml.Node
	if err := yaml.Unmarshal(content, &file); err != nil {
		return nil, fmt.Errorf("parse spec: %w", err)
	}
	if len(file.Content) != 1 || file.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("spec is not a YAML mapping")
	}
	return &specDoc{root: file.Content[0]}, nil
}

// get returns the value under key in a mapping node, or nil.
func get(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// pairs walks a mapping node's entries in document order.
func pairs(node *yaml.Node) [][2]*yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	out := make([][2]*yaml.Node, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		out = append(out, [2]*yaml.Node{node.Content[i], node.Content[i+1]})
	}
	return out
}

func str(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	return node.Value
}

// resolve follows a local $ref, as the spec uses for shared parameters and
// responses. Only "#/..." references occur in it.
func (doc *specDoc) resolve(node *yaml.Node) (*yaml.Node, error) {
	ref := get(node, "$ref")
	if ref == nil {
		return node, nil
	}
	target, ok := strings.CutPrefix(ref.Value, "#/")
	if !ok {
		return nil, fmt.Errorf("unsupported $ref %q", ref.Value)
	}
	current := doc.root
	for segment := range strings.SplitSeq(target, "/") {
		current = get(current, segment)
		if current == nil {
			return nil, fmt.Errorf("unresolved $ref %q", ref.Value)
		}
	}
	return current, nil
}

// nodeJSON renders a YAML value as indented JSON, keeping mapping order.
func nodeJSON(node *yaml.Node) (string, error) {
	var buffer bytes.Buffer
	if err := writeJSON(&buffer, node, ""); err != nil {
		return "", err
	}
	return buffer.String(), nil
}

func writeJSON(buffer *bytes.Buffer, node *yaml.Node, indent string) error {
	switch node.Kind {
	case yaml.DocumentNode:
		return writeJSON(buffer, node.Content[0], indent)
	case yaml.AliasNode:
		return writeJSON(buffer, node.Alias, indent)
	case yaml.MappingNode:
		if len(node.Content) == 0 {
			buffer.WriteString("{}")
			return nil
		}
		buffer.WriteString("{\n")
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, err := json.Marshal(node.Content[i].Value)
			if err != nil {
				return err
			}
			buffer.WriteString(indent + "  ")
			buffer.Write(key)
			buffer.WriteString(": ")
			if err := writeJSON(buffer, node.Content[i+1], indent+"  "); err != nil {
				return err
			}
			if i+2 < len(node.Content) {
				buffer.WriteString(",")
			}
			buffer.WriteString("\n")
		}
		buffer.WriteString(indent + "}")
	case yaml.SequenceNode:
		if len(node.Content) == 0 {
			buffer.WriteString("[]")
			return nil
		}
		buffer.WriteString("[\n")
		for i, item := range node.Content {
			buffer.WriteString(indent + "  ")
			if err := writeJSON(buffer, item, indent+"  "); err != nil {
				return err
			}
			if i+1 < len(node.Content) {
				buffer.WriteString(",")
			}
			buffer.WriteString("\n")
		}
		buffer.WriteString(indent + "]")
	case yaml.ScalarNode:
		return writeScalarJSON(buffer, node)
	default:
		return fmt.Errorf("unsupported YAML node kind %v", node.Kind)
	}
	return nil
}

func writeScalarJSON(buffer *bytes.Buffer, node *yaml.Node) error {
	switch node.ShortTag() {
	case "!!null":
		buffer.WriteString("null")
	case "!!bool":
		value, err := strconv.ParseBool(node.Value)
		if err != nil {
			return err
		}
		buffer.WriteString(strconv.FormatBool(value))
	case "!!int", "!!float":
		buffer.WriteString(node.Value)
	default:
		encoded, err := json.Marshal(node.Value)
		if err != nil {
			return err
		}
		buffer.Write(encoded)
	}
	return nil
}

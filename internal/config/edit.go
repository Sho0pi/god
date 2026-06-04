package config

import (
	"fmt"
	"os"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// SetValues sets one or more dotted keys (e.g. "connectors.telegram.token") in
// the YAML file at path, preserving comments, key order, and unrelated content.
// It validates the edited document with Parse before writing, and backs the old
// file up to path+".bak" — the same validate→backup→write safety the config
// tool uses. Intermediate mappings are created as needed.
func SetValues(path string, kv map[string]any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("parse yaml: %w", err)
	}
	if len(root.Content) == 0 {
		// Empty/blank file — start a fresh top-level mapping.
		root.Kind = yaml.DocumentNode
		root.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return fmt.Errorf("config root is not a mapping")
	}

	for dotted, val := range kv {
		if err := setNode(doc, strings.Split(dotted, "."), val); err != nil {
			return fmt.Errorf("set %q: %w", dotted, err)
		}
	}

	out, err := yaml.Marshal(&root)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}
	if _, err := Parse(out); err != nil {
		return fmt.Errorf("edited config invalid, not saved: %w", err)
	}

	if old, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path+".bak", old, 0o600); err != nil {
			return fmt.Errorf("write backup: %w", err)
		}
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// setNode walks/creates the key path under a mapping node and assigns val at the
// leaf, leaving sibling keys (and their comments) untouched.
func setNode(mapping *yaml.Node, keys []string, val any) error {
	if mapping.Kind != yaml.MappingNode {
		return fmt.Errorf("expected mapping at %q", keys[0])
	}
	key := keys[0]

	// YAML mappings store keys and values as alternating Content entries.
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value != key {
			continue
		}
		v := mapping.Content[i+1]
		if len(keys) == 1 {
			return assignScalar(v, val)
		}
		if v.Kind != yaml.MappingNode {
			*v = yaml.Node{Kind: yaml.MappingNode}
		}
		return setNode(v, keys[1:], val)
	}

	// Key absent — append it (and any missing intermediate mappings).
	kNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	vNode := &yaml.Node{}
	if len(keys) == 1 {
		if err := assignScalar(vNode, val); err != nil {
			return err
		}
	} else {
		vNode.Kind = yaml.MappingNode
		if err := setNode(vNode, keys[1:], val); err != nil {
			return err
		}
	}
	mapping.Content = append(mapping.Content, kNode, vNode)
	return nil
}

// assignScalar overwrites n's value from a Go value while keeping any comments
// attached to the original node.
func assignScalar(n *yaml.Node, val any) error {
	var tmp yaml.Node
	if err := tmp.Encode(val); err != nil {
		return fmt.Errorf("encode value: %w", err)
	}
	head, line, foot := n.HeadComment, n.LineComment, n.FootComment
	*n = tmp
	n.HeadComment, n.LineComment, n.FootComment = head, line, foot
	return nil
}

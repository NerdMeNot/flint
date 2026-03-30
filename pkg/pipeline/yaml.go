package pipeline

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// SecretRef can be a plain string or a map with optional/description fields.
//
//	secrets:
//	  - SECRET_NAME
//	  - OPTIONAL_SECRET:
//	      optional: true
func (s *SecretRef) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		s.Name = node.Value
		return nil
	}

	if node.Kind == yaml.MappingNode && len(node.Content) == 2 {
		s.Name = node.Content[0].Value

		var inner struct {
			Optional    bool   `yaml:"optional"`
			Description string `yaml:"description"`
		}
		if err := node.Content[1].Decode(&inner); err != nil {
			return fmt.Errorf("decoding secret ref %q: %w", s.Name, err)
		}
		s.Optional = inner.Optional
		s.Description = inner.Description
		return nil
	}

	return fmt.Errorf("secret ref must be a string or a single-key map, got %v", node.Kind)
}

// RunnerRef can be a plain string (pool name) or a structured spec.
//
//	runner: standard
//	# or
//	runner:
//	  name: gpu
//	  size: large
func (r *RunnerRef) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		r.Name = node.Value
		return nil
	}

	// Avoid infinite recursion by decoding into an alias type.
	type alias RunnerRef
	var a alias
	if err := node.Decode(&a); err != nil {
		return fmt.Errorf("decoding runner ref: %w", err)
	}
	*r = RunnerRef(a)
	return nil
}

// DoTask is expressed as a single-key map in YAML:
//
//	do:
//	  - compile:
//	      run: go build ./...
//	  - verify:
//	      run: ./bin/server --version
func (d *DoTask) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode || len(node.Content) != 2 {
		return fmt.Errorf("do task must be a single-key map, got %v with %d items", node.Kind, len(node.Content)/2)
	}

	d.Name = node.Content[0].Value

	var inner struct {
		Run string `yaml:"run"`
	}
	if err := node.Content[1].Decode(&inner); err != nil {
		return fmt.Errorf("decoding do task %q: %w", d.Name, err)
	}
	d.Run = inner.Run
	return nil
}

// CacheSpec handles two YAML forms:
//
// Named cache definitions:
//
//	cache:
//	  go-modules:
//	    key: ${{ checksum('go.sum') }}
//	    paths: [/root/go/pkg/mod]
//
// Restore references:
//
//	cache:
//	  restore: [go-modules, go-build]
func (c *CacheSpec) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("cache spec must be a map, got %v", node.Kind)
	}

	// Check if this is a restore-only spec.
	var restoreOnly struct {
		Restore []string `yaml:"restore"`
	}
	if err := node.Decode(&restoreOnly); err == nil && len(restoreOnly.Restore) > 0 {
		c.Restore = restoreOnly.Restore
		return nil
	}

	// Otherwise, treat each key as a named cache entry.
	c.Entries = make(map[string]CacheEntry)
	var raw map[string]CacheEntry
	if err := node.Decode(&raw); err != nil {
		return fmt.Errorf("decoding cache spec: %w", err)
	}
	c.Entries = raw
	return nil
}

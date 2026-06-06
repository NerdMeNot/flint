package pipeline

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// RunCommand represents a step's run field. It accepts either a single string
// or a list of strings in YAML/JSON:
//
//	run: echo hello            # single command
//	run:                       # multiple commands
//	  - npm install
//	  - npm run build
//	  - npm test
//
// When multiple commands are given, they are joined with " && " so that the
// step fails fast on the first non-zero exit.
type RunCommand struct {
	Commands []string
}

// Cmd creates a RunCommand from a single string. Convenience for Go callers
// and tests (pipeline YAML goes through UnmarshalYAML instead).
func Cmd(s string) RunCommand {
	if s == "" {
		return RunCommand{}
	}
	return RunCommand{Commands: []string{s}}
}

// String returns the shell command(s) as a single string.
func (r RunCommand) String() string {
	return strings.Join(r.Commands, " && ")
}

// IsEmpty reports whether no commands are set.
func (r RunCommand) IsEmpty() bool {
	return len(r.Commands) == 0
}

// UnmarshalYAML handles both string and sequence forms.
func (r *RunCommand) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var s string
		if err := node.Decode(&s); err != nil {
			return err
		}
		if s != "" {
			r.Commands = []string{s}
		}
		return nil
	case yaml.SequenceNode:
		var list []string
		if err := node.Decode(&list); err != nil {
			return err
		}
		r.Commands = list
		return nil
	default:
		return fmt.Errorf("run must be a string or list of strings, got %v", node.Kind)
	}
}

// MarshalYAML emits a scalar when there's one command, a sequence when many.
func (r RunCommand) MarshalYAML() (interface{}, error) {
	if len(r.Commands) == 1 {
		return r.Commands[0], nil
	}
	return r.Commands, nil
}

// UnmarshalJSON handles both string and array forms.
func (r *RunCommand) UnmarshalJSON(data []byte) error {
	// Try string first.
	var s string
	if json.Unmarshal(data, &s) == nil {
		if s != "" {
			r.Commands = []string{s}
		}
		return nil
	}
	// Try array.
	var list []string
	if err := json.Unmarshal(data, &list); err != nil {
		return fmt.Errorf("run must be a string or array of strings")
	}
	r.Commands = list
	return nil
}

// MarshalJSON emits a string when there's one command, an array when many.
func (r RunCommand) MarshalJSON() ([]byte, error) {
	if len(r.Commands) == 1 {
		return json.Marshal(r.Commands[0])
	}
	return json.Marshal(r.Commands)
}

// PullRequestTrigger supports a shorthand form:
//
//	pull_request: [main, "release/*"]
//
// Which is equivalent to:
//
//	pull_request:
//	  branches: [main, "release/*"]
func (t *PullRequestTrigger) UnmarshalYAML(node *yaml.Node) error {
	// Shorthand: pull_request: [main, develop]
	if node.Kind == yaml.SequenceNode {
		var branches []string
		if err := node.Decode(&branches); err != nil {
			return err
		}
		t.Branches = branches
		return nil
	}

	// Full form: pull_request: { branches: [...], paths: [...] }
	type alias PullRequestTrigger
	var a alias
	if err := node.Decode(&a); err != nil {
		return err
	}
	*t = PullRequestTrigger(a)
	return nil
}

// unmarshalPromotions handles the promotion field which can be a single
// object or an array.
func unmarshalPromotions(node *yaml.Node) ([]PromotionTrigger, error) {
	// Array form
	if node.Kind == yaml.SequenceNode {
		var list []PromotionTrigger
		if err := node.Decode(&list); err != nil {
			return nil, err
		}
		return list, nil
	}

	// Single object form
	if node.Kind == yaml.MappingNode {
		var single PromotionTrigger
		if err := node.Decode(&single); err != nil {
			return nil, err
		}
		return []PromotionTrigger{single}, nil
	}

	return nil, fmt.Errorf("promotion must be a mapping or sequence, got %v", node.Kind)
}

// UnmarshalYAML for Triggers handles the promotion field specially
// (single object vs array), while delegating the rest to default decoding.
func (t *Triggers) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]

		switch key {
		case "push":
			t.Push = &PushTrigger{}
			if err := val.Decode(t.Push); err != nil {
				return err
			}
		case "pull_request":
			t.PullRequest = &PullRequestTrigger{}
			if err := val.Decode(t.PullRequest); err != nil {
				return err
			}
		case "manual":
			t.Manual = &ManualTrigger{}
			if err := val.Decode(t.Manual); err != nil {
				return err
			}
		case "schedule":
			t.Schedule = &ScheduleTrigger{}
			if err := val.Decode(t.Schedule); err != nil {
				return err
			}
		case "tag":
			t.Tag = &TagTrigger{}
			if err := val.Decode(t.Tag); err != nil {
				return err
			}
		case "promotion":
			promo, err := unmarshalPromotions(val)
			if err != nil {
				return err
			}
			t.Promotion = promo
		case "webhook":
			t.Webhook = &WebhookTrigger{}
			if err := val.Decode(t.Webhook); err != nil {
				return err
			}
		}
	}

	return nil
}

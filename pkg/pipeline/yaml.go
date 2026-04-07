package pipeline

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

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

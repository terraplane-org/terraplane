package terraplaneconfig

import (
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

type ConfigFile struct {
	Environments []Environment `yaml:"environments"`
}

type Environment struct {
	Name   string  `yaml:"name"`
	Agent  string  `yaml:"agent,omitempty"`
	Stacks []Stack `yaml:"stacks"`
}

type Stack struct {
	Name        string   `yaml:"name"`
	Agent       string   `yaml:"agent,omitempty"`
	Dir         string   `yaml:"dir"`
	ToolVersion string   `yaml:"tool_version,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
}

// ResolvedStack is a stack with environment inheritance applied (agent, etc.).
type ResolvedStack struct {
	Environment string
	Name        string
	Agent       string
	Dir         string
	ToolVersion string
	Tags        []string
}

func ParseConfigFile(data []byte) (*ConfigFile, error) {
	if key, err := serverPolicyKey(data); err != nil {
		return nil, fmt.Errorf("parse terraplane config: %w", err)
	} else if key != "" {
		return nil, fmt.Errorf("terraplane.yaml must not set %q; set it in the orchestrator config", key)
	}

	var cfg ConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse terraplane config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// serverPolicyKey reports a gates or apply_requirements key anywhere in the
// document. Those settings belong in the orchestrator config.
func serverPolicyKey(data []byte) (string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", err
	}
	return findPolicyKey(&doc), nil
}

func findPolicyKey(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range n.Content {
			if key := findPolicyKey(child); key != "" {
				return key
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			if key == "gates" || key == "apply_requirements" {
				return key
			}
			if found := findPolicyKey(n.Content[i+1]); found != "" {
				return found
			}
		}
	}
	return ""
}

// Validate checks structural rules: unique stack names, required fields, and
// that every stack resolves to an agent via stack or environment config.
func (c *ConfigFile) Validate() error {
	if len(c.Environments) == 0 {
		return fmt.Errorf("no environments configured")
	}

	seenEnvs := make(map[string]struct{}, len(c.Environments))
	seenStacks := make(map[string]string) // stack name → environment name

	for _, env := range c.Environments {
		name := strings.TrimSpace(env.Name)
		if name == "" {
			return fmt.Errorf("environment name is required")
		}
		if _, ok := seenEnvs[name]; ok {
			return fmt.Errorf("duplicate environment name %q", name)
		}
		seenEnvs[name] = struct{}{}

		if len(env.Stacks) == 0 {
			return fmt.Errorf("environment %q has no stacks", name)
		}

		for _, stack := range env.Stacks {
			stackName := strings.TrimSpace(stack.Name)
			if stackName == "" {
				return fmt.Errorf("stack name is required in environment %q", name)
			}
			if strings.TrimSpace(stack.Dir) == "" {
				return fmt.Errorf("stack %q in environment %q is missing dir", stackName, name)
			}
			if prevEnv, ok := seenStacks[stackName]; ok {
				return fmt.Errorf("stack name %q is not unique (found in environments %q and %q)", stackName, prevEnv, name)
			}
			seenStacks[stackName] = name

			agent := strings.TrimSpace(stack.Agent)
			if agent == "" {
				agent = strings.TrimSpace(env.Agent)
			}
			if agent == "" {
				return fmt.Errorf("stack %q in environment %q has no agent (set agent on the stack or environment)", stackName, name)
			}

			seenTags := make(map[string]struct{}, len(stack.Tags))
			for _, tag := range stack.Tags {
				tag = strings.TrimSpace(tag)
				if tag == "" {
					return fmt.Errorf("stack %q in environment %q has an empty tag", stackName, name)
				}
				if _, ok := seenTags[tag]; ok {
					return fmt.Errorf("stack %q in environment %q has duplicate tag %q", stackName, name, tag)
				}
				seenTags[tag] = struct{}{}
			}
		}
	}
	return nil
}

func (c *ConfigFile) allResolved() []ResolvedStack {
	out := make([]ResolvedStack, 0)
	for _, env := range c.Environments {
		for _, stack := range env.Stacks {
			agent := strings.TrimSpace(stack.Agent)
			if agent == "" {
				agent = strings.TrimSpace(env.Agent)
			}
			var tags []string
			if len(stack.Tags) > 0 {
				tags = make([]string, len(stack.Tags))
				for i, tag := range stack.Tags {
					tags[i] = strings.TrimSpace(tag)
				}
			}
			out = append(out, ResolvedStack{
				Environment: strings.TrimSpace(env.Name),
				Name:        strings.TrimSpace(stack.Name),
				Agent:       agent,
				Dir:         strings.TrimSpace(stack.Dir),
				ToolVersion: strings.TrimSpace(stack.ToolVersion),
				Tags:        tags,
			})
		}
	}
	return out
}

// ResolveStacks returns stacks for the given stack and/or environment selectors.
//
//   - neither set → all stacks
//   - only environments → all stacks in those environments
//   - only stacks → those stacks
//   - both → named stacks that belong to one of the named environments
func (c *ConfigFile) ResolveStacks(stackNames, environmentNames []string) ([]ResolvedStack, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	all := c.allResolved()
	byName := make(map[string]ResolvedStack, len(all))
	byEnv := make(map[string][]ResolvedStack, len(c.Environments))
	for _, stack := range all {
		byName[stack.Name] = stack
		byEnv[stack.Environment] = append(byEnv[stack.Environment], stack)
	}

	if len(stackNames) == 0 && len(environmentNames) == 0 {
		return all, nil
	}

	if len(environmentNames) > 0 {
		for _, envName := range environmentNames {
			if _, ok := byEnv[envName]; !ok {
				return nil, fmt.Errorf("unknown environment %q", envName)
			}
		}
	}

	var resolved []ResolvedStack
	seen := make(map[string]struct{})

	add := func(stack ResolvedStack) {
		if _, ok := seen[stack.Name]; ok {
			return
		}
		seen[stack.Name] = struct{}{}
		resolved = append(resolved, stack)
	}

	switch {
	case len(environmentNames) > 0 && len(stackNames) == 0:
		for _, envName := range environmentNames {
			for _, stack := range byEnv[envName] {
				add(stack)
			}
		}
	case len(environmentNames) == 0 && len(stackNames) > 0:
		for _, name := range stackNames {
			stack, ok := byName[name]
			if !ok {
				return nil, fmt.Errorf("unknown stack %q", name)
			}
			add(stack)
		}
	default:
		allowed := make(map[string]struct{})
		for _, envName := range environmentNames {
			for _, stack := range byEnv[envName] {
				allowed[stack.Name] = struct{}{}
			}
		}
		for _, name := range stackNames {
			stack, ok := byName[name]
			if !ok {
				return nil, fmt.Errorf("unknown stack %q", name)
			}
			if _, ok := allowed[name]; !ok {
				return nil, fmt.Errorf("stack %q is not in environment%s %s", name, plural(len(environmentNames)), quoteList(environmentNames))
			}
			add(stack)
		}
	}

	return resolved, nil
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func quoteList(values []string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%q", v)
	}
	return strings.Join(parts, ", ")
}

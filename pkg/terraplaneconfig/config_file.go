package terraplaneconfig

import (
	"fmt"

	"go.yaml.in/yaml/v3"
)

type ConfigFile struct {
	Stacks []Stack `yaml:"stacks"`
}

type Stack struct {
	Name             string `yaml:"name"`
	Agent            string `yaml:"agent"`
	Dir              string `yaml:"dir"`
	TerraformVersion string `yaml:"terraform_version"`
}

func ParseConfigFile(data []byte) (*ConfigFile, error) {
	var cfg ConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse terraplane config: %w", err)
	}
	return &cfg, nil
}

// ResolveStacks returns stacks matching names. When names is empty, all configured stacks are returned.
func (c *ConfigFile) ResolveStacks(names []string) ([]Stack, error) {
	if len(c.Stacks) == 0 {
		return nil, fmt.Errorf("no stacks configured")
	}
	if len(names) == 0 {
		return c.Stacks, nil
	}

	byName := make(map[string]Stack, len(c.Stacks))
	for _, stack := range c.Stacks {
		byName[stack.Name] = stack
	}

	resolved := make([]Stack, 0, len(names))
	for _, name := range names {
		stack, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("unknown stack %q", name)
		}
		resolved = append(resolved, stack)
	}
	return resolved, nil
}

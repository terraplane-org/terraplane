package orchestratorconfig

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ConfigFile is the orchestrator-side agent and repo configuration.
type ConfigFile struct {
	Agents  map[string]Agent `yaml:"agents"`
	Repos   map[string]Repo  `yaml:"repos"`
	Enabled bool             `yaml:"enabled"`
}

// Agent names the environment variable that holds that agent's token.
type Agent struct {
	TokenEnv string `yaml:"token_env"`
}

// Repo is the environment and stack config for one repo pattern.
// Patterns are path globs: * and ? do not match across /.
type Repo struct {
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

// ResolvedStack is a stack with environment and agent inheritance applied.
type ResolvedStack struct {
	Environment string
	Name        string
	Agent       string
	TokenEnv    string
	Dir         string
	ToolVersion string
	Tags        []string
}

func ParseConfigFile(data []byte) (*ConfigFile, error) {
	var cfg ConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse orchestrator config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate checks agents, repo patterns, and stack structure.
//
// An exact repo pattern outranks a glob. Otherwise the pattern with more
// literal characters wins. Overlapping patterns of equal specificity are rejected.
// Stack names are unique across environments inside a repo pattern, and may
// repeat on a different repo pattern. Every resolved agent must be declared,
// and each agent token_env is unique.
func (c *ConfigFile) Validate() error {
	agents, err := c.normalizedAgents()
	if err != nil {
		return err
	}
	if len(c.Repos) == 0 {
		return fmt.Errorf("no repos configured")
	}

	patterns := make([]string, 0, len(c.Repos))
	for pattern := range c.Repos {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)

	for _, pattern := range patterns {
		if err := validatePattern(pattern); err != nil {
			return err
		}
		if err := validateRepo(pattern, c.Repos[pattern], agents); err != nil {
			return err
		}
	}

	for i := 0; i < len(patterns); i++ {
		for j := i + 1; j < len(patterns); j++ {
			if !patternsOverlap(patterns[i], patterns[j]) {
				continue
			}
			if moreSpecific(patterns[i], patterns[j]) || moreSpecific(patterns[j], patterns[i]) {
				continue
			}
			return fmt.Errorf("repo patterns %q and %q are ambiguous", patterns[i], patterns[j])
		}
	}
	return nil
}

// ResolveStacks matches repo to the most specific repo pattern, then returns
// stacks for the given stack, environment, and tag selectors.
//
// Selectors of the same kind are combined with OR. Different kinds are
// unioned: a stack is included when it matches any selector. With no
// selectors, every stack in the matched repo is returned.
//
// A stack matches the tag selector when it carries any of the named tags.
// Results are ordered as named stacks, then environment stacks, then tag
// matches in config order. Duplicates are dropped.
func (c *ConfigFile) ResolveStacks(repo string, stackNames, environmentNames, tags []string) ([]ResolvedStack, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return nil, fmt.Errorf("repo is required")
	}

	_, matched, err := c.matchRepo(repo)
	if err != nil {
		return nil, err
	}
	agents, err := c.normalizedAgents()
	if err != nil {
		return nil, err
	}

	all := resolveRepo(matched, agents)
	byName := make(map[string]ResolvedStack, len(all))
	byEnv := make(map[string][]ResolvedStack)
	for _, stack := range all {
		byName[stack.Name] = stack
		byEnv[stack.Environment] = append(byEnv[stack.Environment], stack)
	}

	if len(stackNames) == 0 && len(environmentNames) == 0 && len(tags) == 0 {
		return all, nil
	}

	for _, envName := range environmentNames {
		if _, ok := byEnv[envName]; !ok {
			return nil, fmt.Errorf("unknown environment %q", envName)
		}
	}
	if err := knownTags(all, tags); err != nil {
		return nil, err
	}
	for _, name := range stackNames {
		if _, ok := byName[name]; !ok {
			return nil, fmt.Errorf("unknown stack %q", name)
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

	for _, name := range stackNames {
		add(byName[name])
	}
	for _, envName := range environmentNames {
		for _, stack := range byEnv[envName] {
			add(stack)
		}
	}
	if len(tags) > 0 {
		tagSet := make(map[string]struct{}, len(tags))
		for _, tag := range tags {
			tagSet[tag] = struct{}{}
		}
		for _, stack := range all {
			if hasAnyTag(stack, tagSet) {
				add(stack)
			}
		}
	}

	return resolved, nil
}

func knownTags(stacks []ResolvedStack, tags []string) error {
	if len(tags) == 0 {
		return nil
	}
	known := make(map[string]struct{})
	for _, stack := range stacks {
		for _, tag := range stack.Tags {
			known[tag] = struct{}{}
		}
	}
	for _, tag := range tags {
		if _, ok := known[tag]; !ok {
			return fmt.Errorf("unknown tag %q", tag)
		}
	}
	return nil
}

func hasAnyTag(stack ResolvedStack, tags map[string]struct{}) bool {
	for _, tag := range stack.Tags {
		if _, ok := tags[tag]; ok {
			return true
		}
	}
	return false
}

func (c *ConfigFile) normalizedAgents() (map[string]string, error) {
	if len(c.Agents) == 0 {
		return nil, fmt.Errorf("no agents configured")
	}

	names := make([]string, 0, len(c.Agents))
	for name := range c.Agents {
		names = append(names, name)
	}
	sort.Strings(names)

	agents := make(map[string]string, len(names))
	tokens := make(map[string]string, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return nil, fmt.Errorf("agent name is required")
		}
		if _, ok := agents[trimmed]; ok {
			return nil, fmt.Errorf("duplicate agent name %q", trimmed)
		}
		token := strings.TrimSpace(c.Agents[name].TokenEnv)
		if token == "" {
			return nil, fmt.Errorf("agent %q is missing token_env", trimmed)
		}
		if prev, ok := tokens[token]; ok {
			return nil, fmt.Errorf("duplicate token_env %q for agents %q and %q", token, prev, trimmed)
		}
		tokens[token] = trimmed
		agents[trimmed] = token
	}
	return agents, nil
}

func validateRepo(pattern string, repo Repo, agents map[string]string) error {
	if len(repo.Environments) == 0 {
		return fmt.Errorf("repo %q has no environments", pattern)
	}

	seenEnvs := make(map[string]struct{}, len(repo.Environments))
	seenStacks := make(map[string]string)

	for _, env := range repo.Environments {
		name := strings.TrimSpace(env.Name)
		if name == "" {
			return fmt.Errorf("repo %q: environment name is required", pattern)
		}
		if _, ok := seenEnvs[name]; ok {
			return fmt.Errorf("repo %q: duplicate environment name %q", pattern, name)
		}
		seenEnvs[name] = struct{}{}

		envAgent := strings.TrimSpace(env.Agent)
		if envAgent != "" {
			if _, ok := agents[envAgent]; !ok {
				return fmt.Errorf("repo %q: environment %q references unknown agent %q", pattern, name, envAgent)
			}
		}

		if len(env.Stacks) == 0 {
			return fmt.Errorf("repo %q: environment %q has no stacks", pattern, name)
		}

		for _, stack := range env.Stacks {
			stackName := strings.TrimSpace(stack.Name)
			if stackName == "" {
				return fmt.Errorf("repo %q: stack name is required in environment %q", pattern, name)
			}
			if strings.TrimSpace(stack.Dir) == "" {
				return fmt.Errorf("repo %q: stack %q in environment %q is missing dir", pattern, stackName, name)
			}
			if prevEnv, ok := seenStacks[stackName]; ok {
				if prevEnv == name {
					return fmt.Errorf("repo %q: stack name %q is duplicated in environment %q", pattern, stackName, name)
				}
				return fmt.Errorf("repo %q: stack name %q is not unique (found in environments %q and %q)", pattern, stackName, prevEnv, name)
			}
			seenStacks[stackName] = name

			stackAgent := strings.TrimSpace(stack.Agent)
			if stackAgent != "" {
				if _, ok := agents[stackAgent]; !ok {
					return fmt.Errorf("repo %q: stack %q in environment %q references unknown agent %q", pattern, stackName, name, stackAgent)
				}
			}
			if stackAgent == "" && envAgent == "" {
				return fmt.Errorf("repo %q: stack %q in environment %q has no agent (set agent on the stack or environment)", pattern, stackName, name)
			}

			seenTags := make(map[string]struct{}, len(stack.Tags))
			for _, tag := range stack.Tags {
				tag = strings.TrimSpace(tag)
				if tag == "" {
					return fmt.Errorf("repo %q: stack %q in environment %q has an empty tag", pattern, stackName, name)
				}
				if _, ok := seenTags[tag]; ok {
					return fmt.Errorf("repo %q: stack %q in environment %q has duplicate tag %q", pattern, stackName, name, tag)
				}
				seenTags[tag] = struct{}{}
			}
		}
	}
	return nil
}

func (c *ConfigFile) matchRepo(repo string) (string, Repo, error) {
	type hit struct {
		pattern  string
		exact    bool
		literals int
	}

	patterns := make([]string, 0, len(c.Repos))
	for pattern := range c.Repos {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)

	var best *hit
	var tied []string
	for _, pattern := range patterns {
		ok, err := path.Match(pattern, repo)
		if err != nil {
			return "", Repo{}, fmt.Errorf("repo pattern %q: %w", pattern, err)
		}
		if !ok {
			continue
		}
		exact, literals := patternRank(pattern)
		switch {
		case best == nil || outranks(exact, literals, best.exact, best.literals):
			best = &hit{pattern: pattern, exact: exact, literals: literals}
			tied = []string{pattern}
		case exact == best.exact && literals == best.literals:
			tied = append(tied, pattern)
		}
	}
	if best == nil {
		return "", Repo{}, fmt.Errorf("no repo pattern matches %q", repo)
	}
	if len(tied) > 1 {
		return "", Repo{}, fmt.Errorf("repo %q matches multiple patterns with equal specificity: %s", repo, quoteList(tied))
	}
	return best.pattern, c.Repos[best.pattern], nil
}

func resolveRepo(repo Repo, agents map[string]string) []ResolvedStack {
	out := make([]ResolvedStack, 0)
	for _, env := range repo.Environments {
		envName := strings.TrimSpace(env.Name)
		envAgent := strings.TrimSpace(env.Agent)
		for _, stack := range env.Stacks {
			agent := strings.TrimSpace(stack.Agent)
			if agent == "" {
				agent = envAgent
			}
			var tags []string
			if len(stack.Tags) > 0 {
				tags = make([]string, len(stack.Tags))
				for i, tag := range stack.Tags {
					tags[i] = strings.TrimSpace(tag)
				}
			}
			out = append(out, ResolvedStack{
				Environment: envName,
				Name:        strings.TrimSpace(stack.Name),
				Agent:       agent,
				TokenEnv:    agents[agent],
				Dir:         strings.TrimSpace(stack.Dir),
				ToolVersion: strings.TrimSpace(stack.ToolVersion),
				Tags:        tags,
			})
		}
	}
	return out
}

func quoteList(values []string) string {
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = fmt.Sprintf("%q", v)
	}
	return strings.Join(parts, ", ")
}

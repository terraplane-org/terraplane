package orchestratorconfig

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/xyzjace/terraplane/pkg/terraplaneconfig"
	"go.yaml.in/yaml/v3"
)

// ConfigFile is the orchestrator-side agent and repo configuration.
type ConfigFile struct {
	Agents  map[string]Agent `yaml:"agents"`
	Repos   []RepoConfig     `yaml:"repos"`
	Enabled bool             `yaml:"enabled"`
}

// Agent names the environment variable that holds that agent's token.
type Agent struct {
	TokenEnv string `yaml:"token_env"`
}

const (
	gateTypePlan            = "plan"
	requirementTypeApproved = "approved"
)

// Gate is a status Terraplane posts on the pull request commit.
// The git host can require that status before a merge.
type Gate struct {
	Type string `yaml:"type"`
}

// ApplyRequirement is a check Terraplane makes before it starts apply
// for stacks selected by environment, stack name, or tag.
type ApplyRequirement struct {
	Type         string   `yaml:"type"`
	Environments []string `yaml:"environments,omitempty"`
	Stacks       []string `yaml:"stacks,omitempty"`
	Tags         []string `yaml:"tags,omitempty"`
}

// RepoConfig is a terraplane config for the repos matched by Repo. Repo is a
// path glob (see path.Match) compared case-insensitively against the
// owner/name slug; * and ? do not match across /.
//
// Gates and apply requirements are server policy. They are not read from
// terraplane.yaml. Selectors match the terraplane.yaml fetched for the
// pull request.
type RepoConfig struct {
	Repo                        string             `yaml:"repo"`
	Gates                       []Gate             `yaml:"gates,omitempty"`
	ApplyRequirements           []ApplyRequirement `yaml:"apply_requirements,omitempty"`
	terraplaneconfig.ConfigFile `yaml:",inline"`
}

// ResolvedStack is a terraplane stack plus the env var holding its agent's token.
type ResolvedStack struct {
	terraplaneconfig.ResolvedStack
	TokenEnv string
}

// ParseConfigFile decodes and validates a config file. Unknown fields are an error.
func ParseConfigFile(data []byte) (*ConfigFile, error) {
	var cfg ConfigFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse orchestrator config: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("parse orchestrator config: extra YAML document")
		}
		return nil, fmt.Errorf("parse orchestrator config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate checks agents and repos.
//
// Repos are matched in order and the first match wins. Each repo pattern may
// appear once. An exact (wildcard-free) repo listed after a glob that already
// matches it is rejected as unreachable. Each repo body is validated as a
// terraplane config, and every agent it resolves to must be declared.
func (c *ConfigFile) Validate() error {
	if err := c.validateAgents(); err != nil {
		return err
	}
	if len(c.Repos) == 0 {
		return fmt.Errorf("no repos configured")
	}

	seen := make(map[string]struct{}, len(c.Repos))
	for i, rc := range c.Repos {
		if err := validatePattern(rc.Repo); err != nil {
			return err
		}
		key := strings.ToLower(rc.Repo)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("repo %q is listed more than once", rc.Repo)
		}
		seen[key] = struct{}{}

		if isExact(rc.Repo) {
			for _, earlier := range c.Repos[:i] {
				if ok, _ := path.Match(strings.ToLower(earlier.Repo), key); ok {
					return fmt.Errorf("repo %q is unreachable: %q is listed before it and already matches", rc.Repo, earlier.Repo)
				}
			}
		}

		gates, err := normalizeGates(rc.Gates)
		if err != nil {
			return fmt.Errorf("repo %q: %w", rc.Repo, err)
		}
		c.Repos[i].Gates = gates

		reqs, err := normalizeApplyRequirements(rc.ApplyRequirements)
		if err != nil {
			return fmt.Errorf("repo %q: %w", rc.Repo, err)
		}
		c.Repos[i].ApplyRequirements = reqs

		stacks, err := rc.ResolveStacks(nil, nil)
		if err != nil {
			return fmt.Errorf("repo %q: %w", rc.Repo, err)
		}
		for _, stack := range stacks {
			if _, ok := c.Agents[stack.Agent]; !ok {
				return fmt.Errorf("repo %q: stack %q in environment %q references unknown agent %q", rc.Repo, stack.Name, stack.Environment, stack.Agent)
			}
		}
	}
	return nil
}

func (c *ConfigFile) validateAgents() error {
	if len(c.Agents) == 0 {
		return fmt.Errorf("no agents configured")
	}
	names := make([]string, 0, len(c.Agents))
	for name := range c.Agents {
		names = append(names, name)
	}
	sort.Strings(names)

	tokens := make(map[string]string, len(names))
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("agent name is required")
		}
		token := c.Agents[name].TokenEnv
		if strings.TrimSpace(token) == "" {
			return fmt.Errorf("agent %q is missing token_env", name)
		}
		if prev, ok := tokens[token]; ok {
			return fmt.Errorf("duplicate token_env %q for agents %q and %q", token, prev, name)
		}
		tokens[token] = name
	}
	return nil
}

// validatePattern accepts any well-formed path.Match pattern without
// surrounding whitespace.
func validatePattern(pattern string) error {
	if pattern == "" || strings.TrimSpace(pattern) != pattern {
		return fmt.Errorf("repo pattern %q is invalid", pattern)
	}
	if _, err := path.Match(pattern, ""); err != nil {
		return fmt.Errorf("repo pattern %q: %w", pattern, err)
	}
	return nil
}

// isExact reports whether pattern contains no glob metacharacters.
func isExact(pattern string) bool {
	return !strings.ContainsAny(pattern, `*?[\`)
}

// ResolveStacks matches repo to the first matching repo pattern, then filters
// its stacks by the given stack, environment, and tag selectors.
//
// Selectors of the same kind are combined with OR; different kinds are
// combined with AND. A stack is returned when its name is one of stackNames
// (if any), its environment is one of environmentNames (if any), and it
// carries at least one of tags (if any). With no selectors, every stack in
// the matched repo is returned.
//
// Every selector must name something that exists in the matched repo, and
// the filtered result must be non-empty; otherwise an error is returned.
// Results are in config order. The config is assumed to have been validated.
func (c *ConfigFile) ResolveStacks(repo string, stackNames, environmentNames, tags []string) ([]ResolvedStack, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return nil, fmt.Errorf("repo is required")
	}
	matched, err := c.matchRepo(repo)
	if err != nil {
		return nil, err
	}
	stacks, err := matched.ResolveStacks(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("repo %q: %w", matched.Repo, err)
	}

	all := make([]ResolvedStack, len(stacks))
	knownStacks := make(map[string]struct{}, len(stacks))
	knownEnvs := make(map[string]struct{})
	knownTags := make(map[string]struct{})
	for i, stack := range stacks {
		all[i] = ResolvedStack{ResolvedStack: stack, TokenEnv: c.Agents[stack.Agent].TokenEnv}
		knownStacks[stack.Name] = struct{}{}
		knownEnvs[stack.Environment] = struct{}{}
		for _, tag := range stack.Tags {
			knownTags[tag] = struct{}{}
		}
	}

	if len(stackNames) == 0 && len(environmentNames) == 0 && len(tags) == 0 {
		return all, nil
	}
	if err := requireKnown("stack", stackNames, knownStacks); err != nil {
		return nil, err
	}
	if err := requireKnown("environment", environmentNames, knownEnvs); err != nil {
		return nil, err
	}
	if err := requireKnown("tag", tags, knownTags); err != nil {
		return nil, err
	}

	wantStack := toSet(stackNames)
	wantEnv := toSet(environmentNames)
	wantTag := toSet(tags)

	var resolved []ResolvedStack
	for _, stack := range all {
		if len(wantStack) > 0 && !contains(wantStack, stack.Name) {
			continue
		}
		if len(wantEnv) > 0 && !contains(wantEnv, stack.Environment) {
			continue
		}
		if len(wantTag) > 0 && !hasAnyTag(stack.Tags, wantTag) {
			continue
		}
		resolved = append(resolved, stack)
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("no stacks match stacks %q, environments %q, tags %q", stackNames, environmentNames, tags)
	}
	return resolved, nil
}

// PlanGateEnabled reports whether repo posts terraplane/plan statuses.
// A nil config and an unmatched repo report false.
func (c *ConfigFile) PlanGateEnabled(repo string) bool {
	if c == nil {
		return false
	}
	matched, err := c.matchRepo(repo)
	if err != nil {
		return false
	}
	for _, gate := range matched.Gates {
		if gate.Type == gateTypePlan {
			return true
		}
	}
	return false
}

// ApplyRequiresApproval reports whether apply for stack must wait for an
// approved pull request. A nil config and an unmatched repo report false.
func (c *ConfigFile) ApplyRequiresApproval(repo string, stack terraplaneconfig.ResolvedStack) bool {
	if c == nil {
		return false
	}
	matched, err := c.matchRepo(repo)
	if err != nil {
		return false
	}
	for _, req := range matched.ApplyRequirements {
		if req.Type == requirementTypeApproved && req.matches(stack) {
			return true
		}
	}
	return false
}

func (r ApplyRequirement) matches(stack terraplaneconfig.ResolvedStack) bool {
	if len(r.Environments) > 0 && !contains(toSet(r.Environments), stack.Environment) {
		return false
	}
	if len(r.Stacks) > 0 && !contains(toSet(r.Stacks), stack.Name) {
		return false
	}
	if len(r.Tags) > 0 && !hasAnyTag(stack.Tags, toSet(r.Tags)) {
		return false
	}
	return true
}

func normalizeGates(gates []Gate) ([]Gate, error) {
	if gates == nil {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(gates))
	out := make([]Gate, len(gates))
	for i, gate := range gates {
		gate.Type = strings.TrimSpace(gate.Type)
		if gate.Type == "" {
			return nil, fmt.Errorf("gate type is required")
		}
		if gate.Type != gateTypePlan {
			return nil, fmt.Errorf("unknown gate type %q", gate.Type)
		}
		if _, ok := seen[gate.Type]; ok {
			return nil, fmt.Errorf("gate type %q is listed more than once", gate.Type)
		}
		seen[gate.Type] = struct{}{}
		out[i] = gate
	}
	return out, nil
}

func normalizeApplyRequirements(reqs []ApplyRequirement) ([]ApplyRequirement, error) {
	if reqs == nil {
		return nil, nil
	}
	out := make([]ApplyRequirement, len(reqs))
	for i, req := range reqs {
		req.Type = strings.TrimSpace(req.Type)
		if req.Type == "" {
			return nil, fmt.Errorf("apply requirement type is required")
		}
		if req.Type != requirementTypeApproved {
			return nil, fmt.Errorf("unknown apply requirement type %q", req.Type)
		}
		envs, err := normalizeNames("environment", req.Environments)
		if err != nil {
			return nil, err
		}
		stacks, err := normalizeNames("stack", req.Stacks)
		if err != nil {
			return nil, err
		}
		tags, err := normalizeNames("tag", req.Tags)
		if err != nil {
			return nil, err
		}
		if len(envs)+len(stacks)+len(tags) == 0 {
			return nil, fmt.Errorf("apply requirement %q must set environments, stacks, or tags", req.Type)
		}
		req.Environments = envs
		req.Stacks = stacks
		req.Tags = tags
		out[i] = req
	}
	return out, nil
}

func normalizeNames(kind string, values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s name is required", kind)
		}
		if _, ok := seen[value]; ok {
			return nil, fmt.Errorf("duplicate %s %q", kind, value)
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out, nil
}

// matchRepo returns the first repo whose pattern matches the slug,
// comparing case-insensitively.
func (c *ConfigFile) matchRepo(repo string) (*RepoConfig, error) {
	slug := strings.ToLower(repo)
	for i := range c.Repos {
		if ok, _ := path.Match(strings.ToLower(c.Repos[i].Repo), slug); ok {
			return &c.Repos[i], nil
		}
	}
	return nil, fmt.Errorf("no repo pattern matches %q", repo)
}

func requireKnown(kind string, wanted []string, known map[string]struct{}) error {
	for _, name := range wanted {
		if _, ok := known[name]; !ok {
			return fmt.Errorf("unknown %s %q", kind, name)
		}
	}
	return nil
}

func toSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		set[v] = struct{}{}
	}
	return set
}

func contains(set map[string]struct{}, value string) bool {
	_, ok := set[value]
	return ok
}

func hasAnyTag(tags []string, want map[string]struct{}) bool {
	for _, tag := range tags {
		if contains(want, tag) {
			return true
		}
	}
	return false
}

package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

const WorkspaceConfigName = "couchfusion.yaml"

var ErrInvalidConfig = errors.New("invalid CouchFusion configuration")

type Source string

const (
	SourceEmbedded  Source = "embedded"
	SourceGlobal    Source = "global"
	SourceWorkspace Source = "workspace"
)

type SourceMap map[string]Source

type Effective struct {
	Config  Config    `json:"config"`
	Sources SourceMap `json:"sources"`
}

type Override struct {
	Repos     map[string]RepoOverride   `yaml:"repos,omitempty" json:"repos,omitempty"`
	Modules   map[string]ModuleOverride `yaml:"modules,omitempty" json:"modules,omitempty"`
	Workspace *WorkspaceOverride        `yaml:"workspace,omitempty" json:"workspace,omitempty"`
	Prompts   *PromptOverride           `yaml:"prompts,omitempty" json:"prompts,omitempty"`
}

type RepoOverride struct {
	URL        *string `yaml:"url,omitempty" json:"url,omitempty"`
	Branch     *string `yaml:"branch,omitempty" json:"branch,omitempty"`
	Protocol   *string `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	AuthPrompt *bool   `yaml:"authPrompt,omitempty" json:"authPrompt,omitempty"`
}

type ModuleOverride struct {
	Description *string `yaml:"description,omitempty" json:"description,omitempty"`
	Extends     *string `yaml:"extends,omitempty" json:"extends,omitempty"`
}

type WorkspaceOverride struct {
	DefaultRoot *string `yaml:"defaultRoot,omitempty" json:"defaultRoot,omitempty"`
}

type PromptOverride struct {
	DefaultLayerSelection *[]string `yaml:"defaultLayerSelection,omitempty" json:"defaultLayerSelection,omitempty"`
}

func LoadEffective(globalPath, workspaceRoot string) (Effective, error) {
	base, sources, err := loadEmbeddedConfig()
	if err != nil {
		return Effective{}, err
	}

	if strings.TrimSpace(globalPath) == "" {
		globalPath, err = resolvePath("")
		if err != nil {
			return Effective{}, err
		}
	}
	global, exists, err := loadOverride(globalPath)
	if err != nil {
		return Effective{}, fmt.Errorf("load global config: %w", err)
	}
	if exists {
		applyOverride(&base, sources, global, SourceGlobal)
	}

	if strings.TrimSpace(workspaceRoot) != "" {
		workspacePath := filepath.Join(workspaceRoot, WorkspaceConfigName)
		workspace, exists, err := loadOverride(workspacePath)
		if err != nil {
			return Effective{}, fmt.Errorf("load workspace config: %w", err)
		}
		if exists {
			applyOverride(&base, sources, workspace, SourceWorkspace)
		}
	}
	if err := validateEffective(&base); err != nil {
		return Effective{}, err
	}
	return Effective{Config: base, Sources: sources}, nil
}

func loadEmbeddedConfig() (Config, SourceMap, error) {
	var cfg Config
	if err := yaml.Unmarshal(embeddedDefaultConfig, &cfg); err != nil {
		return Config{}, nil, fmt.Errorf("parse embedded config: %w", err)
	}
	cfg.normalizeRepoKeys()
	if err := validateEffective(&cfg); err != nil {
		return Config{}, nil, err
	}
	return cfg, sourcesForConfig(cfg, SourceEmbedded), nil
}

func loadOverride(path string) (Override, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Override{}, false, nil
		}
		return Override{}, false, err
	}
	var override Override
	if err := yaml.Unmarshal(data, &override); err != nil {
		return Override{}, false, err
	}
	return override, true, nil
}

func applyOverride(cfg *Config, sources SourceMap, override Override, source Source) {
	if cfg.Repos == nil {
		cfg.Repos = map[string]RepoConfig{}
	}
	for key, values := range override.Repos {
		repo := cfg.Repos[key]
		if values.URL != nil {
			repo.URL = *values.URL
			sources["repos."+key+".url"] = source
		}
		if values.Branch != nil {
			repo.Branch = *values.Branch
			sources["repos."+key+".branch"] = source
		}
		if values.Protocol != nil {
			repo.Protocol = *values.Protocol
			sources["repos."+key+".protocol"] = source
		}
		if values.AuthPrompt != nil {
			repo.AuthPrompt = *values.AuthPrompt
			sources["repos."+key+".authPrompt"] = source
		}
		cfg.Repos[key] = repo
	}
	if cfg.Modules == nil {
		cfg.Modules = map[string]ModuleConfig{}
	}
	for key, values := range override.Modules {
		module := cfg.Modules[key]
		if values.Description != nil {
			module.Description = *values.Description
			sources["modules."+key+".description"] = source
		}
		if values.Extends != nil {
			module.Extends = *values.Extends
			sources["modules."+key+".extends"] = source
		}
		cfg.Modules[key] = module
	}
	if override.Workspace != nil && override.Workspace.DefaultRoot != nil {
		cfg.Workspace.DefaultRoot = *override.Workspace.DefaultRoot
		sources["workspace.defaultRoot"] = source
	}
	if override.Prompts != nil && override.Prompts.DefaultLayerSelection != nil {
		cfg.Prompts.DefaultLayerSelection = append([]string{}, (*override.Prompts.DefaultLayerSelection)...)
		sources["prompts.defaultLayerSelection"] = source
	}
}

func sourcesForConfig(cfg Config, source Source) SourceMap {
	sources := SourceMap{
		"workspace.defaultRoot":         source,
		"prompts.defaultLayerSelection": source,
	}
	for key := range cfg.Repos {
		prefix := "repos." + key + "."
		for _, field := range []string{"url", "branch", "protocol", "authPrompt"} {
			sources[prefix+field] = source
		}
	}
	for key := range cfg.Modules {
		prefix := "modules." + key + "."
		sources[prefix+"description"] = source
		sources[prefix+"extends"] = source
	}
	return sources
}

func validateEffective(cfg *Config) error {
	if err := cfg.validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	for _, module := range cfg.Prompts.DefaultLayerSelection {
		if _, ok := cfg.Modules[module]; !ok {
			return fmt.Errorf("%w: default module %q is not configured", ErrInvalidConfig, module)
		}
	}
	for key, repo := range cfg.Repos {
		parsed, err := url.Parse(repo.URL)
		if err != nil {
			return fmt.Errorf("%w: repo %q has invalid url", ErrInvalidConfig, key)
		}
		if parsed.User != nil {
			return fmt.Errorf("%w: repo %q URL must not contain credentials", ErrInvalidConfig, key)
		}
	}
	return nil
}

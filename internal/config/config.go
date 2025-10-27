package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// Config captures CLI configuration sourced from YAML or JSON files.
type Config struct {
	Repos     map[string]RepoConfig   `yaml:"repos" json:"repos"`
	Modules   map[string]ModuleConfig `yaml:"modules" json:"modules"`
	Workspace WorkspaceConfig         `yaml:"workspace" json:"workspace"`
	Prompts   PromptConfig            `yaml:"prompts" json:"prompts"`
}

// RepoConfig describes starter repository inputs.
type RepoConfig struct {
	URL        string `yaml:"url" json:"url"`
	Branch     string `yaml:"branch" json:"branch"`
	Protocol   string `yaml:"protocol" json:"protocol"`
	AuthPrompt bool   `yaml:"authPrompt" json:"authPrompt"`
}

// ModuleConfig provides documentation scaffolding per module.
type ModuleConfig struct {
	Description string `yaml:"description" json:"description"`
	Extends     string `yaml:"extends" json:"extends"`
}

// WorkspaceConfig holds directories and defaults.
type WorkspaceConfig struct {
	DefaultRoot string `yaml:"defaultRoot" json:"defaultRoot"`
}

// PromptConfig defines interactive defaults.
type PromptConfig struct {
	DefaultLayerSelection []string `yaml:"defaultLayerSelection" json:"defaultLayerSelection"`
}

// Load attempts to read config files from disk, defaulting to ~/.cli-init/config.yaml.
func Load(path string) (*Config, error) {
	resolved, err := resolvePath(path)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("config file not found at %s", resolved)
		}
		return nil, err
	}

	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		// Attempt JSON as fallback
		if jsonErr := json.Unmarshal(data, cfg); jsonErr != nil {
			return nil, fmt.Errorf("failed to parse config as YAML (%v) or JSON (%v)", err, jsonErr)
		}
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func resolvePath(input string) (string, error) {
	if strings.TrimSpace(input) != "" {
		return expandHome(input)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("unable to determine home directory: %w", err)
	}
	return filepath.Join(home, ".cli-init", "config.yaml"), nil
}

func expandHome(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
	}
	return path, nil
}

func (c *Config) validate() error {
	if len(c.Repos) == 0 {
		return errors.New("config missing repos definitions")
	}

	required := []string{"init", "create_app", "create_layer"}
	for _, key := range required {
		repo, ok := c.Repos[key]
		if !ok {
			return fmt.Errorf("config missing repo configuration for '%s'", key)
		}
		if strings.TrimSpace(repo.URL) == "" {
			return fmt.Errorf("repo '%s' missing url", key)
		}
		if strings.TrimSpace(repo.Branch) == "" {
			return fmt.Errorf("repo '%s' missing branch", key)
		}
		if repo.Protocol != "" && repo.Protocol != "ssh" && repo.Protocol != "https" {
			return fmt.Errorf("repo '%s' protocol must be ssh or https", key)
		}
	}

	return nil
}

// ResolveExtends returns the extends string for a module, defaulting to @layers/<name>.
func (c *Config) ResolveExtends(module string) string {
	if mod, ok := c.Modules[module]; ok {
		if strings.TrimSpace(mod.Extends) != "" {
			return mod.Extends
		}
	}
	return fmt.Sprintf("@layers/%s", module)
}

// DefaultModuleSelection returns the default modules from config.
func (c *Config) DefaultModuleSelection() []string {
	return append([]string{}, c.Prompts.DefaultLayerSelection...)
}

// WorkspaceRoot returns configured workspace root or current directory fallback.
func (c *Config) WorkspaceRoot() string {
	if strings.TrimSpace(c.Workspace.DefaultRoot) == "" {
		return "."
	}
	return c.Workspace.DefaultRoot
}

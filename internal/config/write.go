package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

func WriteGlobal(path string, cfg Config) error {
	if err := validateEffective(&cfg); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode global config: %w", err)
	}
	return writeAtomic(path, data)
}

func WriteWorkspace(workspaceRoot, globalPath string, override Override) error {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return fmt.Errorf("%w: workspace root is required", ErrInvalidConfig)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%w: workspace root is unavailable", ErrInvalidConfig)
	}

	base, sources, err := loadEmbeddedConfig()
	if err != nil {
		return err
	}
	if strings.TrimSpace(globalPath) == "" {
		globalPath, err = resolvePath("")
		if err != nil {
			return err
		}
	}
	global, exists, err := loadOverride(globalPath)
	if err != nil {
		return fmt.Errorf("load global config: %w", err)
	}
	if exists {
		applyOverride(&base, sources, global, SourceGlobal)
	}
	applyOverride(&base, sources, override, SourceWorkspace)
	if err := validateEffective(&base); err != nil {
		return err
	}
	data, err := yaml.Marshal(override)
	if err != nil {
		return fmt.Errorf("encode workspace config: %w", err)
	}
	return writeAtomic(filepath.Join(root, WorkspaceConfigName), data)
}

func writeAtomic(path string, data []byte) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config path is empty")
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	temporary, err := os.CreateTemp(parent, ".couchfusion-config-*.yaml")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure temporary config: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return os.Chmod(path, 0o600)
}

package workspace

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/gitutil"
)

// RunInit performs workspace initialization.
func RunInit(ctx context.Context, cfg *config.Config, targetPath, overrideLayerBranch string, force bool) error {
	root, err := resolvePath(targetPath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("failed to create target path: %w", err)
	}

	appsDir := filepath.Join(root, "apps")
	layersDir := filepath.Join(root, "layers")

	if err := ensureDir(appsDir); err != nil {
		return err
	}

	if err := prepareForClone(layersDir, force); err != nil {
		return err
	}

	repo := cfg.Repos["init"]
	branch := repo.Branch
	if strings.TrimSpace(overrideLayerBranch) != "" {
		branch = overrideLayerBranch
	}

	if err := gitutil.Clone(ctx, repo.URL, branch, layersDir, repo.Protocol, repo.AuthPrompt); err != nil {
		return err
	}

	return nil
}

// ResolveAppCreationInputs handles name/module selection logic.
func ResolveAppCreationInputs(cfg *config.Config, providedName, providedModules string) (string, []string, error) {
	name := strings.TrimSpace(providedName)
	if name == "" {
		var err error
		name, err = prompt("Enter app name: ")
		if err != nil {
			return "", nil, err
		}
	}

	name = sanitizeName(name)
	if name == "" {
		return "", nil, errors.New("app name cannot be empty")
	}

	modules := parseModules(providedModules)
	if len(modules) == 0 {
		list := availableModules(cfg)
		fmt.Printf("Available modules: %s\n", strings.Join(list, ", "))
		selected, err := prompt("Select modules (comma separated, leave empty for defaults): ")
		if err != nil {
			return "", nil, err
		}
		modules = parseModules(selected)
		if len(modules) == 0 {
			modules = cfg.DefaultModuleSelection()
		}
	}

	validModules := map[string]struct{}{}
	for key := range cfg.Modules {
		validModules[key] = struct{}{}
	}

	for _, m := range modules {
		if _, ok := validModules[m]; !ok {
			return "", nil, fmt.Errorf("module '%s' not found in config", m)
		}
	}

	return name, modules, nil
}

// RunCreateApp scaffolds a new application directory and clones starter repo.
func RunCreateApp(ctx context.Context, cfg *config.Config, appName string, modules []string, overrideBranch string, force bool) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("unable to determine current working directory: %w", err)
	}
	appsDir := filepath.Join(root, "apps")

	if err := checkInitialized(root); err != nil {
		return err
	}

	targetDir := filepath.Join(appsDir, appName)
	if err := prepareForClone(targetDir, force); err != nil {
		return err
	}

	repo := cfg.Repos["create_app"]
	branch := repo.Branch
	if strings.TrimSpace(overrideBranch) != "" {
		branch = overrideBranch
	}

	if err := gitutil.Clone(ctx, repo.URL, branch, targetDir, repo.Protocol, repo.AuthPrompt); err != nil {
		return err
	}

	if err := applyLayerParameters(ctx, targetDir, modules); err != nil {
		return err
	}

	if err := updateNuxtExtends(targetDir, modules); err != nil {
		return err
	}

	if err := writeAppMetadata(targetDir, appName, modules); err != nil {
		return err
	}

	if err := writeModuleSetup(targetDir, cfg, modules); err != nil {
		return err
	}

	return nil
}

// RunCreateLayer clones a new layer repository under /layers.
func RunCreateLayer(ctx context.Context, cfg *config.Config, layerName string, overrideBranch string, force bool) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("unable to determine current working directory: %w", err)
	}

	if err := checkInitialized(root); err != nil {
		return err
	}

	layersDir := filepath.Join(root, "layers")
	targetDir := filepath.Join(layersDir, layerName)
	if err := prepareForClone(targetDir, force); err != nil {
		return err
	}

	repo := cfg.Repos["create_layer"]
	branch := repo.Branch
	if strings.TrimSpace(overrideBranch) != "" {
		branch = overrideBranch
	}

	if err := gitutil.Clone(ctx, repo.URL, branch, targetDir, repo.Protocol, repo.AuthPrompt); err != nil {
		return err
	}

	return nil
}

// ResolveLayerName ensures layer name is collected when missing.
func ResolveLayerName(provided string) (string, error) {
	name := strings.TrimSpace(provided)
	if name == "" {
		var err error
		name, err = prompt("Enter layer name: ")
		if err != nil {
			return "", err
		}
	}
	name = sanitizeName(name)
	if name == "" {
		return "", errors.New("layer name cannot be empty")
	}
	return name, nil
}

func ensureDir(path string) error {
	if _, err := os.Stat(path); err == nil {
		// already exists
		return nil
	}
	return os.MkdirAll(path, 0o755)
}

func prepareForClone(target string, force bool) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("failed to ensure parent directory for %s: %w", target, err)
	}

	if info, err := os.Stat(target); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s exists but is not a directory", target)
		}

		empty, err := isDirEmpty(target)
		if err != nil {
			return err
		}
		if !empty {
			if !force {
				return fmt.Errorf("directory %s already exists and is not empty (use --force to override)", target)
			}
			if err := os.RemoveAll(target); err != nil {
				return fmt.Errorf("failed to clear directory %s: %w", target, err)
			}
		}
	}

	return nil
}

func isDirEmpty(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	_, err = f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	return false, err
}

func checkInitialized(root string) error {
	fmt.Printf("Root path: %s\n", root)
	apps := filepath.Join(root, "apps")
	layers := filepath.Join(root, "layers")

	if _, err := os.Stat(apps); err != nil {
		return errors.New("workspace not initialized: missing apps directory")
	}
	if _, err := os.Stat(layers); err != nil {
		return errors.New("workspace not initialized: missing layers directory")
	}

	return nil
}

func writeAppMetadata(targetDir, appName string, modules []string) error {
	meta := map[string]any{
		"appName":     appName,
		"modules":     modules,
		"generatedAt": time.Now().UTC().Format(time.RFC3339),
		"cliVersion":  version(),
	}

	metaPath := filepath.Join(targetDir, "couchfusion.json")
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0o644)
}

func writeModuleSetup(targetDir string, cfg *config.Config, modules []string) error {
	type extendsEntry struct {
		Module string `json:"module"`
		Notes  string `json:"notes,omitempty"`
	}

	extends := []extendsEntry{}
	for _, m := range modules {
		modCfg := cfg.Modules[m]
		extends = append(extends, extendsEntry{
			Module: cfg.ResolveExtends(m),
			Notes:  strings.TrimSpace(modCfg.Description),
		})
	}

	payload := map[string]any{
		"extends":         extends,
		"selectedModules": modules,
		"generatedAt":     time.Now().UTC().Format(time.RFC3339),
		"cliVersion":      version(),
		"nextSteps": []string{
			"Update nuxt.config.ts to include the listed extends entries.",
			"Review layer-specific documentation under /layers/<module>/docs for additional setup.",
		},
	}

	docsDir := filepath.Join(targetDir, "docs")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		return err
	}

	path := filepath.Join(docsDir, "module_setup.json")
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

func prompt(message string) (string, error) {
	fmt.Print(message)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(input), nil
}

func sanitizeName(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	replacer := strings.NewReplacer(" ", "-", "_", "-", "..", ".")
	transformed := replacer.Replace(input)
	b := strings.Builder{}
	for _, r := range transformed {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), "-")
}

func parseModules(input string) []string {
	if strings.TrimSpace(input) == "" {
		return nil
	}
	parts := strings.Split(input, ",")
	out := []string{}
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func availableModules(cfg *config.Config) []string {
	keys := make([]string, 0, len(cfg.Modules))
	for key := range cfg.Modules {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func updateNuxtExtends(targetDir string, modules []string) error {
	path := filepath.Join(targetDir, "nuxt.config.ts")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to read nuxt.config.ts: %w", err)
	}

	extendsLines := []string{}
	for _, module := range modules {
		extendsLines = append(extendsLines, fmt.Sprintf("    '../../layers/%s'", module))
	}

	var replacement string
	if len(extendsLines) == 0 {
		replacement = "  extends: [],"
	} else {
		replacement = fmt.Sprintf("  extends: [\n%s\n  ],", strings.Join(extendsLines, ",\n"))
	}

	pattern := regexp.MustCompile(`(?s)extends\s*:\s*\[.*?\]`)
	if pattern.Match(data) {
		data = pattern.ReplaceAll(data, []byte(replacement))
	} else {
		// attempt to insert after defineNuxtConfig opening brace
		insertPattern := regexp.MustCompile(`defineNuxtConfig\(\{\s*`)
		loc := insertPattern.FindIndex(data)
		if loc == nil {
			return fmt.Errorf("unable to locate extends block or insertion point in nuxt.config.ts")
		}
		insertPos := loc[1]
		newContent := append([]byte{}, data[:insertPos]...)
		newContent = append(newContent, []byte("\n"+replacement+"\n")...)
		newContent = append(newContent, data[insertPos:]...)
		data = newContent
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("failed to update nuxt.config.ts: %w", err)
	}

	return nil
}

var version = func() func() string {
	var v string
	return func() string {
		if v == "" {
			v = os.Getenv("COUCHFUSION_VERSION")
			if v == "" {
				v = "unknown"
			}
		}
		return v
	}
}()

func resolvePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "."
	}
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("unable to resolve home directory: %w", err)
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("unable to determine working directory: %w", err)
	}
	return filepath.Clean(filepath.Join(cwd, path)), nil
}

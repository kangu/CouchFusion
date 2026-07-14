package workspace

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/nuxt-apps/couchfusion/internal/config"
	"github.com/nuxt-apps/couchfusion/internal/gitutil"
)

type InitRequest struct {
	Root        string
	LayerBranch string
	Force       bool
}

type CreateSiteRequest struct {
	Root          string
	Name          string
	Modules       []string
	DBLoginPrefix string
	Branch        string
	Force         bool
	Auth          *AuthCredentials
}

type CreateLayerRequest struct {
	Root   string
	Name   string
	Branch string
	Force  bool
}

type EventLevel string

const (
	EventInfo    EventLevel = "info"
	EventSuccess EventLevel = "success"
)

type Event struct {
	Code    string
	Message string
	Level   EventLevel
}

type EventSink interface {
	Emit(Event)
}

type CloneFunc func(context.Context, string, string, string, string, bool, ...gitutil.CloneOption) error
type ApplyLayerParametersFunc func(context.Context, string, []string) error

type Dependencies struct {
	Clone                CloneFunc
	ApplyLayerParameters ApplyLayerParametersFunc
	Events               EventSink
}

func (d Dependencies) withDefaults() Dependencies {
	if d.Clone == nil {
		d.Clone = gitutil.Clone
	}
	if d.ApplyLayerParameters == nil {
		d.ApplyLayerParameters = func(ctx context.Context, targetDir string, modules []string) error {
			return applyLayerParameters(ctx, targetDir, modules, false)
		}
	}
	return d
}

func (d Dependencies) emit(level EventLevel, code, message string) {
	if d.Events != nil {
		d.Events.Emit(Event{Level: level, Code: code, Message: message})
	}
}

func Init(ctx context.Context, cfg *config.Config, request InitRequest, dependencies Dependencies) error {
	dependencies = dependencies.withDefaults()
	root, err := resolveExplicitRoot(request.Root)
	if err != nil {
		return err
	}
	if cfg == nil {
		return errors.New("config is required")
	}

	dependencies.emit(EventInfo, "workspace.init.started", "Initializing workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("failed to create target path: %w", err)
	}

	if err := ensureDir(filepath.Join(root, "apps")); err != nil {
		return err
	}
	layersDir := filepath.Join(root, "layers")
	if err := prepareForClone(layersDir, request.Force); err != nil {
		return err
	}

	repo := cfg.Repos["init"]
	branch := repo.Branch
	if strings.TrimSpace(request.LayerBranch) != "" {
		branch = request.LayerBranch
	}
	if err := dependencies.Clone(ctx, repo.URL, branch, layersDir, repo.Protocol, repo.AuthPrompt); err != nil {
		return err
	}
	dependencies.emit(EventSuccess, "workspace.init.completed", "Workspace initialized")
	return nil
}

func CreateSite(ctx context.Context, cfg *config.Config, request CreateSiteRequest, dependencies Dependencies) error {
	dependencies = dependencies.withDefaults()
	root, err := resolveExplicitRoot(request.Root)
	if err != nil {
		return err
	}
	if cfg == nil {
		return errors.New("config is required")
	}
	if err := checkInitialized(root); err != nil {
		return err
	}

	name := sanitizeName(request.Name)
	if name == "" {
		return errors.New("app name cannot be empty")
	}
	if err := ValidateDBLoginPrefix(request.DBLoginPrefix); err != nil {
		return err
	}
	if err := validateModules(cfg, request.Modules); err != nil {
		return err
	}
	targetDir := filepath.Join(root, "apps", name)
	if err := prepareForClone(targetDir, request.Force); err != nil {
		return err
	}

	repo := cfg.Repos["new"]
	branch := repo.Branch
	if strings.TrimSpace(request.Branch) != "" {
		branch = request.Branch
	}
	dependencies.emit(EventInfo, "site.clone.started", "Cloning site starter")
	if err := dependencies.Clone(ctx, repo.URL, branch, targetDir, repo.Protocol, repo.AuthPrompt); err != nil {
		return err
	}
	if err := reinitializeGitRepo(ctx, targetDir); err != nil {
		return err
	}
	if err := updateLayerDependencies(targetDir, request.Modules); err != nil {
		return err
	}
	if request.Auth != nil {
		ctx = WithAuthCredentials(ctx, request.Auth.Username, request.Auth.Password)
	}
	if err := dependencies.ApplyLayerParameters(ctx, targetDir, request.Modules); err != nil {
		return err
	}
	if err := updateNuxtExtends(targetDir, request.Modules); err != nil {
		return err
	}
	if err := updateDBLoginPrefix(targetDir, request.DBLoginPrefix); err != nil {
		return err
	}
	dependencies.emit(EventSuccess, "site.config.database_prefix", "Database login prefix configured")
	if err := writeAppMetadata(targetDir, name, request.Modules); err != nil {
		return err
	}
	if err := writeModuleSetup(targetDir, cfg, request.Modules); err != nil {
		return err
	}
	dependencies.emit(EventSuccess, "site.create.completed", "Site created")
	return nil
}

func CreateLayer(ctx context.Context, cfg *config.Config, request CreateLayerRequest, dependencies Dependencies) error {
	dependencies = dependencies.withDefaults()
	root, err := resolveExplicitRoot(request.Root)
	if err != nil {
		return err
	}
	if cfg == nil {
		return errors.New("config is required")
	}
	if err := checkInitialized(root); err != nil {
		return err
	}

	name := sanitizeName(request.Name)
	if name == "" {
		return errors.New("layer name cannot be empty")
	}
	targetDir := filepath.Join(root, "layers", name)
	if err := prepareForClone(targetDir, request.Force); err != nil {
		return err
	}

	repo := cfg.Repos["create_layer"]
	branch := repo.Branch
	if strings.TrimSpace(request.Branch) != "" {
		branch = request.Branch
	}
	dependencies.emit(EventInfo, "layer.clone.started", "Cloning layer starter")
	if err := dependencies.Clone(ctx, repo.URL, branch, targetDir, repo.Protocol, repo.AuthPrompt); err != nil {
		return err
	}
	if err := reinitializeGitRepo(ctx, targetDir); err != nil {
		return err
	}
	dependencies.emit(EventSuccess, "layer.create.completed", "Layer created")
	return nil
}

// RunInit performs workspace initialization.
func RunInit(ctx context.Context, cfg *config.Config, targetPath, overrideLayerBranch string, force bool, cloneOpts ...gitutil.CloneOption) error {
	clone := cloneWithOptions(cloneOpts)
	return Init(ctx, cfg, InitRequest{Root: targetPath, LayerBranch: overrideLayerBranch, Force: force}, Dependencies{Clone: clone})
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
		if len(list) > 0 {
			fmt.Printf("Available modules: %s\n", strings.Join(list, ", "))
			selected, err := prompt("Select modules (comma separated, leave empty for defaults): ")
			if err != nil {
				return "", nil, err
			}
			modules = parseModules(selected)
		}
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

// RunNew scaffolds a new application directory and clones starter repo.
func RunNew(ctx context.Context, cfg *config.Config, appName string, modules []string, overrideBranch string, force bool, cloneOpts ...gitutil.CloneOption) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("unable to determine current working directory: %w", err)
	}
	clone := cloneWithOptions(cloneOpts)
	return CreateSite(ctx, cfg, CreateSiteRequest{Root: root, Name: appName, Modules: modules, Branch: overrideBranch, Force: force}, Dependencies{
		Clone: clone,
		ApplyLayerParameters: func(ctx context.Context, targetDir string, modules []string) error {
			return applyLayerParameters(ctx, targetDir, modules, true)
		},
	})
}

// RunCreateLayer clones a new layer repository under /layers.
func RunCreateLayer(ctx context.Context, cfg *config.Config, layerName string, overrideBranch string, force bool, cloneOpts ...gitutil.CloneOption) error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("unable to determine current working directory: %w", err)
	}

	return CreateLayer(ctx, cfg, CreateLayerRequest{Root: root, Name: layerName, Branch: overrideBranch, Force: force}, Dependencies{Clone: cloneWithOptions(cloneOpts)})
}

func cloneWithOptions(cloneOpts []gitutil.CloneOption) CloneFunc {
	return func(ctx context.Context, repoURL, branch, targetDir, protocol string, authPrompt bool, opts ...gitutil.CloneOption) error {
		combined := append(append([]gitutil.CloneOption{}, cloneOpts...), opts...)
		return gitutil.Clone(ctx, repoURL, branch, targetDir, protocol, authPrompt, combined...)
	}
}

func resolveExplicitRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("workspace root is required")
	}
	return resolvePath(root)
}

func validateModules(cfg *config.Config, modules []string) error {
	for _, module := range modules {
		if _, ok := cfg.Modules[module]; !ok {
			return fmt.Errorf("module '%s' not found in config", module)
		}
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
			"Review nuxt.config.ts and package.json to confirm selected layer wiring.",
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

func isInteractiveTerminal() bool {
	return isatty.IsTerminal(os.Stdout.Fd()) && isatty.IsTerminal(os.Stdin.Fd())
}

// ShouldUseTUI reports whether the CLI should run the Bubble Tea interface.
func ShouldUseTUI() bool {
	if os.Getenv("COUCHFUSION_NO_TUI") != "" {
		return false
	}
	return isInteractiveTerminal()
}

func availableModules(cfg *config.Config) []string {
	keys := make([]string, 0, len(cfg.Modules))
	for key := range cfg.Modules {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// EnsureCurrentWorkspace validates that the current directory contains the expected couchfusion structure.
func EnsureCurrentWorkspace() error {
	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("unable to determine current working directory: %w", err)
	}
	return checkInitialized(root)
}

func updateLayerDependencies(targetDir string, modules []string) error {
	if len(modules) == 0 {
		return nil
	}

	pkgPath := filepath.Join(targetDir, "package.json")

	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return fmt.Errorf("failed to read package.json: %w", err)
	}

	pkg := map[string]any{}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return fmt.Errorf("failed to parse package.json: %w", err)
	}

	deps := map[string]any{}
	if existing, ok := pkg["dependencies"]; ok {
		if existingMap, ok := existing.(map[string]any); ok {
			deps = existingMap
		} else {
			return fmt.Errorf("package.json dependencies must be an object")
		}
	}

	seen := map[string]struct{}{}
	for _, module := range modules {
		if _, ok := seen[module]; ok {
			continue
		}
		seen[module] = struct{}{}

		depName := fmt.Sprintf("@my/%s", module)
		deps[depName] = fmt.Sprintf("file:../../layers/%s", module)
	}

	pkg["dependencies"] = deps

	updated, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal package.json: %w", err)
	}
	if !bytes.HasSuffix(updated, []byte("\n")) {
		updated = append(updated, '\n')
	}

	if err := os.WriteFile(pkgPath, updated, 0o644); err != nil {
		return fmt.Errorf("failed to write package.json: %w", err)
	}

	return nil
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

	replacement := "  extends: []"
	if len(extendsLines) > 0 {
		replacement = fmt.Sprintf("  extends: [\n%s\n  ]", strings.Join(extendsLines, ",\n"))
	}

	pattern := regexp.MustCompile(`(?s)extends\s*:\s*\[.*?\]\s*,?`)
	if pattern.Match(data) {
		data = pattern.ReplaceAllFunc(data, func(match []byte) []byte {
			hasComma := strings.HasSuffix(strings.TrimSpace(string(match)), ",")
			if hasComma {
				return []byte(replacement + ",")
			}
			return []byte(replacement)
		})
	} else {
		insertPattern := regexp.MustCompile(`defineNuxtConfig\(\{\s*`)
		loc := insertPattern.FindIndex(data)
		if loc == nil {
			return fmt.Errorf("unable to locate extends block or insertion point in nuxt.config.ts")
		}
		insertPos := loc[1]
		rest := strings.TrimSpace(string(data[insertPos:]))
		needsComma := rest != "" && !strings.HasPrefix(rest, "}")

		line := "\n" + replacement
		if needsComma {
			line += ","
		}
		line += "\n"

		newContent := append([]byte{}, data[:insertPos]...)
		newContent = append(newContent, []byte(line)...)
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

func reinitializeGitRepo(ctx context.Context, targetDir string) error {
	gitDir := filepath.Join(targetDir, ".git")
	if err := os.RemoveAll(gitDir); err != nil {
		return fmt.Errorf("failed to remove git history in %s: %w", targetDir, err)
	}

	cmd := exec.CommandContext(ctx, "git", "init")
	cmd.Dir = targetDir
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to reinitialize git repository in %s: %w", targetDir, err)
	}

	return nil
}

package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nuxt-apps/couchfusion/internal/registry"
)

var (
	ErrPathEscape = errors.New("project path escapes workspace")
	ErrNotFound   = errors.New("project was not found")
)

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Workspace struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Root      string    `json:"root"`
	Available bool      `json:"available"`
	Problem   string    `json:"problem,omitempty"`
	Sites     []Site    `json:"sites"`
	Layers    []Layer   `json:"layers"`
	Warnings  []Warning `json:"warnings"`
}

type Site struct {
	ID                    string            `json:"id"`
	Name                  string            `json:"name"`
	Root                  string            `json:"root"`
	PackageName           string            `json:"packageName,omitempty"`
	PackageManager        string            `json:"packageManager,omitempty"`
	Modules               []string          `json:"modules"`
	Scripts               map[string]string `json:"scripts"`
	Dependencies          map[string]string `json:"dependencies"`
	DevDependencies       map[string]string `json:"devDependencies"`
	DependenciesInstalled bool              `json:"dependenciesInstalled"`
	Lockfiles             []string          `json:"lockfiles"`
	Warnings              []Warning         `json:"warnings"`
}

type Layer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Root string `json:"root"`
}

type Inspector struct{}

func (Inspector) Workspace(ctx context.Context, entry registry.Entry) (Workspace, error) {
	workspace := Workspace{
		ID:       entry.ID,
		Name:     entry.Name,
		Root:     entry.Root,
		Sites:    []Site{},
		Layers:   []Layer{},
		Warnings: []Warning{},
	}
	if err := ctx.Err(); err != nil {
		return Workspace{}, err
	}
	root, err := existingCanonicalDir(entry.Root)
	if err != nil {
		workspace.Problem = "workspace root is unavailable"
		return workspace, nil
	}
	workspace.Root = root
	appsRoot := filepath.Join(root, "apps")
	layersRoot := filepath.Join(root, "layers")
	if !isDirectory(appsRoot) {
		workspace.Problem = "workspace is missing apps directory"
		return workspace, nil
	}
	if !isDirectory(layersRoot) {
		workspace.Problem = "workspace is missing layers directory"
		return workspace, nil
	}
	workspace.Available = true

	appEntries, err := os.ReadDir(appsRoot)
	if err != nil {
		return Workspace{}, fmt.Errorf("read apps directory: %w", err)
	}
	for _, child := range appEntries {
		if err := ctx.Err(); err != nil {
			return Workspace{}, err
		}
		if !child.IsDir() && child.Type()&os.ModeSymlink == 0 {
			continue
		}
		site, err := (Inspector{}).Site(ctx, root, child.Name())
		if errors.Is(err, ErrPathEscape) {
			workspace.Warnings = append(workspace.Warnings, Warning{Code: "site.path_escape", Message: "Skipped site outside the apps directory"})
			continue
		}
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return Workspace{}, err
		}
		workspace.Sites = append(workspace.Sites, site)
	}
	sort.Slice(workspace.Sites, func(i, j int) bool { return workspace.Sites[i].ID < workspace.Sites[j].ID })

	layerEntries, err := os.ReadDir(layersRoot)
	if err != nil {
		return Workspace{}, fmt.Errorf("read layers directory: %w", err)
	}
	for _, child := range layerEntries {
		if !child.IsDir() && child.Type()&os.ModeSymlink == 0 {
			continue
		}
		childRoot, err := containedChild(layersRoot, child.Name())
		if errors.Is(err, ErrPathEscape) {
			workspace.Warnings = append(workspace.Warnings, Warning{Code: "layer.path_escape", Message: "Skipped layer outside the layers directory"})
			continue
		}
		if err != nil {
			continue
		}
		workspace.Layers = append(workspace.Layers, Layer{ID: child.Name(), Name: child.Name(), Root: childRoot})
	}
	sort.Slice(workspace.Layers, func(i, j int) bool { return workspace.Layers[i].ID < workspace.Layers[j].ID })
	return workspace, nil
}

func (Inspector) Site(ctx context.Context, workspaceRoot, siteID string) (Site, error) {
	if err := ctx.Err(); err != nil {
		return Site{}, err
	}
	root, err := existingCanonicalDir(workspaceRoot)
	if err != nil {
		return Site{}, fmt.Errorf("workspace root: %w", err)
	}
	appsRoot := filepath.Join(root, "apps")
	if !isDirectory(appsRoot) {
		return Site{}, fmt.Errorf("%w: apps directory", ErrNotFound)
	}
	siteRoot, err := containedChild(appsRoot, siteID)
	if err != nil {
		return Site{}, err
	}

	site := Site{
		ID:              siteID,
		Name:            siteID,
		Root:            siteRoot,
		Modules:         []string{},
		Scripts:         map[string]string{},
		Dependencies:    map[string]string{},
		DevDependencies: map[string]string{},
		Lockfiles:       []string{},
		Warnings:        []Warning{},
	}
	metadata := struct {
		AppName string   `json:"appName"`
		Modules []string `json:"modules"`
	}{}
	if err := decodeOptionalJSON(filepath.Join(siteRoot, "couchfusion.json"), &metadata); err != nil {
		site.Warnings = append(site.Warnings, Warning{Code: "site.metadata_invalid", Message: "Could not read couchfusion.json"})
	} else {
		if strings.TrimSpace(metadata.AppName) != "" {
			site.Name = metadata.AppName
		}
		site.Modules = append(site.Modules, metadata.Modules...)
	}

	packageData := struct {
		Name            string            `json:"name"`
		PackageManager  string            `json:"packageManager"`
		Scripts         map[string]string `json:"scripts"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}{}
	if err := decodeOptionalJSON(filepath.Join(siteRoot, "package.json"), &packageData); err != nil {
		site.Warnings = append(site.Warnings, Warning{Code: "site.package_invalid", Message: "Could not read package.json"})
	} else {
		site.PackageName = packageData.Name
		site.PackageManager = packageData.PackageManager
		site.Scripts = cloneMap(packageData.Scripts)
		site.Dependencies = cloneMap(packageData.Dependencies)
		site.DevDependencies = cloneMap(packageData.DevDependencies)
		for _, value := range site.Dependencies {
			if module, ok := strings.CutPrefix(value, "file:../../layers/"); ok && validChildID(module) {
				site.Modules = append(site.Modules, module)
			}
		}
	}
	site.Modules = uniqueSorted(site.Modules)
	site.DependenciesInstalled = isDirectory(filepath.Join(siteRoot, "node_modules"))
	for _, name := range []string{"bun.lock", "bun.lockb", "package-lock.json", "pnpm-lock.yaml", "yarn.lock"} {
		if info, err := os.Stat(filepath.Join(siteRoot, name)); err == nil && !info.IsDir() {
			site.Lockfiles = append(site.Lockfiles, name)
		}
	}
	return site, nil
}

func containedChild(parent, childID string) (string, error) {
	if !validChildID(childID) {
		return "", ErrPathEscape
	}
	parentRoot, err := existingCanonicalDir(parent)
	if err != nil {
		return "", fmt.Errorf("%w: parent directory", ErrNotFound)
	}
	candidate := filepath.Join(parentRoot, childID)
	canonical, err := existingCanonicalDir(candidate)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrNotFound, childID)
	}
	relative, err := filepath.Rel(parentRoot, canonical)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", ErrPathEscape
	}
	return canonical, nil
}

func validChildID(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value && !strings.ContainsAny(value, `/\`)
}

func existingCanonicalDir(path string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	if !isDirectory(canonical) {
		return "", errors.New("not a directory")
	}
	return canonical, nil
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func decodeOptionalJSON(path string, destination any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) == nil {
		return errors.New("JSON file contains multiple values")
	}
	return nil
}

func cloneMap(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func uniqueSorted(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

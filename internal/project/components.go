package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

var (
	ErrComponentNotFound        = errors.New("component was not found")
	ErrComponentRegistryInvalid = errors.New("component registry is invalid")
)

type ComponentRegistryStatus string

const (
	ComponentRegistryReady   ComponentRegistryStatus = "ready"
	ComponentRegistryStale   ComponentRegistryStatus = "stale"
	ComponentRegistryMissing ComponentRegistryStatus = "missing"
	ComponentRegistryInvalid ComponentRegistryStatus = "invalid"
)

type ComponentDefinition struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Description   string `json:"description"`
	Source        string `json:"source"`
	PropCount     int    `json:"propCount"`
	AllowChildren bool   `json:"allowChildren"`
}

type ComponentRegistry struct {
	Status     ComponentRegistryStatus `json:"status"`
	Components []ComponentDefinition   `json:"components"`
	Problem    string                  `json:"problem,omitempty"`
}

type componentManifest struct {
	Version          int                   `json:"version"`
	TypeScriptSHA256 string                `json:"typescriptSha256"`
	Components       []ComponentDefinition `json:"components"`
}

type ComponentInspector struct{}

func (ComponentInspector) Inspect(ctx context.Context, siteRoot string) ComponentRegistry {
	if err := ctx.Err(); err != nil {
		return invalidComponentRegistry("Component registry inspection was interrupted.")
	}
	root, err := existingCanonicalDir(siteRoot)
	if err != nil {
		return invalidComponentRegistry("The site root is unavailable.")
	}
	manifestPath, found, err := findComponentManifest(root)
	if err != nil {
		return invalidComponentRegistry("Component registry metadata is unavailable.")
	}
	if !found {
		return ComponentRegistry{Status: ComponentRegistryMissing, Components: []ComponentDefinition{}}
	}

	manifest, err := decodeComponentManifest(manifestPath)
	if err != nil {
		return invalidComponentRegistry("Component registry JSON is invalid.")
	}
	if manifest.Version != 1 || manifest.Components == nil || !validSHA256(manifest.TypeScriptSHA256) {
		return invalidComponentRegistry("Component registry metadata is invalid.")
	}
	typeScriptPath := strings.TrimSuffix(manifestPath, ".json") + ".ts"
	typeScript, err := os.ReadFile(typeScriptPath)
	if err != nil {
		return invalidComponentRegistry("Component registry TypeScript definitions are unavailable.")
	}
	digest := sha256.Sum256(typeScript)
	if hex.EncodeToString(digest[:]) != manifest.TypeScriptSHA256 {
		return invalidComponentRegistry("Component registry does not match its TypeScript definitions.")
	}

	manifestInfo, err := os.Stat(manifestPath)
	if err != nil || !manifestInfo.Mode().IsRegular() {
		return invalidComponentRegistry("Component registry metadata is unavailable.")
	}
	stale := false
	previousID := ""
	for index, component := range manifest.Components {
		if !validComponentDefinition(component) || (index > 0 && component.ID <= previousID) {
			return invalidComponentRegistry("Component registry metadata is invalid.")
		}
		previousID = component.ID
		_, info, err := resolveComponentFile(root, component.Source)
		if err != nil {
			return invalidComponentRegistry("A registered component source is unsafe or unavailable.")
		}
		if info.ModTime().After(manifestInfo.ModTime()) {
			stale = true
		}
	}

	status := ComponentRegistryReady
	if stale {
		status = ComponentRegistryStale
	}
	return ComponentRegistry{Status: status, Components: append([]ComponentDefinition{}, manifest.Components...)}
}

func (inspector ComponentInspector) ResolveSource(ctx context.Context, siteRoot, componentID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	registry := inspector.Inspect(ctx, siteRoot)
	if registry.Status == ComponentRegistryMissing || registry.Status == ComponentRegistryInvalid {
		return "", ErrComponentRegistryInvalid
	}
	for _, component := range registry.Components {
		if component.ID != componentID {
			continue
		}
		root, err := existingCanonicalDir(siteRoot)
		if err != nil {
			return "", fmt.Errorf("%w: site root", ErrComponentRegistryInvalid)
		}
		filename, _, err := resolveComponentFile(root, component.Source)
		if err != nil {
			return "", fmt.Errorf("%w: component source", ErrComponentRegistryInvalid)
		}
		return filename, nil
	}
	return "", ErrComponentNotFound
}

func findComponentManifest(siteRoot string) (string, bool, error) {
	for _, relative := range []string{
		filepath.Join("app", "content-builder", "component-definitions.json"),
		filepath.Join("content-builder", "component-definitions.json"),
	} {
		candidate := filepath.Join(siteRoot, relative)
		info, err := os.Stat(candidate)
		if err == nil {
			if !info.Mode().IsRegular() {
				return "", false, errors.New("manifest is not a regular file")
			}
			return candidate, true, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", false, err
		}
	}
	return "", false, nil
}

func decodeComponentManifest(filename string) (componentManifest, error) {
	file, err := os.Open(filename)
	if err != nil {
		return componentManifest{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest componentManifest
	if err := decoder.Decode(&manifest); err != nil {
		return componentManifest{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return componentManifest{}, errors.New("manifest contains multiple JSON values")
	}
	return manifest, nil
}

func validComponentDefinition(component ComponentDefinition) bool {
	return validComponentID(component.ID) &&
		strings.TrimSpace(component.Label) != "" &&
		component.Label == strings.TrimSpace(component.Label) &&
		component.PropCount >= 0 &&
		validComponentSource(component.Source)
}

func validComponentID(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || strings.ContainsAny(value, `/\`) {
		return false
	}
	for _, item := range value {
		if unicode.IsControl(item) || unicode.IsSpace(item) {
			return false
		}
	}
	return true
}

func validComponentSource(source string) bool {
	return source != "" &&
		!strings.Contains(source, `\`) &&
		!path.IsAbs(source) &&
		path.Clean(source) == source &&
		!strings.HasPrefix(source, "../") &&
		path.Ext(source) == ".vue"
}

func resolveComponentFile(siteRoot, source string) (string, os.FileInfo, error) {
	if !validComponentSource(source) {
		return "", nil, errors.New("invalid component source")
	}
	candidate := filepath.Join(siteRoot, filepath.FromSlash(source))
	if !pathContained(siteRoot, candidate) {
		return "", nil, errors.New("component source escapes site root")
	}
	canonical, err := filepath.EvalSymlinks(candidate)
	if err != nil || !pathContained(siteRoot, canonical) {
		return "", nil, errors.New("component source escapes site root")
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil, errors.New("component source is not a regular file")
	}
	return canonical, info, nil
}

func pathContained(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, item := range value {
		if (item < '0' || item > '9') && (item < 'a' || item > 'f') {
			return false
		}
	}
	return true
}

func invalidComponentRegistry(problem string) ComponentRegistry {
	return ComponentRegistry{Status: ComponentRegistryInvalid, Components: []ComponentDefinition{}, Problem: problem}
}

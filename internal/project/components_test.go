package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestComponentInspectorReportsSupportedStates(t *testing.T) {
	tests := []struct {
		name    string
		arrange func(*testing.T, string)
		want    ComponentRegistryStatus
		count   int
	}{
		{name: "missing", want: ComponentRegistryMissing},
		{name: "nuxt4 ready", arrange: func(t *testing.T, root string) { writeComponentRegistry(t, root, true, defaultComponents(), nil) }, want: ComponentRegistryReady, count: 1},
		{name: "nuxt3 ready", arrange: func(t *testing.T, root string) { writeComponentRegistry(t, root, false, defaultComponents(), nil) }, want: ComponentRegistryReady, count: 1},
		{name: "valid empty", arrange: func(t *testing.T, root string) { writeComponentRegistry(t, root, true, []ComponentDefinition{}, nil) }, want: ComponentRegistryReady},
		{name: "stale", arrange: writeStaleComponentRegistry, want: ComponentRegistryStale, count: 1},
		{name: "malformed", arrange: writeMalformedComponentRegistry, want: ComponentRegistryInvalid},
		{name: "unsupported version", arrange: func(t *testing.T, root string) {
			writeComponentRegistry(t, root, true, defaultComponents(), func(document map[string]any) { document["version"] = 2 })
		}, want: ComponentRegistryInvalid},
		{name: "duplicate id", arrange: func(t *testing.T, root string) {
			components := append(defaultComponents(), defaultComponents()[0])
			writeComponentRegistry(t, root, true, components, nil)
		}, want: ComponentRegistryInvalid},
		{name: "hash mismatch", arrange: func(t *testing.T, root string) {
			writeComponentRegistry(t, root, true, defaultComponents(), func(document map[string]any) { document["typescriptSha256"] = strings.Repeat("0", 64) })
		}, want: ComponentRegistryInvalid},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if test.arrange != nil {
				test.arrange(t, root)
			}

			got := (ComponentInspector{}).Inspect(context.Background(), root)

			if got.Status != test.want || len(got.Components) != test.count {
				t.Fatalf("Inspect() = %#v, want status %q and count %d", got, test.want, test.count)
			}
			if got.Components == nil {
				t.Fatal("components must encode as an empty array")
			}
			if test.want == ComponentRegistryInvalid && got.Problem == "" {
				t.Fatal("invalid registry must include a safe problem description")
			}
		})
	}
}

func TestComponentInspectorRejectsMalformedMetadata(t *testing.T) {
	tests := []struct {
		name       string
		components []ComponentDefinition
		mutate     func(map[string]any)
	}{
		{name: "unordered ids", components: []ComponentDefinition{componentDefinition("z-card", "app/components/content/ZCard.vue"), componentDefinition("a-hero", "app/components/content/AHero.vue")}},
		{name: "empty id", components: []ComponentDefinition{componentDefinition("", "app/components/content/Hero.vue")}},
		{name: "empty label", components: []ComponentDefinition{{ID: "hero", Source: "app/components/content/Hero.vue"}}},
		{name: "negative prop count", components: []ComponentDefinition{{ID: "hero", Label: "Hero", Source: "app/components/content/Hero.vue", PropCount: -1}}},
		{name: "uppercase hash", components: defaultComponents(), mutate: func(document map[string]any) {
			document["typescriptSha256"] = strings.ToUpper(document["typescriptSha256"].(string))
		}},
		{name: "unknown field", components: defaultComponents(), mutate: func(document map[string]any) { document["executable"] = "bun" }},
		{name: "null components", mutate: func(document map[string]any) { document["components"] = nil }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeComponentRegistry(t, root, true, test.components, test.mutate)

			got := (ComponentInspector{}).Inspect(context.Background(), root)

			if got.Status != ComponentRegistryInvalid || len(got.Components) != 0 {
				t.Fatalf("Inspect() = %#v", got)
			}
		})
	}
}

func TestComponentInspectorRejectsUnsafeSources(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "empty", source: ""},
		{name: "lexical escape", source: "../Outside.vue"},
		{name: "absolute", source: "/tmp/Outside.vue"},
		{name: "backslash", source: `app\components\content\Hero.vue`},
		{name: "not normalized", source: "app/components/content/../Hero.vue"},
		{name: "wrong extension", source: "app/components/content/Hero.ts"},
		{name: "missing", source: "app/components/content/Missing.vue"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeComponentRegistry(t, root, true, []ComponentDefinition{componentDefinition("hero", test.source)}, nil)

			got := (ComponentInspector{}).Inspect(context.Background(), root)

			if got.Status != ComponentRegistryInvalid || len(got.Components) != 0 {
				t.Fatalf("Inspect() = %#v", got)
			}
		})
	}
}

func TestComponentInspectorRejectsDirectoryAndSymlinkSources(t *testing.T) {
	root := t.TempDir()
	directorySource := "app/components/content/Directory.vue"
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(directorySource)), 0o755); err != nil {
		t.Fatal(err)
	}
	writeComponentRegistry(t, root, true, []ComponentDefinition{componentDefinition("directory", directorySource)}, nil)
	if got := (ComponentInspector{}).Inspect(context.Background(), root); got.Status != ComponentRegistryInvalid {
		t.Fatalf("directory Inspect() = %#v", got)
	}

	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	root = t.TempDir()
	outside := filepath.Join(t.TempDir(), "Outside.vue")
	if err := os.WriteFile(outside, []byte("<template />"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := "app/components/content/Linked.vue"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, filepath.FromSlash(source))), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, filepath.FromSlash(source))); err != nil {
		t.Fatal(err)
	}
	writeComponentRegistry(t, root, true, []ComponentDefinition{componentDefinition("linked", source)}, nil)
	if got := (ComponentInspector{}).Inspect(context.Background(), root); got.Status != ComponentRegistryInvalid {
		t.Fatalf("symlink Inspect() = %#v", got)
	}
}

func TestResolveSourceLooksUpIDAndRevalidatesContainment(t *testing.T) {
	root := t.TempDir()
	writeComponentRegistry(t, root, true, defaultComponents(), nil)

	source, err := (ComponentInspector{}).ResolveSource(context.Background(), root, "hero")
	if err != nil || !strings.HasSuffix(source, filepath.Join("content", "Hero.vue")) {
		t.Fatalf("ResolveSource() source = %q, error = %v", source, err)
	}
	if _, err := (ComponentInspector{}).ResolveSource(context.Background(), root, "missing"); !errors.Is(err, ErrComponentNotFound) {
		t.Fatalf("missing error = %v, want ErrComponentNotFound", err)
	}

	if runtime.GOOS == "windows" {
		return
	}
	registered := filepath.Join(root, filepath.FromSlash(defaultComponents()[0].Source))
	outside := filepath.Join(t.TempDir(), "Outside.vue")
	if err := os.WriteFile(outside, []byte("<template />"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(registered); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, registered); err != nil {
		t.Fatal(err)
	}
	if _, err := (ComponentInspector{}).ResolveSource(context.Background(), root, "hero"); !errors.Is(err, ErrComponentRegistryInvalid) {
		t.Fatalf("changed source error = %v, want ErrComponentRegistryInvalid", err)
	}
}

func defaultComponents() []ComponentDefinition {
	return []ComponentDefinition{componentDefinition("hero", "app/components/content/Hero.vue")}
}

func componentDefinition(id, source string) ComponentDefinition {
	return ComponentDefinition{ID: id, Label: "Hero", Description: "Lead block", Source: source, PropCount: 2, AllowChildren: true}
}

func writeComponentRegistry(t *testing.T, root string, nuxt4 bool, components []ComponentDefinition, mutate func(map[string]any)) {
	t.Helper()
	builder := filepath.Join(root, "content-builder")
	if nuxt4 {
		builder = filepath.Join(root, "app", "content-builder")
	}
	if err := os.MkdirAll(builder, 0o755); err != nil {
		t.Fatal(err)
	}
	typeScript := []byte("export default []\n")
	if err := os.WriteFile(filepath.Join(builder, "component-definitions.ts"), typeScript, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, component := range components {
		if component.Source == "" || strings.Contains(component.Source, "..") || filepath.IsAbs(component.Source) || strings.Contains(component.Source, `\`) || filepath.Ext(component.Source) != ".vue" || strings.Contains(component.Source, "Missing") {
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(component.Source))
		if info, err := os.Stat(target); err == nil && info.IsDir() {
			continue
		}
		if _, err := os.Lstat(target); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("<template />\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	digest := sha256.Sum256(typeScript)
	document := map[string]any{
		"version":          1,
		"typescriptSha256": hex.EncodeToString(digest[:]),
		"components":       components,
	}
	if mutate != nil {
		mutate(document)
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(builder, "component-definitions.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeStaleComponentRegistry(t *testing.T, root string) {
	t.Helper()
	writeComponentRegistry(t, root, true, defaultComponents(), nil)
	manifest := filepath.Join(root, "app", "content-builder", "component-definitions.json")
	source := filepath.Join(root, filepath.FromSlash(defaultComponents()[0].Source))
	old := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-time.Hour)
	if err := os.Chtimes(manifest, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(source, newer, newer); err != nil {
		t.Fatal(err)
	}
}

func writeMalformedComponentRegistry(t *testing.T, root string) {
	t.Helper()
	builder := filepath.Join(root, "app", "content-builder")
	if err := os.MkdirAll(builder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(builder, "component-definitions.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
}

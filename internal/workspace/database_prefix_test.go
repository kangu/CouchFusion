package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateDBLoginPrefix(t *testing.T) {
	t.Parallel()

	for _, prefix := range []string{"ab", "ns", "zz"} {
		if err := ValidateDBLoginPrefix(prefix); err != nil {
			t.Errorf("ValidateDBLoginPrefix(%q) error = %v", prefix, err)
		}
	}

	for _, prefix := range []string{"", "a", "abc", "Aq", "a1", "a-", "éa"} {
		if err := ValidateDBLoginPrefix(prefix); err == nil {
			t.Errorf("ValidateDBLoginPrefix(%q) error = nil, want validation error", prefix)
		}
	}
}

func TestUpdateDBLoginPrefixPreservesConfigAndMode(t *testing.T) {
	t.Parallel()

	for _, quote := range []string{"'", `"`} {
		quote := quote
		t.Run(quote, func(t *testing.T) {
			t.Parallel()
			targetDir := t.TempDir()
			path := filepath.Join(targetDir, "nuxt.config.ts")
			before := "export default defineNuxtConfig({\n  runtimeConfig: {\n    dbLoginPrefix: " + quote + "ns-" + quote + ",\n    public: { apiBase: '/api' },\n  },\n})\n"
			if err := os.WriteFile(path, []byte(before), 0o640); err != nil {
				t.Fatal(err)
			}

			if err := updateDBLoginPrefix(targetDir, "ac"); err != nil {
				t.Fatalf("updateDBLoginPrefix() error = %v", err)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(before, quote+"ns-"+quote, quote+"ac-"+quote, 1)
			if string(got) != want {
				t.Fatalf("config changed unexpectedly\ngot:\n%s\nwant:\n%s", got, want)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o640 {
				t.Fatalf("mode = %o, want 640", info.Mode().Perm())
			}
			matches, err := filepath.Glob(filepath.Join(targetDir, ".nuxt.config.ts-*"))
			if err != nil {
				t.Fatal(err)
			}
			if len(matches) != 0 {
				t.Fatalf("temporary files remain: %v", matches)
			}
		})
	}
}

func TestUpdateDBLoginPrefixRejectsUnsafeConfig(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"missing property":    "export default defineNuxtConfig({ runtimeConfig: {} })\n",
		"duplicate property":  "export default defineNuxtConfig({ runtimeConfig: { dbLoginPrefix: 'ns-', dbLoginPrefix: 'xy-' } })\n",
		"non-string property": "export default defineNuxtConfig({ runtimeConfig: { dbLoginPrefix: getPrefix() } })\n",
	}
	for name, contents := range tests {
		name, contents := name, contents
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			targetDir := t.TempDir()
			path := filepath.Join(targetDir, "nuxt.config.ts")
			if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
				t.Fatal(err)
			}

			if err := updateDBLoginPrefix(targetDir, "ac"); err == nil {
				t.Fatal("updateDBLoginPrefix() error = nil, want guarded replacement error")
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != contents {
				t.Fatalf("config changed on failure\ngot: %s\nwant: %s", got, contents)
			}
			matches, err := filepath.Glob(filepath.Join(targetDir, ".nuxt.config.ts-*"))
			if err != nil {
				t.Fatal(err)
			}
			if len(matches) != 0 {
				t.Fatalf("temporary files remain: %v", matches)
			}
		})
	}
}

func TestUpdateDBLoginPrefixRejectsMissingConfig(t *testing.T) {
	t.Parallel()

	if err := updateDBLoginPrefix(t.TempDir(), "ac"); err == nil {
		t.Fatal("updateDBLoginPrefix() error = nil, want missing file error")
	}
}

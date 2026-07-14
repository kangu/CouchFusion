package baseline

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/nuxt-apps/couchfusion/internal/agent/model"
)

func TestCaptureAndCompareDistinguishesBaselineDirtyWork(t *testing.T) {
	root := initializeRepository(t)
	writeFile(t, root, "tracked.vue", "committed\n")
	writeFile(t, root, "removed.vue", "remove me\n")
	git(t, root, "add", "tracked.vue", "removed.vue")
	git(t, root, "commit", "-m", "fixture")

	writeFile(t, root, "tracked.vue", "dirty before generation\n")
	writeFile(t, root, "untracked.vue", "untracked before generation\n")
	snapshot, err := Capture(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, root, "tracked.vue", "changed by generation\n")
	writeFile(t, root, "untracked.vue", "changed untracked by generation\n")
	writeFile(t, root, "new.vue", "new by generation\n")
	if err := os.Remove(filepath.Join(root, "removed.vue")); err != nil {
		t.Fatal(err)
	}

	changes, err := Compare(context.Background(), root, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	assertChange(t, changes, "tracked.vue", model.ChangeModified, true)
	assertChange(t, changes, "untracked.vue", model.ChangeModified, true)
	assertChange(t, changes, "new.vue", model.ChangeAdded, false)
	assertChange(t, changes, "removed.vue", model.ChangeDeleted, false)
}

func TestCompareOmitsUnchangedBaselineDirtyFiles(t *testing.T) {
	root := initializeRepository(t)
	writeFile(t, root, "tracked.vue", "committed\n")
	git(t, root, "add", "tracked.vue")
	git(t, root, "commit", "-m", "fixture")
	writeFile(t, root, "tracked.vue", "dirty before generation\n")
	snapshot, err := Capture(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	changes, err := Compare(context.Background(), root, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %#v, want none", changes)
	}
}

func TestCompareRejectsSymlinksOutsideRoot(t *testing.T) {
	root := initializeRepository(t)
	snapshot, err := Capture(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.vue")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escaped.vue")); err != nil {
		t.Fatal(err)
	}
	changes, err := Compare(context.Background(), root, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Fatalf("changes = %#v, want escaped symlink omitted", changes)
	}
}

func TestFingerprintBindsWorkspaceSiteAndCanonicalRoots(t *testing.T) {
	first := Fingerprint("w1", "shop", "/workspace/apps", "/workspace/apps/shop")
	second := Fingerprint("w1", "shop", "/workspace/apps", "/workspace/apps/shop")
	changed := Fingerprint("w1", "other", "/workspace/apps", "/workspace/apps/other")
	if first == "" || first != second || first == changed {
		t.Fatalf("fingerprints = %q, %q, %q", first, second, changed)
	}
}

func initializeRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init")
	git(t, root, "config", "user.email", "fixture@example.test")
	git(t, root, "config", "user.name", "Fixture")
	return root
}

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertChange(t *testing.T, changes []model.ChangedFile, path string, kind model.ChangeKind, baselineDirty bool) {
	t.Helper()
	for _, change := range changes {
		if change.Path == path {
			if change.Kind != kind || change.BaselineDirty != baselineDirty {
				t.Fatalf("change %q = %#v", path, change)
			}
			return
		}
	}
	t.Fatalf("change %q missing from %#v", path, changes)
}

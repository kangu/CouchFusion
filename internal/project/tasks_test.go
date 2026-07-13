package project

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCatalogReturnsInstallThenStandardAndCustomScripts(t *testing.T) {
	site := Site{
		ID:   "shop",
		Root: filepath.Join(string(filepath.Separator), "workspace", "apps", "shop"),
		Scripts: map[string]string{
			"publish-site": "node publish.mjs",
			"test":         "vitest",
			"dev":          "nuxt dev",
			"build":        "nuxt build",
			"preview":      "nuxt preview",
		},
	}

	tasks := (Catalog{}).Tasks(site)

	got := make([]string, len(tasks))
	for index, task := range tasks {
		got[index] = task.ID
	}
	want := []string{"install", "script:dev", "script:build", "script:preview", "script:test", "script:publish-site"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("task IDs = %#v, want %#v", got, want)
	}
	if !tasks[1].LongRunning || !tasks[3].LongRunning || tasks[2].LongRunning {
		t.Fatalf("long-running flags = %#v", tasks)
	}
}

func TestCatalogResolvesCommandsWithoutUsingScriptBodies(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "workspace", "apps", "shop")
	site := Site{Root: root, Scripts: map[string]string{"dev": "nuxt dev --host 0.0.0.0"}}
	catalog := Catalog{}

	install, err := catalog.Resolve(site, "install")
	if err != nil {
		t.Fatal(err)
	}
	if install.Executable != "bun" || !reflect.DeepEqual(install.Args, []string{"install"}) || install.Dir != root {
		t.Fatalf("install command = %#v", install)
	}

	dev, err := catalog.Resolve(site, "script:dev")
	if err != nil {
		t.Fatal(err)
	}
	if dev.Executable != "bun" || !reflect.DeepEqual(dev.Args, []string{"run", "dev"}) || dev.Dir != root {
		t.Fatalf("dev command = %#v", dev)
	}
	if reflect.DeepEqual(dev.Args, []string{"nuxt", "dev", "--host", "0.0.0.0"}) {
		t.Fatalf("script body leaked into args: %#v", dev.Args)
	}
}

func TestCatalogRejectsUndeclaredAndMalformedTaskIDs(t *testing.T) {
	catalog := Catalog{}
	site := Site{Root: "/workspace/apps/shop", Scripts: map[string]string{"dev": "nuxt dev"}}

	for _, taskID := range []string{"script:publish", "dev", "script:", "script:../dev"} {
		if _, err := catalog.Resolve(site, taskID); !errors.Is(err, ErrTaskNotFound) {
			t.Errorf("Resolve(%q) error = %v, want ErrTaskNotFound", taskID, err)
		}
	}
}

func TestCatalogRejectsDeclaredScriptNameThatLooksLikeAnOption(t *testing.T) {
	site := Site{Root: "/workspace/apps/shop", Scripts: map[string]string{"--watch": "node watch.mjs"}}
	tasks := (Catalog{}).Tasks(site)
	if len(tasks) != 1 || tasks[0].ID != "install" {
		t.Fatalf("Tasks() = %#v, want install only", tasks)
	}

	_, err := (Catalog{}).Resolve(site, "script:--watch")

	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("Resolve() error = %v, want ErrTaskNotFound", err)
	}
}

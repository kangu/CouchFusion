package project

import (
	"errors"
	"sort"
	"strings"
)

var ErrTaskNotFound = errors.New("site task was not found")

type Task struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	LongRunning bool   `json:"longRunning"`
}

type Command struct {
	Executable  string
	Args        []string
	Dir         string
	Environment map[string]string
}

type Catalog struct{}

func (Catalog) Tasks(site Site) []Task {
	tasks := []Task{{ID: "install", Name: "Install dependencies", Kind: "install"}}
	standard := []string{"dev", "build", "preview", "test"}
	seen := map[string]struct{}{}
	for _, name := range standard {
		if site.Scripts[name] == "" {
			continue
		}
		seen[name] = struct{}{}
		tasks = append(tasks, scriptTask(name))
	}
	custom := make([]string, 0, len(site.Scripts))
	for name := range site.Scripts {
		if _, exists := seen[name]; !exists && validScriptName(name) {
			custom = append(custom, name)
		}
	}
	sort.Strings(custom)
	for _, name := range custom {
		tasks = append(tasks, scriptTask(name))
	}
	return tasks
}

func (Catalog) Resolve(site Site, taskID string) (Command, error) {
	if taskID == "install" {
		return Command{Executable: "bun", Args: []string{"install"}, Dir: site.Root}, nil
	}
	name, ok := strings.CutPrefix(taskID, "script:")
	if !ok || !validScriptName(name) || site.Scripts[name] == "" {
		return Command{}, ErrTaskNotFound
	}
	return Command{Executable: "bun", Args: []string{"run", name}, Dir: site.Root}, nil
}

func scriptTask(name string) Task {
	return Task{
		ID:          "script:" + name,
		Name:        name,
		Kind:        scriptKind(name),
		LongRunning: name == "dev" || name == "preview",
	}
}

func scriptKind(name string) string {
	switch name {
	case "dev", "build", "preview", "test":
		return name
	default:
		return "script"
	}
}

func validScriptName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && name != "." && name != ".." && !strings.HasPrefix(name, "-") && !strings.ContainsAny(name, `/\`) && !strings.ContainsAny(name, "\r\n\t ")
}

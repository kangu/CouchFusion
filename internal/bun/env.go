package bun

import (
	"os"
	"path/filepath"
	"strings"
)

var osReadFile = os.ReadFile

type Env []string

func (e Env) Get(key string) string {
	prefix := key + "="
	for _, item := range e {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

func (e Env) Set(key, value string) Env {
	prefix := key + "="
	out := make([]string, 0, len(e)+1)
	found := false
	for _, item := range e {
		if strings.HasPrefix(item, prefix) {
			if !found {
				out = append(out, prefix+value)
				found = true
			}
			continue
		}
		out = append(out, item)
	}
	if !found {
		out = append(out, prefix+value)
	}
	return out
}

func RefreshEnv(input []string, home string) Env {
	env := Env(append([]string{}, input...))
	if strings.TrimSpace(home) == "" {
		if resolved, err := os.UserHomeDir(); err == nil {
			home = resolved
		}
	}
	bunInstall := filepath.Join(home, ".bun")
	bunBin := filepath.Join(bunInstall, "bin")
	env = env.Set("BUN_INSTALL", bunInstall)
	path := env.Get("PATH")
	if path == "" {
		path = bunBin
	} else if !pathContains(path, bunBin) {
		path = bunBin + string(os.PathListSeparator) + path
	}
	env = env.Set("PATH", path)
	return env
}

func ApplyEnv(env Env) {
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			_ = os.Setenv(key, value)
		}
	}
}

func ShellProfilePath(home, shell string) string {
	paths := ShellProfilePaths(home, shell)
	if len(paths) == 0 {
		return filepath.Join(home, ".profile")
	}
	return paths[0]
}

func ShellProfilePaths(home, shell string) []string {
	name := filepath.Base(shell)
	switch name {
	case "zsh":
		return []string{
			filepath.Join(home, ".zshrc"),
			filepath.Join(home, ".zprofile"),
		}
	case "bash":
		paths := []string{
			filepath.Join(home, ".bashrc"),
			filepath.Join(home, ".profile"),
		}
		for _, path := range []string{
			filepath.Join(home, ".bash_profile"),
			filepath.Join(home, ".bash_login"),
		} {
			if _, err := os.Stat(path); err == nil {
				paths = append(paths, path)
			}
		}
		return paths
	case "fish":
		return []string{filepath.Join(home, ".config", "fish", "config.fish")}
	default:
		return []string{filepath.Join(home, ".profile")}
	}
}

func EnsureProfileBlocks(paths []string, fish bool) error {
	for _, path := range paths {
		if err := EnsureProfileBlock(path, fish); err != nil {
			return err
		}
	}
	return nil
}

func EnsureProfileBlock(path string, fish bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(existing, "BUN_INSTALL") || strings.Contains(existing, ".bun/bin") {
		return nil
	}
	block := "\n# Bun runtime\nexport BUN_INSTALL=\"$HOME/.bun\"\nexport PATH=\"$BUN_INSTALL/bin:$PATH\"\n"
	if fish {
		block = "\n# Bun runtime\nset -gx BUN_INSTALL $HOME/.bun\nfish_add_path $BUN_INSTALL/bin\n"
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(block)
	return err
}

func EnsureCouchFusionBinShim(home, bunBin string) (string, error) {
	shimDir := filepath.Join(home, ".couchfusion", "bin")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		return "", err
	}
	return ensureBunShimInDir(shimDir, bunBin, true)
}

func EnsureCurrentPathShim(pathValue, bunBin string) (string, error) {
	target := filepath.Join(bunBin, "bun")
	if !fileExists(target) {
		return "", nil
	}
	for _, dir := range filepath.SplitList(pathValue) {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		shim, err := ensureBunShimInDir(dir, bunBin, false)
		if err == nil && shim != "" {
			return shim, nil
		}
	}
	return "", nil
}

func ensureBunShimInDir(shimDir, bunBin string, replaceExisting bool) (string, error) {
	target := filepath.Join(bunBin, "bun")
	shim := filepath.Join(shimDir, "bun")
	if _, err := os.Stat(target); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if existing, err := os.Readlink(shim); err == nil && existing == target {
		return shim, nil
	}
	if !replaceExisting && fileExists(shim) {
		return "", nil
	}
	_ = os.Remove(shim)
	if err := os.Symlink(target, shim); err == nil {
		return shim, nil
	}
	input, err := os.ReadFile(target)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(shim, input, 0o755); err != nil {
		return "", err
	}
	return shim, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func pathContains(pathValue, entry string) bool {
	for _, item := range filepath.SplitList(pathValue) {
		if item == entry {
			return true
		}
	}
	return false
}

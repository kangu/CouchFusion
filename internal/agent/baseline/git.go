package baseline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nuxt-apps/couchfusion/internal/agent/model"
)

type Entry struct {
	Path          string `json:"path"`
	Status        string `json:"status"`
	SHA256        string `json:"sha256"`
	BaselineDirty bool   `json:"baselineDirty,omitempty"`
}

type Snapshot struct {
	CapturedAt time.Time `json:"capturedAt"`
	Files      []Entry   `json:"files"`
}

func Capture(ctx context.Context, root string) (Snapshot, error) {
	root, err := canonicalRoot(root)
	if err != nil {
		return Snapshot{}, err
	}
	dirty, err := dirtyPaths(ctx, root)
	if err != nil {
		return Snapshot{}, err
	}
	paths, err := listedPaths(ctx, root)
	if err != nil {
		return Snapshot{}, err
	}
	entries := make([]Entry, 0, len(paths))
	for _, path := range paths {
		hash, ok, err := hashRegularFile(root, path)
		if err != nil {
			return Snapshot{}, err
		}
		if !ok {
			continue
		}
		status := "tracked"
		if dirty[path] == "untracked" {
			status = "untracked"
		}
		_, baselineDirty := dirty[path]
		entries = append(entries, Entry{Path: path, Status: status, SHA256: hash, BaselineDirty: baselineDirty})
	}
	return Snapshot{CapturedAt: time.Now().UTC(), Files: entries}, nil
}

func Compare(ctx context.Context, root string, snapshot Snapshot) ([]model.ChangedFile, error) {
	root, err := canonicalRoot(root)
	if err != nil {
		return nil, err
	}
	paths, err := listedPaths(ctx, root)
	if err != nil {
		return nil, err
	}
	before := make(map[string]Entry, len(snapshot.Files))
	for _, entry := range snapshot.Files {
		if validRelativePath(entry.Path) {
			before[entry.Path] = entry
		}
	}
	after := make(map[string]string, len(paths))
	for _, path := range paths {
		hash, ok, err := hashRegularFile(root, path)
		if err != nil {
			return nil, err
		}
		if ok {
			after[path] = hash
		}
	}
	union := make(map[string]struct{}, len(before)+len(after))
	for path := range before {
		union[path] = struct{}{}
	}
	for path := range after {
		union[path] = struct{}{}
	}
	changes := make([]model.ChangedFile, 0)
	for path := range union {
		old, existed := before[path]
		newHash, exists := after[path]
		var kind model.ChangeKind
		switch {
		case !existed && exists:
			kind = model.ChangeAdded
		case existed && !exists:
			kind = model.ChangeDeleted
		case existed && exists && old.SHA256 != newHash:
			kind = model.ChangeModified
		default:
			continue
		}
		changes = append(changes, model.ChangedFile{Path: path, Kind: kind, BaselineDirty: old.BaselineDirty})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

func Fingerprint(workspaceID, siteID, appsRoot, siteRoot string) string {
	hash := sha256.New()
	for _, value := range []string{workspaceID, siteID, filepath.Clean(appsRoot), filepath.Clean(siteRoot)} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func dirtyPaths(ctx context.Context, root string) (map[string]string, error) {
	output, err := gitOutput(ctx, root, "status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	records := strings.Split(string(output), "\x00")
	for index := 0; index < len(records); index++ {
		record := records[index]
		if record == "" {
			continue
		}
		var path, status string
		switch record[0] {
		case '?':
			path, status = strings.TrimPrefix(record, "? "), "untracked"
		case '1', 'u':
			parts := strings.SplitN(record, " ", 9)
			if len(parts) == 9 {
				path, status = parts[8], "tracked"
			}
		case '2':
			parts := strings.SplitN(record, " ", 10)
			if len(parts) == 10 {
				path, status = parts[9], "tracked"
				index++ // original rename path is the next NUL record
			}
		}
		path = filepath.ToSlash(path)
		if validRelativePath(path) {
			result[path] = status
		}
	}
	return result, nil
}

func listedPaths(ctx context.Context, root string) ([]string, error) {
	output, err := gitOutput(ctx, root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for _, raw := range strings.Split(string(output), "\x00") {
		path := filepath.ToSlash(raw)
		if validRelativePath(path) {
			seen[path] = struct{}{}
		}
	}
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func hashRegularFile(root, relative string) (string, bool, error) {
	if !validRelativePath(relative) {
		return "", false, nil
	}
	filename := filepath.Join(root, filepath.FromSlash(relative))
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() {
		return "", false, nil
	}
	resolved, err := filepath.EvalSymlinks(filename)
	if err != nil {
		return "", false, err
	}
	contained, err := filepath.Rel(root, resolved)
	if err != nil || !validRelativePath(filepath.ToSlash(contained)) {
		return "", false, nil
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return "", false, err
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:]), true, nil
}

func canonicalRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("repository root is required")
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	return filepath.Abs(resolved)
}

func validRelativePath(path string) bool {
	if path == "" || strings.ContainsRune(path, 0) || filepath.IsAbs(filepath.FromSlash(path)) {
		return false
	}
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	return cleaned == path && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func gitOutput(ctx context.Context, root string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s failed: %w", strings.Join(args, " "), err)
	}
	return output, nil
}

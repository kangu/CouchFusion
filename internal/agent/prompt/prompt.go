package prompt

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nuxt-apps/couchfusion/internal/agent/protocol"
)

const (
	SkillName      = "couchfusion-component-builder"
	MaxPromptBytes = 16 * 1024
)

var (
	ErrInvalidPrompt     = errors.New("agent prompt is invalid")
	ErrUnsafeDestination = errors.New("agent skill destination is unsafe")
)

//go:embed skill/couchfusion-component-builder/SKILL.md
var embeddedSkill []byte

type Skill struct {
	Name   string
	Root   string
	Path   string
	Digest string
}

func MaterializeSkill(stateRoot string) (Skill, error) {
	digestBytes := sha256.Sum256(embeddedSkill)
	digest := hex.EncodeToString(digestBytes[:])
	root := filepath.Join(stateRoot, "skills", digest)
	skillDir := filepath.Join(root, SkillName)
	path := filepath.Join(skillDir, "SKILL.md")
	result := Skill{Name: SkillName, Root: root, Path: path, Digest: digest}

	if strings.TrimSpace(stateRoot) == "" {
		return Skill{}, fmt.Errorf("%w: state root is required", ErrUnsafeDestination)
	}
	for _, dir := range []string{stateRoot, filepath.Join(stateRoot, "skills"), root, skillDir} {
		if err := ensurePrivateDirectory(dir); err != nil {
			return Skill{}, err
		}
	}

	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return Skill{}, fmt.Errorf("%w: skill file is not regular", ErrUnsafeDestination)
		}
		current, readErr := os.ReadFile(path)
		if readErr != nil {
			return Skill{}, fmt.Errorf("read materialized skill: %w", readErr)
		}
		if string(current) != string(embeddedSkill) {
			return Skill{}, fmt.Errorf("%w: content-addressed skill was modified", ErrUnsafeDestination)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return Skill{}, fmt.Errorf("secure materialized skill: %w", err)
		}
		return result, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Skill{}, fmt.Errorf("inspect materialized skill: %w", err)
	}

	temporary, err := os.CreateTemp(skillDir, ".SKILL.md-*")
	if err != nil {
		return Skill{}, fmt.Errorf("create temporary skill: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return Skill{}, fmt.Errorf("secure temporary skill: %w", err)
	}
	if _, err := temporary.Write(embeddedSkill); err != nil {
		_ = temporary.Close()
		return Skill{}, fmt.Errorf("write temporary skill: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return Skill{}, fmt.Errorf("sync temporary skill: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return Skill{}, fmt.Errorf("close temporary skill: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return Skill{}, fmt.Errorf("install materialized skill: %w", err)
	}
	removeTemporary = false
	return result, nil
}

func InitialTurn(userPrompt string, skill Skill) (protocol.TurnInput, error) {
	if err := validatePrompt(userPrompt); err != nil {
		return nil, err
	}
	if skill.Name != SkillName || strings.TrimSpace(skill.Path) == "" {
		return nil, fmt.Errorf("%w: managed skill is unavailable", ErrInvalidPrompt)
	}
	return protocol.TurnInput{
		{Type: "text", Text: "$" + SkillName + " Follow the attached first-party skill for this component generation."},
		{Type: "skill", Name: skill.Name, Path: skill.Path},
		{Type: "text", Text: userPrompt},
	}, nil
}

func FollowUp(userPrompt string) (protocol.TurnInput, error) {
	if err := validatePrompt(userPrompt); err != nil {
		return nil, err
	}
	return protocol.TurnInput{{Type: "text", Text: userPrompt}}, nil
}

func DeveloperInstructions() string {
	return `You are generating or updating CouchFusion Content components. Modify files only within the selected application root. Preserve all pre-existing uncommitted work and avoid unrelated formatting or rewrites. Do not stage, commit, branch, tag, reset, clean, restore, or otherwise change Git history or index state. Dependency manifest or lockfile changes require explicit consent for this generation before editing or running a mutating package command. Network access requires a separate explicit approval. Inspect applicable repository instructions and established Content component, builder metadata, style, test, and registry conventions. Run focused validation using installed project tooling. Update source component metadata and use the canonical registry generator; do not hand-edit generated registry TypeScript or JSON. Report relative changed files and validation results, then leave review and commit to the user.`
}

func validatePrompt(userPrompt string) error {
	trimmed := strings.TrimSpace(userPrompt)
	if trimmed == "" {
		return fmt.Errorf("%w: prompt is required", ErrInvalidPrompt)
	}
	if len([]byte(trimmed)) > MaxPromptBytes {
		return fmt.Errorf("%w: prompt exceeds %d bytes", ErrInvalidPrompt, MaxPromptBytes)
	}
	return nil
}

func ensurePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%w: %q is not a directory", ErrUnsafeDestination, path)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return fmt.Errorf("secure skill directory: %w", err)
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect skill directory: %w", err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return fmt.Errorf("create skill directory: %w", err)
	}
	return nil
}

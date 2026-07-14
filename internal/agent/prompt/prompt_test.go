package prompt

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMaterializeSkillCreatesPrivateContentAddressedSkill(t *testing.T) {
	stateRoot := t.TempDir()

	skill, err := MaterializeSkill(stateRoot)
	if err != nil {
		t.Fatalf("MaterializeSkill() error = %v", err)
	}
	if skill.Name != "couchfusion-component-builder" {
		t.Fatalf("Name = %q", skill.Name)
	}
	if skill.Digest == "" {
		t.Fatal("Digest is empty")
	}
	wantRoot := filepath.Join(stateRoot, "skills", skill.Digest)
	wantPath := filepath.Join(wantRoot, skill.Name, "SKILL.md")
	if skill.Root != wantRoot || skill.Path != wantPath {
		t.Fatalf("skill = %#v, want root %q and path %q", skill, wantRoot, wantPath)
	}

	for _, path := range []string{filepath.Join(stateRoot, "skills"), wantRoot, filepath.Dir(wantPath)} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat %q: %v", path, statErr)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("mode %q = %o, want 700", path, got)
		}
	}
	info, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("stat skill: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("skill mode = %o, want 600", got)
	}
	firstModTime := info.ModTime()

	time.Sleep(10 * time.Millisecond)
	again, err := MaterializeSkill(stateRoot)
	if err != nil {
		t.Fatalf("second MaterializeSkill() error = %v", err)
	}
	if again != skill {
		t.Fatalf("second skill = %#v, want %#v", again, skill)
	}
	info, err = os.Stat(wantPath)
	if err != nil {
		t.Fatalf("stat materialized skill again: %v", err)
	}
	if !info.ModTime().Equal(firstModTime) {
		t.Fatalf("idempotent materialization rewrote skill: %v != %v", info.ModTime(), firstModTime)
	}
}

func TestMaterializeSkillRejectsSymlinkDestination(t *testing.T) {
	reference, err := MaterializeSkill(t.TempDir())
	if err != nil {
		t.Fatalf("reference MaterializeSkill() error = %v", err)
	}
	stateRoot := t.TempDir()
	digestRoot := filepath.Join(stateRoot, "skills", reference.Digest)
	if err := os.MkdirAll(digestRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(digestRoot, reference.Name)); err != nil {
		t.Fatal(err)
	}

	if _, err := MaterializeSkill(stateRoot); !errors.Is(err, ErrUnsafeDestination) {
		t.Fatalf("MaterializeSkill() error = %v, want ErrUnsafeDestination", err)
	}
}

func TestInitialTurnKeepsContextLayersDistinct(t *testing.T) {
	skill, err := MaterializeSkill(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	userPrompt := "  Build a compact testimonial card.  "
	input, err := InitialTurn(userPrompt, skill)
	if err != nil {
		t.Fatalf("InitialTurn() error = %v", err)
	}
	if len(input) != 3 {
		t.Fatalf("len(input) = %d, want 3: %#v", len(input), input)
	}
	if input[0].Type != "text" || !strings.HasPrefix(input[0].Text, "$couchfusion-component-builder") {
		t.Fatalf("first input = %#v", input[0])
	}
	if input[1].Type != "skill" || input[1].Name != skill.Name || input[1].Path != skill.Path {
		t.Fatalf("skill input = %#v", input[1])
	}
	if input[2].Type != "text" || input[2].Text != userPrompt {
		t.Fatalf("user input = %#v, want exact prompt %q", input[2], userPrompt)
	}
}

func TestDeveloperInstructionsEnforceProductPolicy(t *testing.T) {
	instructions := strings.ToLower(DeveloperInstructions())
	for _, required := range []string{
		"selected application root",
		"pre-existing uncommitted",
		"do not stage",
		"dependency",
		"explicit consent",
		"network",
		"focused validation",
		"canonical registry generator",
		"do not hand-edit generated registry",
	} {
		if !strings.Contains(instructions, required) {
			t.Errorf("DeveloperInstructions() missing %q", required)
		}
	}
}

func TestFollowUpContainsOnlyUserPrompt(t *testing.T) {
	userPrompt := "Please make the spacing tighter."
	input, err := FollowUp(userPrompt)
	if err != nil {
		t.Fatalf("FollowUp() error = %v", err)
	}
	if len(input) != 1 || input[0].Type != "text" || input[0].Text != userPrompt {
		t.Fatalf("FollowUp() = %#v", input)
	}
	if strings.Contains(input[0].Text, DeveloperInstructions()) {
		t.Fatal("follow-up contains developer instructions")
	}
}

func TestPromptValidation(t *testing.T) {
	skill := Skill{Name: "couchfusion-component-builder", Path: "/managed/SKILL.md"}
	tests := []struct {
		name   string
		prompt string
	}{
		{name: "blank", prompt: " \n\t "},
		{name: "over limit after trim", prompt: strings.Repeat("x", MaxPromptBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name+" initial", func(t *testing.T) {
			if _, err := InitialTurn(test.prompt, skill); !errors.Is(err, ErrInvalidPrompt) {
				t.Fatalf("InitialTurn() error = %v, want ErrInvalidPrompt", err)
			}
		})
		t.Run(test.name+" follow-up", func(t *testing.T) {
			if _, err := FollowUp(test.prompt); !errors.Is(err, ErrInvalidPrompt) {
				t.Fatalf("FollowUp() error = %v, want ErrInvalidPrompt", err)
			}
		})
	}

	boundary := strings.Repeat("x", MaxPromptBytes)
	if _, err := InitialTurn(boundary, skill); err != nil {
		t.Fatalf("InitialTurn() at boundary error = %v", err)
	}
}

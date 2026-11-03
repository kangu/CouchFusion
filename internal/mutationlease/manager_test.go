package mutationlease

import (
	"errors"
	"testing"
)

func TestManagerSerializesOwnersAndReleaseIsIdempotent(t *testing.T) {
	leases := NewManager()
	release, err := leases.Acquire("workspace-1", "job-1", "job")
	if err != nil {
		t.Fatal(err)
	}

	_, err = leases.Acquire("workspace-1", "conversation-1", "agent")
	var conflict ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("Acquire() error = %#v, want ConflictError", err)
	}
	if conflict.Scope != "workspace-1" || conflict.ActiveOwnerID != "job-1" || conflict.ActiveOwnerType != "job" {
		t.Fatalf("conflict = %#v", conflict)
	}

	release()
	release()
	if _, err := leases.Acquire("workspace-1", "conversation-1", "agent"); err != nil {
		t.Fatalf("Acquire() after release error = %v", err)
	}
}

func TestManagerDoesNotLeaseEmptyScope(t *testing.T) {
	leases := NewManager()
	firstRelease, err := leases.Acquire("", "job-1", "job")
	if err != nil {
		t.Fatal(err)
	}
	secondRelease, err := leases.Acquire("", "job-2", "job")
	if err != nil {
		t.Fatal(err)
	}
	firstRelease()
	secondRelease()
}

func TestManagerRejectsBlankOwnerMetadata(t *testing.T) {
	leases := NewManager()
	for _, test := range []struct {
		ownerID   string
		ownerType string
	}{
		{ownerID: "", ownerType: "job"},
		{ownerID: "job-1", ownerType: ""},
	} {
		if _, err := leases.Acquire("workspace-1", test.ownerID, test.ownerType); !errors.Is(err, ErrInvalidOwner) {
			t.Fatalf("Acquire(%q, %q) error = %v, want ErrInvalidOwner", test.ownerID, test.ownerType, err)
		}
	}
}

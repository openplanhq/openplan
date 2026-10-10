package runuser

import (
	"context"
	"os"
	"testing"
)

func TestPoolUsersMatchTheImage(t *testing.T) {
	t.Parallel()

	users := PoolUsers()
	if len(users) != PoolSize {
		t.Fatalf("len = %d, want %d", len(users), PoolSize)
	}
	first, last := users[0], users[len(users)-1]
	if first != (User{Name: "openplan-run-1", UID: 70001, GID: 70001}) {
		t.Fatalf("first = %+v", first)
	}
	if last != (User{Name: "openplan-run-40", UID: 70040, GID: 70040}) {
		t.Fatalf("last = %+v", last)
	}
}

func TestIsPoolUID(t *testing.T) {
	t.Parallel()

	for uid, want := range map[int]bool{0: false, 10001: false, 70000: false, 70001: true, 70040: true, 70041: false} {
		if got := IsPoolUID(uid); got != want {
			t.Errorf("IsPoolUID(%d) = %v, want %v", uid, got, want)
		}
	}
}

// Off root a process can only "switch" to itself, which is how unit tests run
// every branch as the current user.
func TestCredentialAsTheCurrentUserNeedsNoSwitch(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("as root every user gets a credential")
	}

	credential, err := DevelopmentUsers(1)[0].Credential()
	if err != nil || credential != nil {
		t.Fatalf("Credential() = %v, %v; want nil, nil", credential, err)
	}
}

func TestCredentialForAnotherUserNeedsRoot(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root can switch to any user")
	}

	if _, err := PoolUsers()[0].Credential(); err == nil {
		t.Fatal("Credential() for another uid returned no error without root")
	}
}

// As root the child must also drop root's supplementary groups, or it keeps
// group access to whatever root's groups can read.
func TestCredentialAsRootClearsGroups(t *testing.T) {
	t.Parallel()
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}

	credential, err := PoolUsers()[0].Credential()
	if err != nil {
		t.Fatal(err)
	}
	if credential.Uid != 70001 || credential.Gid != 70001 || credential.Groups == nil || len(credential.Groups) != 0 || credential.NoSetGroups {
		t.Fatalf("credential = %+v", credential)
	}
}

func TestBranchTravelsInTheContext(t *testing.T) {
	t.Parallel()

	if _, ok := BranchFrom(context.Background()); ok {
		t.Fatal("empty context has a branch")
	}
	branch := Branch{User: PoolUsers()[2], Home: "/w/home", TempDir: "/w/tmp"}
	got, ok := BranchFrom(WithBranch(context.Background(), branch))
	if !ok || got != branch {
		t.Fatalf("BranchFrom = %+v, %v; want %+v", got, ok, branch)
	}
}

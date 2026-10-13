package isolation

import (
	"os"
	"strings"
	"testing"
)

func TestPoolUsersMatchTheImage(t *testing.T) {
	t.Parallel()

	users := poolUsers()
	if len(users) != PoolSize {
		t.Fatalf("len = %d, want %d", len(users), PoolSize)
	}
	first, last := users[0], users[len(users)-1]
	if first != (user{name: "openplan-run-1", uid: 70001, gid: 70001}) {
		t.Fatalf("first = %+v", first)
	}
	if last != (user{name: "openplan-run-40", uid: 70040, gid: 70040}) {
		t.Fatalf("last = %+v", last)
	}
}

func TestIsPoolUID(t *testing.T) {
	t.Parallel()

	for uid, want := range map[int]bool{0: false, 10001: false, 70000: false, 70001: true, 70040: true, 70041: false} {
		if got := isPoolUID(uid); got != want {
			t.Errorf("isPoolUID(%d) = %v, want %v", uid, got, want)
		}
	}
}

// Off root a process can only "switch" to itself, which is how unit tests run
// every session as the current user.
func TestCredentialAsTheCurrentUserNeedsNoSwitch(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("as root every user gets a credential")
	}

	credential, err := developmentUsers(1)[0].credential()
	if err != nil || credential != nil {
		t.Fatalf("credential() = %v, %v; want nil, nil", credential, err)
	}
}

func TestCredentialForAnotherUserNeedsRoot(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root can switch to any pool user")
	}

	if _, err := poolUsers()[0].credential(); err == nil {
		t.Fatal("credential() for another uid returned no error without root")
	}
}

// As root the child must also drop root's supplementary groups, or it keeps
// group access to whatever root's groups can read.
func TestCredentialAsRootClearsGroups(t *testing.T) {
	t.Parallel()
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}

	credential, err := poolUsers()[0].credential()
	if err != nil {
		t.Fatal(err)
	}
	if credential.Uid != 70001 || credential.Gid != 70001 || credential.Groups == nil || len(credential.Groups) != 0 || credential.NoSetGroups {
		t.Fatalf("credential = %+v", credential)
	}
}

// A user nobody filled in has uid 0. As root, starting a child as it would
// run tenant code as root, with root's capabilities.
func TestCredentialAsRootRefusesUsersOutsideThePool(t *testing.T) {
	t.Parallel()
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}

	for _, u := range []user{{}, {name: "openplan", uid: 10001, gid: 10001}} {
		if _, err := u.credential(); err == nil || !strings.Contains(err.Error(), "not a pool user") {
			t.Errorf("credential() for uid %d = %v, want a refusal", u.uid, err)
		}
	}
}

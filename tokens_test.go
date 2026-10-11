package main

import (
	"errors"
	"net/netip"
	"testing"
	"time"

	"heckel.io/ntfy/v2/user"
)

func deviceManager(t *testing.T) (*user.Manager, *user.User) {
	t.Helper()
	m, err := openUserManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	if err := m.AddUser(deviceUser, "device-password", user.RoleUser, false); err != nil {
		t.Fatal(err)
	}
	u, err := m.User(deviceUser)
	if err != nil {
		t.Fatal(err)
	}
	return m, u
}

func TestRoomTokensDoNotExpire(t *testing.T) {
	m, _ := deviceManager(t)
	token, err := issueRoomToken(m, "tossling", neverExpires, netip.MustParseAddr("192.0.2.7"))
	if err != nil {
		t.Fatal(err)
	}
	if token.Expires.Unix() != 0 {
		t.Fatalf("expires = %v", token.Expires)
	}
	if _, err := m.AuthenticateToken(token.Value); err != nil {
		t.Fatalf("a new room token does not authenticate: %v", err)
	}
}

func TestAnExplicitExpiryIsKept(t *testing.T) {
	m, _ := deviceManager(t)
	at := time.Now().Add(time.Hour).Truncate(time.Second)
	token, err := issueRoomToken(m, "short", at, netip.IPv4Unspecified())
	if err != nil {
		t.Fatal(err)
	}
	if !token.Expires.Equal(at) {
		t.Fatalf("expires = %v, want %v", token.Expires, at)
	}
}

func TestOldRoomTokensStopExpiring(t *testing.T) {
	m, u := deviceManager(t)
	alive, err := m.CreateToken(u.ID, "tossling", time.Now().Add(72*time.Hour), netip.IPv4Unspecified(), false)
	if err != nil {
		t.Fatal(err)
	}
	dead, err := m.CreateToken(u.ID, "tossling", time.Now().Add(-time.Minute), netip.IPv4Unspecified(), false)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := keepRoomTokens(m)
	if err != nil {
		t.Fatal(err)
	}
	if kept != 1 {
		t.Fatalf("kept = %d, want 1", kept)
	}
	if got, err := m.Token(u.ID, alive.Value); err != nil || got.Expires.Unix() != 0 {
		t.Fatalf("the live token: %v, %v", got, err)
	}
	if _, err := m.AuthenticateToken(dead.Value); err == nil {
		t.Fatal("an expired token came back to life")
	}
}

func TestTheServerRefusesTokensPastTheLimitInsteadOfDroppingOne(t *testing.T) {
	m, u := deviceManager(t)
	var first *user.Token
	for i := 0; i < roomTokenLimit; i++ {
		token, err := issueRoomToken(m, "tossling", neverExpires, netip.IPv4Unspecified())
		if err != nil {
			t.Fatalf("token %d: %v", i, err)
		}
		if first == nil {
			first = token
		}
	}
	if _, err := issueRoomToken(m, "tossling", neverExpires, netip.IPv4Unspecified()); !errors.Is(err, errRoomTokenLimit) {
		t.Fatalf("err = %v, want the limit", err)
	}
	tokens, err := m.Tokens(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != roomTokenLimit {
		t.Fatalf("%d tokens, want %d", len(tokens), roomTokenLimit)
	}
	if _, err := m.AuthenticateToken(first.Value); err != nil {
		t.Fatalf("the first room lost its token: %v", err)
	}
}

func TestExpiredTokensMakeRoomForNewOnes(t *testing.T) {
	m, u := deviceManager(t)
	for i := 0; i < roomTokenLimit; i++ {
		if _, err := m.CreateToken(u.ID, "tossling", time.Now().Add(-time.Minute), netip.IPv4Unspecified(), false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := issueRoomToken(m, "tossling", neverExpires, netip.IPv4Unspecified()); err != nil {
		t.Fatal(err)
	}
	tokens, err := m.Tokens(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 {
		t.Fatalf("%d tokens left, want 1", len(tokens))
	}
}

func TestThePanelRemovesARoomTokenByItsID(t *testing.T) {
	m, _ := deviceManager(t)
	token, err := issueRoomToken(m, "tossling", neverExpires, netip.IPv4Unspecified())
	if err != nil {
		t.Fatal(err)
	}
	listed, err := roomTokens(m)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != roomTokenID(token.Value) || listed[0].ID == token.Value {
		t.Fatalf("listed = %+v", listed)
	}
	if err := removeRoomToken(m, listed[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AuthenticateToken(token.Value); err == nil {
		t.Fatal("the removed token still works")
	}
	if err := removeRoomToken(m, listed[0].ID); err == nil {
		t.Fatal("removing it twice succeeded")
	}
}

func TestADeviceGetsATokenOfItsOwnAndTheRoomTokenStays(t *testing.T) {
	m, u := deviceManager(t)
	shared, err := issueRoomToken(m, "tossling", neverExpires, netip.IPv4Unspecified())
	if err != nil {
		t.Fatal(err)
	}
	own, err := issueDeviceToken(m, "pixel1", "Pixel  9", netip.MustParseAddr("192.0.2.9"))
	if err != nil {
		t.Fatal(err)
	}
	if own.Expires.Unix() != 0 || own.Label != "tossling-device:pixel1:Pixel 9" {
		t.Fatalf("own = %+v", own)
	}
	if _, err := m.AuthenticateToken(shared.Value); err != nil {
		t.Fatal("the room token must stay for invites and older devices")
	}
	again, err := issueDeviceToken(m, "pixel1", "Pixel 9", netip.IPv4Unspecified())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AuthenticateToken(own.Value); err == nil {
		t.Fatal("a device keeps one token: the old one must go")
	}
	tokens, _ := m.Tokens(u.ID)
	if len(tokens) != 2 {
		t.Fatalf("%d tokens, want the room one and the new device one", len(tokens))
	}
	if _, err := m.AuthenticateToken(again.Value); err != nil {
		t.Fatal(err)
	}
}

func TestRemovingADeviceCutsOffOnlyThatDevice(t *testing.T) {
	m, _ := deviceManager(t)
	pixel, _ := issueDeviceToken(m, "pixel1", "Pixel", netip.IPv4Unspecified())
	mac, _ := issueDeviceToken(m, "mac1", "Mac", netip.IPv4Unspecified())
	removed, err := removeDeviceTokens(m, "pixel1")
	if err != nil || removed != 1 {
		t.Fatalf("removed %d, %v", removed, err)
	}
	if _, err := m.AuthenticateToken(pixel.Value); err == nil {
		t.Fatal("the removed device still gets in")
	}
	if _, err := m.AuthenticateToken(mac.Value); err != nil {
		t.Fatal("another device lost its token")
	}
	if removed, err := removeDeviceTokens(m, "pixel1"); err != nil || removed != 0 {
		t.Fatalf("removing twice: %d, %v", removed, err)
	}
}

func TestDeviceIDsAreChecked(t *testing.T) {
	m, _ := deviceManager(t)
	for _, id := range []string{"", "a:b", "white space", "../x"} {
		if _, err := issueDeviceToken(m, id, "x", netip.IPv4Unspecified()); !errors.Is(err, errDeviceID) {
			t.Fatalf("id %q: %v", id, err)
		}
	}
}

func TestADeviceTokenPastTheLimitIsRefusedButARenewalFits(t *testing.T) {
	m, _ := deviceManager(t)
	if _, err := issueDeviceToken(m, "pixel1", "Pixel", netip.IPv4Unspecified()); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < roomTokenLimit; i++ {
		if _, err := issueRoomToken(m, "tossling", neverExpires, netip.IPv4Unspecified()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := issueDeviceToken(m, "mac1", "Mac", netip.IPv4Unspecified()); !errors.Is(err, errRoomTokenLimit) {
		t.Fatalf("err = %v", err)
	}
	if _, err := issueDeviceToken(m, "pixel1", "Pixel", netip.IPv4Unspecified()); err != nil {
		t.Fatalf("replacing a device's own token must fit: %v", err)
	}
}

func TestThePanelShowsDevicesByName(t *testing.T) {
	m, _ := deviceManager(t)
	if _, err := issueDeviceToken(m, "pixel1", "Pixel 9", netip.IPv4Unspecified()); err != nil {
		t.Fatal(err)
	}
	listed, _ := roomTokens(m)
	if len(listed) != 1 || listed[0].Label != "Pixel 9" || listed[0].Device != "pixel1" {
		t.Fatalf("listed = %+v", listed)
	}
}

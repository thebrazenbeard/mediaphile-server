package auth

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

func authRepo(t *testing.T) *catalog.Repository {
	t.Helper()
	db, err := catalog.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return catalog.NewRepository(db)
}

func TestPasswordHashAndVerify(t *testing.T) {
	encoded, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if encoded == "correct horse battery staple" {
		t.Fatal("password stored in plaintext")
	}
	if !VerifyPassword("correct horse battery staple", encoded) {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword("wrong", encoded) {
		t.Fatal("wrong password accepted")
	}
}

func TestBootstrapIsOneTimeAndSessionsAreHashedAndRevocable(t *testing.T) {
	ctx := context.Background()
	repo := authRepo(t)
	svc := NewWithBootstrap(repo, "bootstrap-secret")
	principal, token, err := svc.Bootstrap(ctx, "bootstrap-secret", "patrick", "strong-password")
	if err != nil {
		t.Fatal(err)
	}
	if !principal.Admin || token == "" {
		t.Fatalf("unexpected bootstrap result: %#v token=%q", principal, token)
	}
	if _, _, err := svc.Bootstrap(ctx, "bootstrap-secret", "second", "password"); err != ErrAlreadyInitialized {
		t.Fatalf("second bootstrap err=%v want=%v", err, ErrAlreadyInitialized)
	}
	var stored string
	if err := repo.DB().QueryRow("SELECT token_hash FROM auth_sessions LIMIT 1").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == token || len(stored) != 64 {
		t.Fatalf("stored session is not a SHA-256 verifier: %q", stored)
	}

	got, err := svc.Authenticate(ctx, token)
	if err != nil || got.ID != principal.ID {
		t.Fatalf("authenticate=%#v err=%v", got, err)
	}
	if err := svc.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, token); err != ErrInvalidSession && err != sql.ErrNoRows {
		t.Fatalf("revoked token err=%v", err)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	ctx := context.Background()
	repo := authRepo(t)
	svc := NewWithBootstrap(repo, "bootstrap-secret")
	if _, _, err := svc.Bootstrap(ctx, "bootstrap-secret", "patrick", "correct-password"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Login(ctx, "patrick", "wrong-password"); err != ErrInvalidCredentials {
		t.Fatalf("err=%v want=%v", err, ErrInvalidCredentials)
	}
}

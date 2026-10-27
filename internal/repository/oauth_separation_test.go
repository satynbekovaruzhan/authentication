package repository

import (
	"testing"
	"time"
)

func TestOAuthProvidersHaveSeparateUsers(t *testing.T) {
	r, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	google, err := r.CreateOAuthUser(OAuthAccount{Provider: "google", ProviderID: "g1", Email: "same@example.com", Username: "person_google"})
	if err != nil {
		t.Fatal(err)
	}
	github, err := r.CreateOAuthUser(OAuthAccount{Provider: "github", ProviderID: "123", Email: "same@example.com", Username: "person_github"})
	if err != nil {
		t.Fatal(err)
	}
	if google == github {
		t.Fatal("providers share a user")
	}
	for provider, identity := range map[string]string{"google": "g1", "github": "123"} {
		user, err := r.UserByOAuth(provider, identity)
		if err != nil {
			t.Fatal(err)
		}
		expected := google
		if provider == "github" {
			expected = github
		}
		if user.ID != expected {
			t.Fatalf("%s resolves to wrong user", provider)
		}
	}
	local, err := r.CreateUser(NewUser{Email: "same@example.com", Username: "local", PasswordHash: "test-hash"})
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := r.UserCredentialsByLogin("same@example.com")
	if err != nil || id != local {
		t.Fatalf("password login: id=%d err=%v", id, err)
	}
	if _, err := r.CreateUser(NewUser{Email: "same@example.com", Username: "duplicate", PasswordHash: "test-hash"}); err == nil {
		t.Fatal("duplicate password email accepted")
	}
	// A failed link must not leave an orphan user.
	if _, err := r.CreateOAuthUser(OAuthAccount{Provider: "github", ProviderID: "123", Email: "same@example.com", Username: "orphan"}); err == nil {
		t.Fatal("duplicate identity accepted")
	}
	exists, err := r.UsernameExists("orphan")
	if err != nil || exists {
		t.Fatalf("orphan user: exists=%v err=%v", exists, err)
	}
}

func TestMigrationSplitsExistingLinksAndKeepsPosts(t *testing.T) {
	r, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	original, err := r.CreateOAuthUser(OAuthAccount{Provider: "google", ProviderID: "g1", Email: "same@example.com", Username: "original"})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.LinkOAuthAccount(original, "github", "123"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.Exec("INSERT INTO posts (user_id, title, content) VALUES (?, 'Keep', 'Existing content')", original); err != nil {
		t.Fatal(err)
	}
	if err := r.CreateSession(original, "old-session", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.Exec("DELETE FROM schema_migrations WHERE name = 'separate_oauth_v1'"); err != nil {
		t.Fatal(err)
	}
	if err := migrateSeparateOAuthAccounts(r.db); err != nil {
		t.Fatal(err)
	}
	google, err := r.UserByOAuth("google", "g1")
	if err != nil {
		t.Fatal(err)
	}
	github, err := r.UserByOAuth("github", "123")
	if err != nil {
		t.Fatal(err)
	}
	if google.ID != original || github.ID == original {
		t.Fatal("existing account was not split correctly")
	}
	var owner int
	if err := r.db.QueryRow("SELECT user_id FROM posts WHERE title = 'Keep'").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != original {
		t.Fatal("existing post moved")
	}
	if _, err := r.UserBySession("old-session", time.Now()); err != ErrNotFound {
		t.Fatalf("old session remains: %v", err)
	}
	if err := migrateSeparateOAuthAccounts(r.db); err != nil {
		t.Fatal(err)
	}
	again, err := r.UserByOAuth("github", "123")
	if err != nil || again.ID != github.ID {
		t.Fatal("migration is not idempotent")
	}
}

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Each check here is something that would be a security bug if it broke.
func TestPassword(t *testing.T) {
	const pw = "correct horse"

	hash, err := hashPassword(pw)
	if err != nil {
		t.Fatalf("hashPassword(%q) failed: %v", pw, err)
	}

	if !checkPassword(hash, pw) {
		t.Error("correct password was rejected")
	}
	if checkPassword(hash, "wrong") {
		t.Error("wrong password was accepted")
	}

	// Catches storing the password itself, or a silently changed cost.
	if hash == pw || !strings.HasPrefix(hash, "$2a$12$") {
		t.Errorf("hash = %q, want a bcrypt cost 12 hash", hash)
	}

	// The salt is random, so the same password must not hash the same twice.
	if again, _ := hashPassword(pw); again == hash {
		t.Error("two hashes of the same password are identical")
	}

	// bcrypt ignores everything past 72 bytes; that must be an error, not a
	// silent truncation.
	if _, err := hashPassword(strings.Repeat("a", 73)); !errors.Is(err, bcrypt.ErrPasswordTooLong) {
		t.Errorf("73 byte password: err = %v, want ErrPasswordTooLong", err)
	}

	if checkPassword("not-a-hash", pw) {
		t.Error("a malformed hash matched")
	}
}

// Runs against a real Postgres, because the thing under test is the SQL.
// Skipped when DATABASE_URL is not set:
//
//	env (cat .env) go test .
func TestUserStore(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	// A unique address per run, removed afterwards, so the test never
	// touches real rows.
	email := fmt.Sprintf("test-%d@example.invalid", time.Now().UnixNano())
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM users WHERE email = $1`, email)
	})

	hash, err := hashPassword("pw")
	if err != nil {
		t.Fatal(err)
	}
	if err := createUser(ctx, pool, email, hash); err != nil {
		t.Fatalf("createUser: %v", err)
	}

	id, gotHash, err := getCredentials(ctx, pool, email)
	if err != nil {
		t.Fatalf("getCredentials: %v", err)
	}
	if len(id) != 36 {
		t.Errorf("id = %q, want a UUID", id)
	}
	if gotHash != hash {
		t.Errorf("stored hash = %q, want %q", gotHash, hash)
	}

	if err := createUser(ctx, pool, email, hash); err == nil {
		t.Error("duplicate email was accepted")
	}

	// The login handler relies on this to tell "unknown email" from a
	// database failure.
	if _, _, err := getCredentials(ctx, pool, "nobody@example.invalid"); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("unknown email: err = %v, want pgx.ErrNoRows", err)
	}
}

package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const bcryptCost int = 12

func hashPassword(pw string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if errors.Is(err, bcrypt.ErrPasswordTooLong) {
		return "", fmt.Errorf("password terlalu panjang: %w", err)
	}
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

// checkPassword reports whether pw matches a stored bcrypt hash.
func checkPassword(hash, pw string) bool {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)); err != nil {
		return false
	}
	return true
}

// createUser stores a new user. hash must come from hashPassword.
// A duplicate email fails on the UNIQUE constraint.
func createUser(ctx context.Context, pool *pgxpool.Pool, email, hash string) error {
	stmt := `
		INSERT INTO users (
			email, password_hash
		) VALUES (
			$1, $2
		)
	`
	_, err := pool.Exec(ctx, stmt, email, hash)
	return err
}

// getCredentials returns what the login form needs to verify a user: the id
// (which becomes the "sub" claim) and the password hash.
//
// It is the only function that reads password_hash, so the hash never rides
// along in a general-purpose user struct that might get logged or serialized.
//
// When no user has that email the error is pgx.ErrNoRows; check it with
// errors.Is to tell "unknown email" apart from a database failure.
func getCredentials(ctx context.Context, pool *pgxpool.Pool, email string) (id, hash string, err error) {
	stmt := `
		SELECT id, password_hash
		FROM users
		WHERE email = $1
	`
	err = pool.QueryRow(ctx, stmt, email).Scan(&id, &hash)
	return id, hash, err
}

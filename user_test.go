package main

// Tests for user.go. Run with: go test -run TestPassword -v .
//
// Each case below is one thing that would be a security bug if it broke.
// Write them as one test function with several checks, the same style as
// TestHealthz in main_test.go.
//
// look at: https://go.dev/doc/tutorial/add-a-test
// look at: https://pkg.go.dev/testing

// TODO: func TestPassword(t *testing.T) { ... }
//
// CASE 1: the correct password is accepted.
//   hash, err := hashPassword("correct horse")
//   -> err must be nil (use t.Fatal here: the rest is meaningless without a hash)
//   -> checkPassword(hash, "correct horse") must be true
//
// CASE 2: a wrong password is rejected.
//   -> checkPassword(hash, "wrong") must be false
//
// CASE 3: the hash is not the plain password.
//   -> hash != "correct horse"
//   -> strings.HasPrefix(hash, "$2a$12$")   // "$2a$" = bcrypt, "12" = the cost
//   This one catches someone accidentally storing the password itself.
//
// CASE 4: a password longer than 72 bytes is refused, not truncated.
//   long := strings.Repeat("a", 73)
//   _, err := hashPassword(long)
//   -> errors.Is(err, bcrypt.ErrPasswordTooLong) must be true
//
// CASE 5: garbage in the hash column does not crash or match.
//   -> checkPassword("not-a-hash", "anything") must be false
//
// Tip: t.Errorf reports a failure and keeps going; t.Fatalf stops the test.
// Use Fatal only when later checks cannot run without this one.

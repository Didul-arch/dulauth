package main

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"

	"github.com/go-jose/go-jose/v4"
	"github.com/zitadel/oidc/v3/pkg/op"
)

// Milestone M3: OIDC provider (PRD §12).
// Done when these two return valid JSON:
//
//	curl -s localhost:3031/.well-known/openid-configuration
//	curl -s localhost:3031/keys
//
// The library (zitadel/oidc, package "op") owns the protocol. It asks us for
// everything it does not know through ONE interface, op.Storage: who are the
// clients, who are the users, where is the signing key, where do in-flight
// logins live. This file is our implementation of that interface.
//
// The interface has about 20 methods. M3 only needs FOUR of them to really
// work. The rest are stubs that M4 and M5 fill in.
//
// Before you start:
//
//	go get github.com/zitadel/oidc/v3
//	openssl genrsa -out signing.pem 2048      (already gitignored by *.pem)
//
// look at: https://github.com/zitadel/oidc/tree/main/example/server/storage  (storage.go, oidc.go, client.go)
// look at: https://pkg.go.dev/github.com/zitadel/oidc/v3/pkg/op#Storage
// look at: https://openid.net/specs/openid-connect-discovery-1_0.html#ProviderMetadata

// STEP 1: the storage struct.
//
// It holds what the methods need. For M3 that is only the RSA private key
// and its key ID ("kid"). Later it grows: the pgx pool (M4), the client list
// (M4), and the in-memory auth requests guarded by a sync.Mutex (M4).
//
// TODO: type storage struct { key *rsa.PrivateKey; keyID string }

type storage struct {
	key   *rsa.PrivateKey
	keyID string
}

// STEP 2: newStorage(signingKeyPath string) (*storage, error)
//
// Load the private key from the PEM file.
//
//   - os.ReadFile the path.
//   - pem.Decode gives you a *pem.Block; nil means the file is not PEM.
//   - OpenSSL 3 writes "BEGIN PRIVATE KEY", which is PKCS#8, so parse
//     block.Bytes with x509.ParsePKCS8PrivateKey. It returns `any`; type-assert
//     to *rsa.PrivateKey and return an error if it is some other key type.
//   - The key ID must be stable for the same key and change when the key
//     changes, so derive it from the public key instead of inventing a name:
//     jose.JSONWebKey{Key: &key.PublicKey}.Thumbprint(crypto.SHA256), then
//     base64.RawURLEncoding.EncodeToString. This is RFC 7638.
//   - Wrap every error with what you were doing: fmt.Errorf("read signing key: %w", err).
//
// look at: https://pkg.go.dev/encoding/pem#Decode
// look at: https://pkg.go.dev/crypto/x509#ParsePKCS8PrivateKey
// look at: https://pkg.go.dev/github.com/go-jose/go-jose/v4#JSONWebKey.Thumbprint
// look at: https://go.dev/tour/methods/15  (type assertions)
//
// TODO: write newStorage

func newStorage(signingKeyPath string) (*storage, error) {
	key, err := os.ReadFile(signingKeyPath)
	if err != nil {
		return nil, fmt.Errorf("Error reading key, got %w", err)
	}
	block, _ := pem.Decode(key)
	if block == nil {
		return nil, errors.New("signing key is not PEM")
	}

	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("Error parsing private key: %w", err)
	}
	rsaKey, ok := parsed.(*rsa.PrivateKey)

	if !ok {
		return nil, errors.New("Error parsing rsa key")
	}

	jwk := jose.JSONWebKey{Key: &rsaKey.PublicKey}
	thumb, err := jwk.Thumbprint(crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("compute key id got: %w", err)
	}

	return &storage{
		key:   rsaKey,
		keyID: base64.RawURLEncoding.EncodeToString(thumb),
	}, nil
}

// STEP 3: two tiny types that describe the key to the library.
//
// The library does not take *rsa.PrivateKey directly. It wants values that
// satisfy its own small interfaces:
//
//	op.SigningKey  (private half, used to SIGN tokens)
//	    SignatureAlgorithm() jose.SignatureAlgorithm   -> jose.RS256  (PRD FR-5)
//	    Key() any                                      -> the *rsa.PrivateKey
//	    ID() string                                    -> the kid
//
//	op.Key         (public half, PUBLISHED at /keys)
//	    ID() string                                    -> the same kid
//	    Algorithm() jose.SignatureAlgorithm            -> jose.RS256
//	    Use() string                                   -> "sig"
//	    Key() any                                      -> the *rsa.PublicKey
//
// Returning the private key from the public type would publish it at /keys.
// Keeping them as two separate types makes that mistake hard to write.
//
// jose is github.com/go-jose/go-jose/v4.
//
// TODO: type signingKey struct{...} with its 3 methods
type signingKey struct {
	key *rsa.PrivateKey
	id  string
}

func (k *signingKey) SignatureAlgorithm() jose.SignatureAlgorithm {
	return jose.RS256
}

func (k *signingKey) Key() any {
	return k.key
}

func (k *signingKey) ID() string {
	return k.id
}

// type Key interface {
// 	ID() string
// 	Algorithm() jose.SignatureAlgorithm
// 	Use() string
// 	Key() any
// }

type publicKey struct {
	key *rsa.PublicKey
	id  string
}

func (k *publicKey) Algorithm() jose.SignatureAlgorithm {
	return jose.RS256
}

func (k *publicKey) Key() any {
	return k.key
}

func (k *publicKey) ID() string {
	return k.id
}

func (k *publicKey) Use() string {
	return "sig"
}

// TODO: type publicKey struct{...} with its 4 methods
// STEP 4: hand the key to the library.
//
// GOAL: the library can ask storage for the key whenever it needs it.
//
// WHY: STEP 2 loaded the key and STEP 3 wrapped it, but both live inside
// storage, where the library cannot reach them. The library only talks to
// storage through methods with exact names and signatures (they are part of
// op.Storage). Give it those methods and it can sign tokens and serve /keys.
//
// Every method is a single `return` of something you already have. No new
// logic. ctx is unused for now; the signature must match op.Storage exactly.
//
//  1. SigningKey: "which key do I SIGN tokens with?"
//     Return your privateKey{...} built from s.key and s.keyID.
//
//     func (s *storage) SigningKey(ctx context.Context) (op.SigningKey, error)
//
//  2. KeySet: "which keys may I PUBLISH at /keys?"
//     Clients (porto) download these to check the signature on a token.
//     Return a slice holding one &publicKey{...}. The public half is
//     &s.key.PublicKey. Never put s.key itself here: that would publish
//     the private key. Later, key rotation (PRD §13.6) means returning the
//     old and the new key in this slice at the same time.
//
//     func (s *storage) KeySet(ctx context.Context) ([]op.Key, error)
//
//  3. SignatureAlgorithms: "which algorithm do you sign with?"
//     Return []jose.SignatureAlgorithm{jose.RS256}. Discovery shows it as
//     "id_token_signing_alg_values_supported", so clients know what to expect.
//
//     func (s *storage) SignatureAlgorithms(ctx context.Context) ([]jose.SignatureAlgorithm, error)
//
//  4. Health: "are you OK?"
//     Return nil for now. In M4, when storage holds the pgx pool, ping it here.
//
//     func (s *storage) Health(ctx context.Context) error
//
// New imports: "context" and "github.com/zitadel/oidc/v3/pkg/op".
//
// TODO: write the four methods
func (s *storage) SigningKey(ctx context.Context) (op.SigningKey, error) {
	return &signingKey{key: s.key, id: s.keyID}, nil
}

// STEP 5: stub every other method so the type satisfies op.Storage.
//
// Add this line. It costs nothing at runtime and makes the compiler tell you
// when a method is missing or has the wrong signature:
//
//	var _ op.Storage = (*storage)(nil)
//
// Writing 16 stubs by hand is busywork. Generate them, then delete the four
// you already wrote in STEP 4:
//
//	go run github.com/josharian/impl@latest 's *storage' github.com/zitadel/oidc/v3/pkg/op.Storage
//
// The generated bodies are panic("not implemented"). Replace each with a
// returned error where the signature allows it, so a stray request gets an
// error response instead of a panic in the log:
//
//	return nil, errors.New("not implemented")
//
// Do not try to implement them yet. Each one gets its turn:
//
//	M4 (login): CreateAuthRequest, AuthRequestByID, GetClientByClientID
//	M5 (token): AuthRequestByCode, SaveAuthCode, DeleteAuthRequest,
//	            CreateAccessToken, SetUserinfoFromScopes/Token,
//	            GetPrivateClaimsFromScopes, TerminateSession
//	never in the MVP (refresh tokens, JWT profile, revocation): the rest
//
// look at: https://pkg.go.dev/github.com/zitadel/oidc/v3/pkg/op#AuthStorage
// look at: https://pkg.go.dev/github.com/zitadel/oidc/v3/pkg/op#OPStorage
//
// TODO: the assertion line and the stubs

// STEP 6: build the provider. This part goes in main.go, inside run().
//
//  1. Read two new env vars and fail if either is empty (same check you
//     wrote for DATABASE_URL):
//     ISSUER            e.g. http://localhost:3031   (no trailing slash)
//     SIGNING_KEY_PATH  e.g. ./signing.pem
//
//  2. store, err := newStorage(signingKeyPath)
//
//  3. Build the config:
//
//     config := &op.Config{
//     CryptoKey:       <a [32]byte secret, see below>,
//     CodeMethodS256:  true,                        // PRD FR-2
//     SupportedScopes: []string{"openid", "email"}, // PRD FR-7
//     }
//
//     CryptoKey encrypts the authorization code, so it must be secret and
//     must stay the same across restarts. PRD §10 has no env var for it.
//     Simplest option that needs no new secret: derive it from the signing
//     key file, sha256.Sum256(append([]byte("dul-auth code key:"), pemBytes...)).
//     Ask Dodi about the trade-off before you pick.
//
//  4. Create it:
//
//     provider, err := op.NewProvider(config, store, op.StaticIssuer(issuer), opts...)
//
//     For an http:// issuer (local dev only) opts must contain
//     op.WithAllowInsecure(), otherwise NewProvider refuses to start. Add it
//     only when strings.HasPrefix(issuer, "http://"), so production cannot
//     run without TLS by accident.
//
//  5. *op.Provider is an http.Handler. Give it to newHandler and mount it
//     as the fallback route, after /healthz:
//
//     mux.Handle("/", provider)
//
//     newHandler therefore needs a parameter now, and TestHealthz must pass
//     something for it.
//
// look at: https://pkg.go.dev/github.com/zitadel/oidc/v3/pkg/op#NewProvider
// look at: https://github.com/zitadel/oidc/blob/main/example/server/exampleop/op.go
//
// STEP 7: check it.
//
//	env (cat .env) go run .
//	curl -s localhost:3031/.well-known/openid-configuration | jq
//	curl -s localhost:3031/keys | jq
//
// In the first, "issuer" must equal ISSUER exactly, and the endpoints must
// match PRD §7.1 (/authorize, /oauth/token, /keys, /userinfo, /end_session).
// In the second you should see one key with "kty":"RSA", "use":"sig",
// "alg":"RS256", a "kid", and "n" and "e". There must be NO "d", "p" or "q":
// those are the private parts.

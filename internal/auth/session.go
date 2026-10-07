package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

var (
	ErrAlreadyInitialized = errors.New("server already initialized")
	ErrInvalidBootstrap   = errors.New("invalid bootstrap secret")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidSession     = errors.New("invalid session")
)

type Service struct {
	repo          *catalog.Repository
	bootstrapHash [32]byte
	mu            sync.Mutex
}

func New(repo *catalog.Repository) (*Service, string, error) {
	secret, err := randomToken(32)
	if err != nil {
		return nil, "", err
	}
	return NewWithBootstrap(repo, secret), secret, nil
}

func NewWithBootstrap(repo *catalog.Repository, secret string) *Service {
	return &Service{repo: repo, bootstrapHash: sha256.Sum256([]byte(secret))}
}

func (s *Service) Initialized(ctx context.Context) bool {
	n, err := s.repo.CountAdmins(ctx)
	return err == nil && n > 0
}

func (s *Service) Bootstrap(ctx context.Context, secret, username, password string) (catalog.Principal, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Initialized(ctx) {
		return catalog.Principal{}, "", ErrAlreadyInitialized
	}
	got := sha256.Sum256([]byte(secret))
	if subtle.ConstantTimeCompare(got[:], s.bootstrapHash[:]) != 1 {
		return catalog.Principal{}, "", ErrInvalidBootstrap
	}
	if username == "" || password == "" {
		return catalog.Principal{}, "", ErrInvalidCredentials
	}
	hash, err := HashPassword(password)
	if err != nil {
		return catalog.Principal{}, "", err
	}
	id, err := randomToken(18)
	if err != nil {
		return catalog.Principal{}, "", err
	}
	principal := catalog.Principal{ID: "usr_" + id, Username: username, PasswordHash: hash, Admin: true}
	if err := s.repo.CreatePrincipal(ctx, principal); err != nil {
		return catalog.Principal{}, "", err
	}
	token, err := s.issueSession(ctx, principal.ID)
	if err != nil {
		return catalog.Principal{}, "", err
	}
	return principal, token, nil
}

func (s *Service) Login(ctx context.Context, username, password string) (catalog.Principal, string, error) {
	principal, err := s.repo.GetPrincipalByUsername(ctx, username)
	if err != nil || !VerifyPassword(password, principal.PasswordHash) {
		return catalog.Principal{}, "", ErrInvalidCredentials
	}
	token, err := s.issueSession(ctx, principal.ID)
	if err != nil {
		return catalog.Principal{}, "", err
	}
	return principal, token, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (catalog.Principal, error) {
	if token == "" {
		return catalog.Principal{}, ErrInvalidSession
	}
	principal, err := s.repo.GetPrincipalByTokenHash(ctx, tokenHash(token))
	if err != nil {
		return catalog.Principal{}, ErrInvalidSession
	}
	return principal, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return ErrInvalidSession
	}
	if err := s.repo.RevokeAuthSession(ctx, tokenHash(token)); err != nil {
		return ErrInvalidSession
	}
	return nil
}

func (s *Service) issueSession(ctx context.Context, principalID string) (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	id, err := randomToken(18)
	if err != nil {
		return "", err
	}
	if err := s.repo.CreateAuthSession(ctx, "ses_"+id, principalID, tokenHash(token)); err != nil {
		return "", err
	}
	return token, nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

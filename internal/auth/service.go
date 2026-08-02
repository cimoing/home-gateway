package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"home-gateway/internal/model"

	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/bcrypt"
)

const (
	defaultSessionDuration = 24 * time.Hour
	maxLoginAttempts       = 10
	loginAttemptWindow     = 5 * time.Minute
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrRateLimited        = errors.New("too many login attempts")
	ErrUnauthenticated    = errors.New("not authenticated")
)

// LoginMetadata contains audit information about a login attempt.
type LoginMetadata struct {
	IPAddress string
	UserAgent string
}

type sessionEntry struct {
	userID    int64
	expiresAt time.Time
	lastSeen  time.Time
}

type failedAttempt struct {
	at time.Time
}

// Service authenticates users and manages in-memory sessions.
type Service struct {
	db              *sqlx.DB
	sessionDuration time.Duration
	now             func() time.Time
	dummyHash       []byte

	mu           sync.Mutex
	sessions     map[string]sessionEntry
	failedLogins map[string][]failedAttempt
}

// NewService creates an authentication service.
func NewService(db *sqlx.DB) *Service {
	dummyHash, _ := bcrypt.GenerateFromPassword([]byte("invalid-password"), bcrypt.DefaultCost)
	return &Service{
		db:              db,
		sessionDuration: defaultSessionDuration,
		now:             time.Now,
		dummyHash:       dummyHash,
		sessions:        make(map[string]sessionEntry),
		failedLogins:    make(map[string][]failedAttempt),
	}
}

// Login verifies credentials, records the attempt, and creates a session.
func (s *Service) Login(
	ctx context.Context,
	username string,
	password []byte,
	metadata LoginMetadata,
) (string, time.Time, error) {
	metadata.IPAddress = truncate(metadata.IPAddress, 45)
	metadata.UserAgent = truncate(metadata.UserAgent, 1024)
	now := s.now().UTC()

	if s.isRateLimited(metadata.IPAddress, now) {
		return "", time.Time{}, ErrRateLimited
	}

	user, err := s.findUser(ctx, username)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, err
	}

	hash := s.dummyHash
	if err == nil {
		hash = []byte(user.PasswordHash)
	}
	passwordMatches := bcrypt.CompareHashAndPassword(hash, password) == nil
	if err != nil || !passwordMatches || !user.Enabled {
		reason := "invalid_credentials"
		if err == nil && !user.Enabled {
			reason = "user_disabled"
		}
		var userID *int64
		if err == nil {
			userID = &user.ID
		}
		s.recordFailedAttempt(metadata.IPAddress, now)
		if logErr := s.recordLogin(ctx, userID, username, false, &reason, metadata, now); logErr != nil {
			return "", time.Time{}, logErr
		}
		return "", time.Time{}, ErrInvalidCredentials
	}

	token, tokenHash, err := newSessionToken()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create session token: %w", err)
	}
	expiresAt := now.Add(s.sessionDuration)

	s.mu.Lock()
	s.sessions[tokenHash] = sessionEntry{
		userID: user.ID, expiresAt: expiresAt, lastSeen: now,
	}
	s.mu.Unlock()

	updateUser := s.db.Rebind(`
		UPDATE users SET last_login_at = ?, updated_at = ? WHERE id = ?
	`)
	if _, err := s.db.ExecContext(ctx, updateUser, now, now, user.ID); err != nil {
		s.mu.Lock()
		delete(s.sessions, tokenHash)
		s.mu.Unlock()
		return "", time.Time{}, fmt.Errorf("update last login: %w", err)
	}
	if err := s.recordLogin(ctx, &user.ID, username, true, nil, metadata, now); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// UserForSession returns the active user represented by token.
func (s *Service) UserForSession(ctx context.Context, token string) (model.User, error) {
	if token == "" {
		return model.User{}, ErrUnauthenticated
	}

	now := s.now().UTC()
	tokenHash := hashToken(token)

	s.mu.Lock()
	entry, ok := s.sessions[tokenHash]
	if !ok || !entry.expiresAt.After(now) {
		delete(s.sessions, tokenHash)
		s.mu.Unlock()
		return model.User{}, ErrUnauthenticated
	}
	entry.lastSeen = now
	s.sessions[tokenHash] = entry
	userID := entry.userID
	s.mu.Unlock()

	query := s.db.Rebind(`
		SELECT id, username, password_hash, display_name, email,
		       enabled, created_at, updated_at, last_login_at
		FROM users WHERE id = ? AND enabled = ?
	`)
	var user model.User
	if err := s.db.GetContext(ctx, &user, query, userID, true); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.mu.Lock()
			delete(s.sessions, tokenHash)
			s.mu.Unlock()
			return model.User{}, ErrUnauthenticated
		}
		return model.User{}, fmt.Errorf("find session user: %w", err)
	}
	return user, nil
}

// Logout revokes a session token.
func (s *Service) Logout(_ context.Context, token string) error {
	if token == "" {
		return nil
	}
	s.mu.Lock()
	delete(s.sessions, hashToken(token))
	s.mu.Unlock()
	return nil
}

func (s *Service) findUser(ctx context.Context, username string) (model.User, error) {
	query := s.db.Rebind(`
		SELECT id, username, password_hash, display_name, email, enabled,
		       created_at, updated_at, last_login_at
		FROM users WHERE username = ?
	`)
	var user model.User
	if err := s.db.GetContext(ctx, &user, query, username); err != nil {
		return model.User{}, err
	}
	return user, nil
}

func (s *Service) isRateLimited(ipAddress string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	since := now.Add(-loginAttemptWindow)
	attempts := s.failedLogins[ipAddress]
	kept := attempts[:0]
	for _, attempt := range attempts {
		if attempt.at.After(since) {
			kept = append(kept, attempt)
		}
	}
	s.failedLogins[ipAddress] = kept
	return len(kept) >= maxLoginAttempts
}

func (s *Service) recordFailedAttempt(ipAddress string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failedLogins[ipAddress] = append(s.failedLogins[ipAddress], failedAttempt{at: at})
}

func (s *Service) recordLogin(
	ctx context.Context,
	userID *int64,
	username string,
	success bool,
	failureReason *string,
	metadata LoginMetadata,
	createdAt time.Time,
) error {
	query := s.db.Rebind(`
		INSERT INTO user_login_logs
		    (user_id, username, success, failure_reason, ip_address, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if _, err := s.db.ExecContext(
		ctx,
		query,
		userID,
		username,
		success,
		failureReason,
		metadata.IPAddress,
		metadata.UserAgent,
		createdAt,
	); err != nil {
		return fmt.Errorf("record login attempt: %w", err)
	}
	return nil
}

func newSessionToken() (token string, tokenHash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func truncate(value string, maxRunes int) string {
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes])
}

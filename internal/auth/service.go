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

// Service authenticates users and manages revocable sessions.
type Service struct {
	db              *sqlx.DB
	sessionDuration time.Duration
	now             func() time.Time
	dummyHash       []byte
}

// NewService creates an authentication service.
func NewService(db *sqlx.DB) *Service {
	dummyHash, _ := bcrypt.GenerateFromPassword([]byte("invalid-password"), bcrypt.DefaultCost)
	return &Service{
		db:              db,
		sessionDuration: defaultSessionDuration,
		now:             time.Now,
		dummyHash:       dummyHash,
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

	limited, err := s.isRateLimited(ctx, metadata.IPAddress, now.Add(-loginAttemptWindow))
	if err != nil {
		return "", time.Time{}, err
	}
	if limited {
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

	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("begin login transaction: %w", err)
	}
	defer tx.Rollback()

	insertSession := tx.Rebind(`
		INSERT INTO user_sessions
		    (token_hash, user_id, expires_at, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?)
	`)
	if _, err := tx.ExecContext(ctx, insertSession, tokenHash, user.ID, expiresAt, now, now); err != nil {
		return "", time.Time{}, fmt.Errorf("create user session: %w", err)
	}

	updateUser := tx.Rebind(`
		UPDATE users SET last_login_at = ?, updated_at = ? WHERE id = ?
	`)
	if _, err := tx.ExecContext(ctx, updateUser, now, now, user.ID); err != nil {
		return "", time.Time{}, fmt.Errorf("update last login: %w", err)
	}
	if err := recordLogin(ctx, tx, &user.ID, username, true, nil, metadata, now); err != nil {
		return "", time.Time{}, err
	}
	if err := tx.Commit(); err != nil {
		return "", time.Time{}, fmt.Errorf("commit login transaction: %w", err)
	}
	return token, expiresAt, nil
}

// UserForSession returns the active user represented by token.
func (s *Service) UserForSession(ctx context.Context, token string) (model.User, error) {
	if token == "" {
		return model.User{}, ErrUnauthenticated
	}

	now := s.now().UTC()
	query := s.db.Rebind(`
		SELECT u.id, u.username, u.password_hash, u.display_name, u.email,
		       u.enabled, u.created_at, u.updated_at, u.last_login_at
		FROM user_sessions AS s
		JOIN users AS u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ? AND u.enabled = ?
	`)
	var user model.User
	if err := s.db.GetContext(ctx, &user, query, hashToken(token), now, true); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.User{}, ErrUnauthenticated
		}
		return model.User{}, fmt.Errorf("find user session: %w", err)
	}

	update := s.db.Rebind(`UPDATE user_sessions SET last_seen_at = ? WHERE token_hash = ?`)
	if _, err := s.db.ExecContext(ctx, update, now, hashToken(token)); err != nil {
		return model.User{}, fmt.Errorf("update user session: %w", err)
	}
	return user, nil
}

// Logout revokes a session token.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	query := s.db.Rebind(`DELETE FROM user_sessions WHERE token_hash = ?`)
	if _, err := s.db.ExecContext(ctx, query, hashToken(token)); err != nil {
		return fmt.Errorf("delete user session: %w", err)
	}
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

func (s *Service) isRateLimited(ctx context.Context, ipAddress string, since time.Time) (bool, error) {
	query := s.db.Rebind(`
		SELECT COUNT(*) FROM user_login_logs
		WHERE ip_address = ? AND success = ? AND created_at >= ?
	`)
	var attempts int
	if err := s.db.GetContext(ctx, &attempts, query, ipAddress, false, since); err != nil {
		return false, fmt.Errorf("check login attempts: %w", err)
	}
	return attempts >= maxLoginAttempts, nil
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
	return recordLogin(ctx, s.db, userID, username, success, failureReason, metadata, createdAt)
}

type rebindingExecer interface {
	Rebind(string) string
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func recordLogin(
	ctx context.Context,
	execer rebindingExecer,
	userID *int64,
	username string,
	success bool,
	failureReason *string,
	metadata LoginMetadata,
	createdAt time.Time,
) error {
	query := execer.Rebind(`
		INSERT INTO user_login_logs
		    (user_id, username, success, failure_reason, ip_address, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`)
	if _, err := execer.ExecContext(
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

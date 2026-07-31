package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrAlreadyExists = errors.New("user already exists")
	ErrNotFound      = errors.New("user not found")
)

// Service manages user credentials.
type Service struct {
	db *sqlx.DB
}

// NewService creates a user service backed by db.
func NewService(db *sqlx.DB) *Service {
	return &Service{db: db}
}

// Create creates an enabled user with a bcrypt password hash.
func (s *Service) Create(ctx context.Context, username string, password []byte) error {
	if err := ValidateUsername(username); err != nil {
		return err
	}
	if err := ValidatePassword(password); err != nil {
		return err
	}

	var exists bool
	queryExists := s.db.Rebind(`SELECT EXISTS(SELECT 1 FROM users WHERE username = ?)`)
	if err := s.db.GetContext(ctx, &exists, queryExists, username); err != nil {
		return fmt.Errorf("check user: %w", err)
	}
	if exists {
		return ErrAlreadyExists
	}

	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	query := s.db.Rebind(`
		INSERT INTO users (username, password_hash, display_name, enabled)
		VALUES (?, ?, ?, ?)
	`)
	if _, err := s.db.ExecContext(ctx, query, username, string(hash), username, true); err != nil {
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

// UpdatePassword replaces a user's bcrypt password hash.
func (s *Service) UpdatePassword(ctx context.Context, username string, password []byte) error {
	if err := ValidateUsername(username); err != nil {
		return err
	}
	if err := ValidatePassword(password); err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	query := s.db.Rebind(`
		UPDATE users
		SET password_hash = ?, updated_at = CURRENT_TIMESTAMP
		WHERE username = ?
	`)
	result, err := s.db.ExecContext(ctx, query, string(hash), username)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read update result: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// ValidateUsername applies the database-compatible username constraints.
func ValidateUsername(username string) error {
	if username != strings.TrimSpace(username) {
		return errors.New("username must not start or end with whitespace")
	}
	length := utf8.RuneCountInString(username)
	if length == 0 || length > 64 {
		return errors.New("username must contain 1 to 64 characters")
	}
	for _, character := range username {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return errors.New("username must not contain whitespace or control characters")
		}
	}
	return nil
}

// ValidatePassword applies bcrypt's input limit and a minimum length.
func ValidatePassword(password []byte) error {
	if !utf8.Valid(password) {
		return errors.New("password must be valid UTF-8")
	}
	if len(password) < 8 {
		return errors.New("password must contain at least 8 bytes")
	}
	if len(password) > 72 {
		return errors.New("password must not exceed 72 bytes")
	}
	return nil
}

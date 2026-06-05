package totp

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// ErrNotFound is returned when no TOTP record exists for the account.
var ErrNotFound = errors.New("totp: no record found")

// ErrAlreadyConfirmed is returned when attempting to set up TOTP that is already confirmed.
var ErrAlreadyConfirmed = errors.New("totp: already confirmed")

// ErrInvalidCode is returned when the submitted code is incorrect.
var ErrInvalidCode = errors.New("totp: invalid code")

// ErrNotConfirmed is returned when TOTP has not been confirmed yet.
var ErrNotConfirmed = errors.New("totp: not confirmed")

const backupCodeCount = 8
const backupCodeLength = 8

// Service handles TOTP operations.
type Service struct {
	db        *sqlx.DB
	secretKey []byte // 32-byte AES-256 key derived from app secret
}

// New creates a new TOTP Service. appSecret is the application's SECRET_KEY config value.
func New(db *sqlx.DB, appSecret string) *Service {
	// Derive a 32-byte key via SHA-256
	h := sha256.Sum256([]byte(appSecret))
	return &Service{db: db, secretKey: h[:]}
}

// SetupResult is returned by Setup.
type SetupResult struct {
	OTPAuthURI string `json:"otp_auth_uri"`
	Secret     string `json:"secret"`
}

// ConfirmResult is returned by Confirm.
type ConfirmResult struct {
	BackupCodes []string `json:"backup_codes"`
}

// Setup generates a new TOTP secret for the account, stores it unconfirmed, and returns the
// otpauth:// URI plus the raw secret for display.
func (s *Service) Setup(ctx context.Context, accountID int64) (*SetupResult, error) {
	// Check for an existing confirmed entry
	var confirmedAt *time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT confirmed_at FROM account_totp WHERE account_id = $1`, accountID,
	).Scan(&confirmedAt)
	if err == nil && confirmedAt != nil {
		return nil, ErrAlreadyConfirmed
	}

	accountName := strconv.FormatInt(accountID, 10)
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "SentinelSnap",
		AccountName: accountName,
	})
	if err != nil {
		return nil, fmt.Errorf("totp generate: %w", err)
	}

	rawSecret := key.Secret()
	encryptedSecret, err := s.encrypt(rawSecret)
	if err != nil {
		return nil, fmt.Errorf("totp encrypt secret: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO account_totp (account_id, secret, confirmed_at)
		 VALUES ($1, $2, NULL)
		 ON CONFLICT (account_id) DO UPDATE SET secret = EXCLUDED.secret, confirmed_at = NULL`,
		accountID, encryptedSecret,
	)
	if err != nil {
		return nil, fmt.Errorf("totp store secret: %w", err)
	}

	return &SetupResult{
		OTPAuthURI: key.URL(),
		Secret:     rawSecret,
	}, nil
}

// Confirm verifies the submitted code against the stored secret. On success it marks the record
// confirmed and generates 8 single-use backup codes.
func (s *Service) Confirm(ctx context.Context, accountID int64, code string) (*ConfirmResult, error) {
	encryptedSecret, confirmedAt, err := s.loadSecret(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if confirmedAt != nil {
		return nil, ErrAlreadyConfirmed
	}

	rawSecret, err := s.decrypt(encryptedSecret)
	if err != nil {
		return nil, fmt.Errorf("totp decrypt secret: %w", err)
	}

	valid, err := totp.ValidateCustom(code, rawSecret, time.Now().UTC(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil || !valid {
		return nil, ErrInvalidCode
	}

	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx,
		`UPDATE account_totp SET confirmed_at = $1 WHERE account_id = $2`,
		now, accountID,
	)
	if err != nil {
		return nil, fmt.Errorf("totp confirm: %w", err)
	}

	// Delete any old backup codes for this account
	_, _ = s.db.ExecContext(ctx, `DELETE FROM account_totp_backup_codes WHERE account_id = $1`, accountID)

	plainCodes, err := s.generateBackupCodes(ctx, accountID)
	if err != nil {
		return nil, err
	}

	return &ConfirmResult{BackupCodes: plainCodes}, nil
}

// Verify checks a TOTP code (±1 window) or a single-use backup code.
// Returns nil on success.
func (s *Service) Verify(ctx context.Context, accountID int64, code string) error {
	encryptedSecret, confirmedAt, err := s.loadSecret(ctx, accountID)
	if err != nil {
		return err
	}
	if confirmedAt == nil {
		return ErrNotConfirmed
	}

	rawSecret, err := s.decrypt(encryptedSecret)
	if err != nil {
		return fmt.Errorf("totp decrypt secret: %w", err)
	}

	valid, err := totp.ValidateCustom(code, rawSecret, time.Now().UTC(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err == nil && valid {
		return nil
	}

	// Try backup codes
	if err := s.consumeBackupCode(ctx, accountID, code); err == nil {
		return nil
	}

	return ErrInvalidCode
}

// Disable removes TOTP for the account after verifying the submitted code.
func (s *Service) Disable(ctx context.Context, accountID int64, code string) error {
	if err := s.Verify(ctx, accountID, code); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `DELETE FROM account_totp WHERE account_id = $1`, accountID)
	if err != nil {
		return fmt.Errorf("totp disable: %w", err)
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM account_totp_backup_codes WHERE account_id = $1`, accountID)
	return nil
}

// --- internal helpers ---

func (s *Service) loadSecret(ctx context.Context, accountID int64) (encryptedSecret string, confirmedAt *time.Time, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT secret, confirmed_at FROM account_totp WHERE account_id = $1`, accountID,
	).Scan(&encryptedSecret, &confirmedAt)
	if err != nil {
		if isNoRows(err) {
			return "", nil, ErrNotFound
		}
		return "", nil, fmt.Errorf("totp load secret: %w", err)
	}
	return encryptedSecret, confirmedAt, nil
}

func isNoRows(err error) bool {
	return err != nil && err.Error() == "sql: no rows in result set"
}

// encrypt encrypts plaintext using AES-GCM and returns a base64-encoded ciphertext.
func (s *Service) encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.secretKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt reverses encrypt.
func (s *Service) decrypt(encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.secretKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("totp: ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// randAlphanumeric generates a random alphanumeric string of the given length.
func randAlphanumeric(length int) (string, error) {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	buf := make([]byte, length)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", err
	}
	for i, b := range buf {
		buf[i] = chars[int(b)%len(chars)]
	}
	return string(buf), nil
}

func (s *Service) generateBackupCodes(ctx context.Context, accountID int64) ([]string, error) {
	plain := make([]string, backupCodeCount)
	for i := range plain {
		code, err := randAlphanumeric(backupCodeLength)
		if err != nil {
			return nil, fmt.Errorf("totp generate backup code: %w", err)
		}
		plain[i] = code

		hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("totp hash backup code: %w", err)
		}

		_, err = s.db.ExecContext(ctx,
			`INSERT INTO account_totp_backup_codes (account_id, code_hash) VALUES ($1, $2)`,
			accountID, string(hash),
		)
		if err != nil {
			return nil, fmt.Errorf("totp store backup code: %w", err)
		}
	}
	return plain, nil
}

func (s *Service) consumeBackupCode(ctx context.Context, accountID int64, code string) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, code_hash FROM account_totp_backup_codes WHERE account_id = $1 AND used_at IS NULL`,
		accountID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		var hash string
		if err := rows.Scan(&id, &hash); err != nil {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(code)) == nil {
			// Mark as used
			_, err = s.db.ExecContext(ctx,
				`UPDATE account_totp_backup_codes SET used_at = $1 WHERE id = $2`,
				time.Now().UTC(), id,
			)
			return err
		}
	}
	return errors.New("no matching backup code")
}

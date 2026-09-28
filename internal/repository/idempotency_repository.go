package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

var (
	ErrIdempotencyConflict = errors.New("idempotency conflict: key already used with different payload")
	ErrRequestInFlight     = errors.New("request in flight: concurrent request with the same key in progress")
)

type IdempotencyRecord struct {
	Key          string
	RequestPath  string
	RequestMethod string
	RequestHash  string
	StatusCode   int
	ResponseBody string
	State        string
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

type IdempotencyRepository struct {
	db *sql.DB
}

func NewIdempotencyRepository(db *sql.DB) *IdempotencyRepository {
	return &IdempotencyRepository{db: db}
}

func HashPayload(method, path string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte(":"))
	h.Write([]byte(path))
	h.Write([]byte(":"))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func (r *IdempotencyRepository) LockOrGet(ctx context.Context, key, method, path string, body []byte) (*IdempotencyRecord, error) {
	expectedHash := HashPayload(method, path, body)

	insertQuery := `
		INSERT INTO idempotency_keys (key, request_path, request_method, request_hash, state, created_at, expires_at)
		VALUES ($1, $2, $3, $4, 'STARTED', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP + INTERVAL '24 hours')
		ON CONFLICT (key) DO NOTHING;
	`
	res, err := r.db.ExecContext(ctx, insertQuery, key, path, method, expectedHash)
	if err != nil {
		return nil, fmt.Errorf("failed to insert idempotency record: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to check rows affected: %w", err)
	}

	if rows == 1 {
		return nil, nil
	}

	var rec IdempotencyRecord
	var statusCode sql.NullInt64
	var responseBody sql.NullString

	selectQuery := `
		SELECT key, request_path, request_method, request_hash, status_code, response_body, state, created_at, expires_at
		FROM idempotency_keys
		WHERE key = $1;
	`
	err = r.db.QueryRowContext(ctx, selectQuery, key).Scan(
		&rec.Key,
		&rec.RequestPath,
		&rec.RequestMethod,
		&rec.RequestHash,
		&statusCode,
		&responseBody,
		&rec.State,
		&rec.CreatedAt,
		&rec.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch existing idempotency record: %w", err)
	}

	if statusCode.Valid {
		rec.StatusCode = int(statusCode.Int64)
	}
	if responseBody.Valid {
		rec.ResponseBody = responseBody.String
	}

	if time.Now().After(rec.ExpiresAt) {
		_, _ = r.db.ExecContext(ctx, "DELETE FROM idempotency_keys WHERE key = $1", key)
		resRetry, err := r.db.ExecContext(ctx, insertQuery, key, path, method, expectedHash)
		if err == nil {
			if rRows, _ := resRetry.RowsAffected(); rRows == 1 {
				return nil, nil
			}
		}
	}

	if rec.RequestHash != expectedHash {
		return nil, ErrIdempotencyConflict
	}

	if rec.State == "STARTED" {
		if time.Since(rec.CreatedAt) > 2*time.Minute {
			_, _ = r.db.ExecContext(ctx, "DELETE FROM idempotency_keys WHERE key = $1", key)
			resRetry, err := r.db.ExecContext(ctx, insertQuery, key, path, method, expectedHash)
			if err == nil {
				if rRows, _ := resRetry.RowsAffected(); rRows == 1 {
					return nil, nil
				}
			}
		}
		return nil, ErrRequestInFlight
	}

	return &rec, nil
}

func (r *IdempotencyRepository) Complete(ctx context.Context, key string, statusCode int, responseBody string) error {
	query := `
		UPDATE idempotency_keys
		SET state = 'COMPLETED',
		    status_code = $2,
		    response_body = $3
		WHERE key = $1;
	`
	_, err := r.db.ExecContext(ctx, query, key, statusCode, responseBody)
	return err
}

func (r *IdempotencyRepository) Delete(ctx context.Context, key string) error {
	query := `DELETE FROM idempotency_keys WHERE key = $1;`
	_, err := r.db.ExecContext(ctx, query, key)
	return err
}

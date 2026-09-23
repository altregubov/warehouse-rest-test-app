package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type UserRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	GetByUsername(ctx context.Context, username string) (*domain.User, error)
	Create(ctx context.Context, user *domain.User) error
	UpdateBalance(ctx context.Context, id uuid.UUID, newBalance float64) (*domain.User, error)
	UpdateFilters(ctx context.Context, id uuid.UUID, categories, manufacturers []string) (*domain.User, error)
}

type sqlUserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) UserRepository {
	return &sqlUserRepository{db: db}
}

func (r *sqlUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	query := `
		SELECT id, username, password_hash, role, balance, allowed_categories, allowed_manufacturers, created_at, updated_at
		FROM users
		WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, id)

	var u domain.User
	var allowedCategories, allowedManufacturers pq.StringArray

	err := row.Scan(
		&u.ID,
		&u.Username,
		&u.PasswordHash,
		&u.Role,
		&u.Balance,
		&allowedCategories,
		&allowedManufacturers,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan user: %w", err)
	}

	u.AllowedCategories = []string(allowedCategories)
	if u.AllowedCategories == nil {
		u.AllowedCategories = []string{}
	}
	u.AllowedManufacturers = []string(allowedManufacturers)
	if u.AllowedManufacturers == nil {
		u.AllowedManufacturers = []string{}
	}

	return &u, nil
}

func (r *sqlUserRepository) GetByUsername(ctx context.Context, username string) (*domain.User, error) {
	query := `
		SELECT id, username, password_hash, role, balance, allowed_categories, allowed_manufacturers, created_at, updated_at
		FROM users
		WHERE username = $1
	`
	row := r.db.QueryRowContext(ctx, query, username)

	var u domain.User
	var allowedCategories, allowedManufacturers pq.StringArray

	err := row.Scan(
		&u.ID,
		&u.Username,
		&u.PasswordHash,
		&u.Role,
		&u.Balance,
		&allowedCategories,
		&allowedManufacturers,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan user: %w", err)
	}

	u.AllowedCategories = []string(allowedCategories)
	if u.AllowedCategories == nil {
		u.AllowedCategories = []string{}
	}
	u.AllowedManufacturers = []string(allowedManufacturers)
	if u.AllowedManufacturers == nil {
		u.AllowedManufacturers = []string{}
	}

	return &u, nil
}

func (r *sqlUserRepository) Create(ctx context.Context, u *domain.User) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	if u.AllowedCategories == nil {
		u.AllowedCategories = []string{}
	}
	if u.AllowedManufacturers == nil {
		u.AllowedManufacturers = []string{}
	}

	query := `
		INSERT INTO users (id, username, password_hash, role, balance, allowed_categories, allowed_manufacturers)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at, updated_at
	`
	err := r.db.QueryRowContext(
		ctx,
		query,
		u.ID,
		u.Username,
		u.PasswordHash,
		u.Role,
		u.Balance,
		pq.Array(u.AllowedCategories),
		pq.Array(u.AllowedManufacturers),
	).Scan(&u.CreatedAt, &u.UpdatedAt)

	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" { // unique_violation
			return domain.ErrUsernameTaken
		}
		return fmt.Errorf("failed to insert user: %w", err)
	}

	return nil
}

func (r *sqlUserRepository) UpdateBalance(ctx context.Context, id uuid.UUID, newBalance float64) (*domain.User, error) {
	query := `
		UPDATE users
		SET balance = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
		RETURNING id, username, password_hash, role, balance, allowed_categories, allowed_manufacturers, created_at, updated_at
	`
	row := r.db.QueryRowContext(ctx, query, newBalance, id)

	var u domain.User
	var allowedCategories, allowedManufacturers pq.StringArray

	err := row.Scan(
		&u.ID,
		&u.Username,
		&u.PasswordHash,
		&u.Role,
		&u.Balance,
		&allowedCategories,
		&allowedManufacturers,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to update user balance: %w", err)
	}

	u.AllowedCategories = []string(allowedCategories)
	if u.AllowedCategories == nil {
		u.AllowedCategories = []string{}
	}
	u.AllowedManufacturers = []string(allowedManufacturers)
	if u.AllowedManufacturers == nil {
		u.AllowedManufacturers = []string{}
	}

	return &u, nil
}

func (r *sqlUserRepository) UpdateFilters(ctx context.Context, id uuid.UUID, categories, manufacturers []string) (*domain.User, error) {
	if categories == nil {
		categories = []string{}
	}
	if manufacturers == nil {
		manufacturers = []string{}
	}

	query := `
		UPDATE users
		SET allowed_categories = $1, allowed_manufacturers = $2, updated_at = CURRENT_TIMESTAMP
		WHERE id = $3
		RETURNING id, username, password_hash, role, balance, allowed_categories, allowed_manufacturers, created_at, updated_at
	`
	row := r.db.QueryRowContext(ctx, query, pq.Array(categories), pq.Array(manufacturers), id)

	var u domain.User
	var allowedCategories, allowedManufacturers pq.StringArray

	err := row.Scan(
		&u.ID,
		&u.Username,
		&u.PasswordHash,
		&u.Role,
		&u.Balance,
		&allowedCategories,
		&allowedManufacturers,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to update user filters: %w", err)
	}

	u.AllowedCategories = []string(allowedCategories)
	if u.AllowedCategories == nil {
		u.AllowedCategories = []string{}
	}
	u.AllowedManufacturers = []string(allowedManufacturers)
	if u.AllowedManufacturers == nil {
		u.AllowedManufacturers = []string{}
	}

	return &u, nil
}

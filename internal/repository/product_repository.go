package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type ProductRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Product, error)
	List(ctx context.Context, filterCategory string, allowedCategories, allowedManufacturers []string) ([]domain.Product, error)
	ListPaginated(ctx context.Context, params domain.ProductFilterParams, allowedCategories, allowedManufacturers []string) ([]domain.Product, int, error)
	Create(ctx context.Context, product *domain.Product) error
	UpdateStock(ctx context.Context, id uuid.UUID, newStock int) (*domain.Product, error)
}

type sqlProductRepository struct {
	db *sql.DB
}

func NewProductRepository(db *sql.DB) ProductRepository {
	return &sqlProductRepository{db: db}
}

func (r *sqlProductRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Product, error) {
	query := `
		SELECT id, category, manufacturer, model, price, stock_quantity, created_at, updated_at
		FROM products
		WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, id)

	var p domain.Product
	err := row.Scan(
		&p.ID,
		&p.Category,
		&p.Manufacturer,
		&p.Model,
		&p.Price,
		&p.StockQuantity,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan product: %w", err)
	}

	return &p, nil
}

func (r *sqlProductRepository) List(ctx context.Context, filterCategory string, allowedCategories, allowedManufacturers []string) ([]domain.Product, error) {
	filterCategory = strings.TrimSpace(filterCategory)

	// If user is restricted to specific categories, and requested a category not in their whitelist, return empty list
	if len(allowedCategories) > 0 && filterCategory != "" {
		allowed := false
		for _, cat := range allowedCategories {
			if strings.EqualFold(cat, filterCategory) {
				allowed = true
				break
			}
		}
		if !allowed {
			return []domain.Product{}, nil
		}
	}

	query := `
		SELECT id, category, manufacturer, model, price, stock_quantity, created_at, updated_at
		FROM products
		WHERE 1=1
	`
	args := []any{}
	argIdx := 1

	if filterCategory != "" {
		query += fmt.Sprintf(" AND LOWER(TRIM(category)) = LOWER(TRIM($%d))", argIdx)
		args = append(args, filterCategory)
		argIdx++
	} else if len(allowedCategories) > 0 {
		normCats := make([]string, len(allowedCategories))
		for i, c := range allowedCategories {
			normCats[i] = strings.ToLower(strings.TrimSpace(c))
		}
		query += fmt.Sprintf(" AND LOWER(TRIM(category)) = ANY($%d)", argIdx)
		args = append(args, pq.Array(normCats))
		argIdx++
	}

	if len(allowedManufacturers) > 0 {
		normMfgs := make([]string, len(allowedManufacturers))
		for i, m := range allowedManufacturers {
			normMfgs[i] = strings.ToLower(strings.TrimSpace(m))
		}
		query += fmt.Sprintf(" AND LOWER(TRIM(manufacturer)) = ANY($%d)", argIdx)
		args = append(args, pq.Array(normMfgs))
		argIdx++
	}

	query += " ORDER BY category ASC, manufacturer ASC, model ASC"

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query products: %w", err)
	}
	defer rows.Close()

	products := []domain.Product{}
	for rows.Next() {
		var p domain.Product
		if err := rows.Scan(
			&p.ID,
			&p.Category,
			&p.Manufacturer,
			&p.Model,
			&p.Price,
			&p.StockQuantity,
			&p.CreatedAt,
			&p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan product row: %w", err)
		}
		products = append(products, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading products: %w", err)
	}

	return products, nil
}

func (r *sqlProductRepository) ListPaginated(ctx context.Context, params domain.ProductFilterParams, allowedCategories, allowedManufacturers []string) ([]domain.Product, int, error) {
	filterCategory := strings.TrimSpace(params.Category)
	if len(allowedCategories) > 0 && filterCategory != "" {
		allowed := false
		for _, cat := range allowedCategories {
			if strings.EqualFold(cat, filterCategory) {
				allowed = true
				break
			}
		}
		if !allowed {
			return []domain.Product{}, 0, nil
		}
	}

	filterManufacturer := strings.TrimSpace(params.Manufacturer)
	if len(allowedManufacturers) > 0 && filterManufacturer != "" {
		allowed := false
		for _, m := range allowedManufacturers {
			if strings.EqualFold(m, filterManufacturer) {
				allowed = true
				break
			}
		}
		if !allowed {
			return []domain.Product{}, 0, nil
		}
	}

	whereClause := " WHERE 1=1"
	args := []any{}
	argIdx := 1

	if filterCategory != "" {
		whereClause += fmt.Sprintf(" AND LOWER(TRIM(category)) = LOWER(TRIM($%d))", argIdx)
		args = append(args, filterCategory)
		argIdx++
	} else if len(allowedCategories) > 0 {
		normCats := make([]string, len(allowedCategories))
		for i, c := range allowedCategories {
			normCats[i] = strings.ToLower(strings.TrimSpace(c))
		}
		whereClause += fmt.Sprintf(" AND LOWER(TRIM(category)) = ANY($%d)", argIdx)
		args = append(args, pq.Array(normCats))
		argIdx++
	}

	if filterManufacturer != "" {
		whereClause += fmt.Sprintf(" AND LOWER(TRIM(manufacturer)) = LOWER(TRIM($%d))", argIdx)
		args = append(args, filterManufacturer)
		argIdx++
	} else if len(allowedManufacturers) > 0 {
		normMfgs := make([]string, len(allowedManufacturers))
		for i, m := range allowedManufacturers {
			normMfgs[i] = strings.ToLower(strings.TrimSpace(m))
		}
		whereClause += fmt.Sprintf(" AND LOWER(TRIM(manufacturer)) = ANY($%d)", argIdx)
		args = append(args, pq.Array(normMfgs))
		argIdx++
	}

	countQuery := "SELECT COUNT(*) FROM products" + whereClause
	var totalCount int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, 0, fmt.Errorf("failed to count products: %w", err)
	}
	if totalCount == 0 {
		return []domain.Product{}, 0, nil
	}

	var orderClause string
	switch strings.ToLower(strings.TrimSpace(params.SortBy)) {
	case "price":
		if strings.EqualFold(params.Order, "desc") {
			orderClause = "price DESC, id ASC"
		} else {
			orderClause = "price ASC, id ASC"
		}
	case "model":
		if strings.EqualFold(params.Order, "desc") {
			orderClause = "model DESC, id ASC"
		} else {
			orderClause = "model ASC, id ASC"
		}
	case "created_at":
		if strings.EqualFold(params.Order, "desc") {
			orderClause = "created_at DESC, id ASC"
		} else {
			orderClause = "created_at ASC, id ASC"
		}
	default:
		if strings.EqualFold(params.Order, "desc") {
			orderClause = "created_at DESC, id ASC"
		} else {
			orderClause = "created_at ASC, id ASC"
		}
	}

	page := params.Page
	if page < 1 {
		page = 1
	}
	pageSize := params.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	selectQuery := fmt.Sprintf(`
		SELECT id, category, manufacturer, model, price, stock_quantity, created_at, updated_at
		FROM products
		%s
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, whereClause, orderClause, argIdx, argIdx+1)

	queryArgs := append(args, pageSize, offset)

	rows, err := r.db.QueryContext(ctx, selectQuery, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query products: %w", err)
	}
	defer rows.Close()

	products := []domain.Product{}
	for rows.Next() {
		var p domain.Product
		if err := rows.Scan(
			&p.ID,
			&p.Category,
			&p.Manufacturer,
			&p.Model,
			&p.Price,
			&p.StockQuantity,
			&p.CreatedAt,
			&p.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("failed to scan product row: %w", err)
		}
		products = append(products, p)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error reading products: %w", err)
	}

	return products, totalCount, nil
}

func (r *sqlProductRepository) Create(ctx context.Context, p *domain.Product) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}

	query := `
		INSERT INTO products (id, category, manufacturer, model, price, stock_quantity)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at
	`
	err := r.db.QueryRowContext(
		ctx,
		query,
		p.ID,
		p.Category,
		p.Manufacturer,
		p.Model,
		p.Price,
		p.StockQuantity,
	).Scan(&p.CreatedAt, &p.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to insert product: %w", err)
	}

	return nil
}

func (r *sqlProductRepository) UpdateStock(ctx context.Context, id uuid.UUID, newStock int) (*domain.Product, error) {
	query := `
		UPDATE products
		SET stock_quantity = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2
		RETURNING id, category, manufacturer, model, price, stock_quantity, created_at, updated_at
	`
	row := r.db.QueryRowContext(ctx, query, newStock, id)

	var p domain.Product
	err := row.Scan(
		&p.ID,
		&p.Category,
		&p.Manufacturer,
		&p.Model,
		&p.Price,
		&p.StockQuantity,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to update product stock: %w", err)
	}

	return &p, nil
}

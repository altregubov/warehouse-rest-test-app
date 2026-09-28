package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type OrderRepository interface {
	CreateOrderTx(ctx context.Context, userID, productID uuid.UUID, quantity int) (*domain.OrderResponse, error)
	ListByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.OrderResponse, error)
	GetByID(ctx context.Context, orderID uuid.UUID) (*domain.OrderResponse, error)
	ListAll(ctx context.Context) ([]*domain.OrderResponse, error)
	UpdateStatus(ctx context.Context, orderID uuid.UUID, status string) (*domain.OrderResponse, error)
}

type sqlOrderRepository struct {
	db *sql.DB
}

func NewOrderRepository(db *sql.DB) OrderRepository {
	return &sqlOrderRepository{db: db}
}

func (r *sqlOrderRepository) CreateOrderTx(ctx context.Context, userID, productID uuid.UUID, quantity int) (*domain.OrderResponse, error) {
	if quantity <= 0 {
		return nil, fmt.Errorf("%w: quantity must be greater than zero", domain.ErrInvalidInput)
	}

	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Lock and fetch user
	var user domain.User
	var allowedCategories, allowedManufacturers pq.StringArray
	userQuery := `
		SELECT id, username, balance, allowed_categories, allowed_manufacturers
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
		FOR UPDATE
	`
	err = tx.QueryRowContext(ctx, userQuery, userID).Scan(
		&user.ID,
		&user.Username,
		&user.Balance,
		&allowedCategories,
		&allowedManufacturers,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: user not found", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("failed to lock user: %w", err)
	}
	user.AllowedCategories = []string(allowedCategories)
	user.AllowedManufacturers = []string(allowedManufacturers)

	// 2. Lock and fetch product
	var product domain.Product
	prodQuery := `
		SELECT id, category, manufacturer, model, price, stock_quantity
		FROM products
		WHERE id = $1
		FOR UPDATE
	`
	err = tx.QueryRowContext(ctx, prodQuery, productID).Scan(
		&product.ID,
		&product.Category,
		&product.Manufacturer,
		&product.Model,
		&product.Price,
		&product.StockQuantity,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: product not found", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("failed to lock product: %w", err)
	}

	// 3. Check user filter permissions
	if len(user.AllowedCategories) > 0 {
		catAllowed := false
		for _, cat := range user.AllowedCategories {
			if strings.EqualFold(cat, product.Category) {
				catAllowed = true
				break
			}
		}
		if !catAllowed {
			return nil, domain.ErrProductDisallowed
		}
	}

	if len(user.AllowedManufacturers) > 0 {
		mfgAllowed := false
		for _, mfg := range user.AllowedManufacturers {
			if strings.EqualFold(mfg, product.Manufacturer) {
				mfgAllowed = true
				break
			}
		}
		if !mfgAllowed {
			return nil, domain.ErrProductDisallowed
		}
	}

	// 4. Verify stock
	if product.StockQuantity < quantity {
		return nil, domain.ErrInsufficientStock
	}

	// 5. Calculate total cost and verify balance using integer cents to eliminate floating-point drift
	unitPriceCents := domain.DollarsToCents(product.Price)
	totalCostCents := unitPriceCents * int64(quantity)
	userBalanceCents := domain.DollarsToCents(user.Balance)

	if userBalanceCents < totalCostCents {
		return nil, domain.ErrInsufficientBalance
	}

	// 6. Decrement stock
	newStock := product.StockQuantity - quantity
	_, err = tx.ExecContext(ctx, "UPDATE products SET stock_quantity = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2", newStock, product.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to update product stock: %w", err)
	}

	// 7. Deduct balance in exact cents
	newBalanceCents := userBalanceCents - totalCostCents
	newBalance := domain.CentsToDollars(newBalanceCents)
	totalCost := domain.CentsToDollars(totalCostCents)
	unitPrice := domain.CentsToDollars(unitPriceCents)

	_, err = tx.ExecContext(ctx, "UPDATE users SET balance = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2", newBalance, user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to update user balance: %w", err)
	}

	// 8. Insert order
	orderID := uuid.New()
	createdAt := time.Now().UTC()
	orderQuery := `
		INSERT INTO orders (id, user_id, product_id, product_model, unit_price, quantity, total_price, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'CREATED', $8)
	`
	_, err = tx.ExecContext(ctx, orderQuery, orderID, user.ID, product.ID, product.Model, unitPrice, quantity, totalCost, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to insert order: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit order transaction: %w", err)
	}

	return &domain.OrderResponse{
		OrderID:          orderID,
		UserID:           user.ID,
		ProductID:        product.ID,
		ProductModel:     product.Model,
		Quantity:         quantity,
		UnitPrice:        unitPrice,
		TotalPrice:       totalCost,
		Status:           "CREATED",
		RemainingBalance: newBalance,
		CreatedAt:        createdAt,
	}, nil
}

func (r *sqlOrderRepository) ListByUserID(ctx context.Context, userID uuid.UUID) ([]*domain.OrderResponse, error) {
	query := `
		SELECT o.id, o.user_id, o.product_id, o.product_model, o.quantity, o.unit_price, o.total_price, o.status, o.created_at
		FROM orders o
		WHERE o.user_id = $1
		ORDER BY o.created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query user orders: %w", err)
	}
	defer rows.Close()

	orders := make([]*domain.OrderResponse, 0)
	for rows.Next() {
		var o domain.OrderResponse
		if err := rows.Scan(
			&o.OrderID,
			&o.UserID,
			&o.ProductID,
			&o.ProductModel,
			&o.Quantity,
			&o.UnitPrice,
			&o.TotalPrice,
			&o.Status,
			&o.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan order: %w", err)
		}
		orders = append(orders, &o)
	}
	return orders, nil
}

func (r *sqlOrderRepository) GetByID(ctx context.Context, orderID uuid.UUID) (*domain.OrderResponse, error) {
	query := `
		SELECT o.id, o.user_id, o.product_id, o.product_model, o.quantity, o.unit_price, o.total_price, o.status, o.created_at
		FROM orders o
		WHERE o.id = $1
	`
	var o domain.OrderResponse
	err := r.db.QueryRowContext(ctx, query, orderID).Scan(
		&o.OrderID,
		&o.UserID,
		&o.ProductID,
		&o.ProductModel,
		&o.Quantity,
		&o.UnitPrice,
		&o.TotalPrice,
		&o.Status,
		&o.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get order: %w", err)
	}
	return &o, nil
}

func (r *sqlOrderRepository) ListAll(ctx context.Context) ([]*domain.OrderResponse, error) {
	query := `
		SELECT o.id, o.user_id, o.product_id, o.product_model, o.quantity, o.unit_price, o.total_price, o.status, o.created_at
		FROM orders o
		ORDER BY o.created_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query all orders: %w", err)
	}
	defer rows.Close()

	orders := make([]*domain.OrderResponse, 0)
	for rows.Next() {
		var o domain.OrderResponse
		if err := rows.Scan(
			&o.OrderID,
			&o.UserID,
			&o.ProductID,
			&o.ProductModel,
			&o.Quantity,
			&o.UnitPrice,
			&o.TotalPrice,
			&o.Status,
			&o.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan order: %w", err)
		}
		orders = append(orders, &o)
	}
	return orders, nil
}

func (r *sqlOrderRepository) UpdateStatus(ctx context.Context, orderID uuid.UUID, status string) (*domain.OrderResponse, error) {
	query := `
		UPDATE orders
		SET status = $1
		WHERE id = $2
		RETURNING id, user_id, product_id, product_model, unit_price, quantity, total_price, status, created_at
	`
	var o domain.OrderResponse
	err := r.db.QueryRowContext(ctx, query, status, orderID).Scan(
		&o.OrderID,
		&o.UserID,
		&o.ProductID,
		&o.ProductModel,
		&o.UnitPrice,
		&o.Quantity,
		&o.TotalPrice,
		&o.Status,
		&o.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("failed to update order status: %w", err)
	}

	return &o, nil
}

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
		WHERE id = $1
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

	// 5. Calculate total cost and verify balance
	totalCost := product.Price * float64(quantity)
	if user.Balance < totalCost {
		return nil, domain.ErrInsufficientBalance
	}

	// 6. Decrement stock
	newStock := product.StockQuantity - quantity
	_, err = tx.ExecContext(ctx, "UPDATE products SET stock_quantity = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2", newStock, product.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to update product stock: %w", err)
	}

	// 7. Deduct balance
	newBalance := user.Balance - totalCost
	_, err = tx.ExecContext(ctx, "UPDATE users SET balance = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2", newBalance, user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to update user balance: %w", err)
	}

	// 8. Insert order
	orderID := uuid.New()
	createdAt := time.Now().UTC()
	orderQuery := `
		INSERT INTO orders (id, user_id, product_id, quantity, total_price, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err = tx.ExecContext(ctx, orderQuery, orderID, user.ID, product.ID, quantity, totalCost, createdAt)
	if err != nil {
		return nil, fmt.Errorf("failed to insert order: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit order transaction: %w", err)
	}

	return &domain.OrderResponse{
		OrderID:          orderID,
		ProductID:        product.ID,
		ProductModel:     product.Model,
		Quantity:         quantity,
		UnitPrice:        product.Price,
		TotalPrice:       totalCost,
		RemainingBalance: newBalance,
		CreatedAt:        createdAt,
	}, nil
}

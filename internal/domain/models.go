package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Roles
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

var (
	ErrNotFound            = errors.New("resource not found")
	ErrInvalidCredentials  = errors.New("invalid username or password")
	ErrForbiddenRole       = errors.New("access forbidden for this role")
	ErrUsernameTaken       = errors.New("username already exists")
	ErrInsufficientStock   = errors.New("insufficient stock for product")
	ErrInsufficientBalance = errors.New("insufficient balance for transaction")
	ErrProductDisallowed   = errors.New("product is not allowed by user filter permissions")
	ErrInvalidInput        = errors.New("invalid input data")
)

// Domain Models

type User struct {
	ID                   uuid.UUID `json:"id"`
	Username             string    `json:"username"`
	PasswordHash         string    `json:"-"`
	Role                 string    `json:"role"`
	Balance              float64   `json:"balance"`
	AllowedCategories   []string  `json:"allowed_categories"`
	AllowedManufacturers []string  `json:"allowed_manufacturers"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type Product struct {
	ID            uuid.UUID `json:"id"`
	Category      string    `json:"category"`
	Manufacturer  string    `json:"manufacturer"`
	Model         string    `json:"model"`
	Price         float64   `json:"price"`
	StockQuantity int       `json:"stock_quantity"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Order struct {
	ID         uuid.UUID `json:"id"`
	UserID     uuid.UUID `json:"user_id"`
	ProductID  uuid.UUID `json:"product_id"`
	Quantity   int       `json:"quantity"`
	TotalPrice float64   `json:"total_price"`
	CreatedAt  time.Time `json:"created_at"`
}

// DTOs

type LoginRequest struct {
	Username string `json:"username" example:"admin"`
	Password string `json:"password" example:"admin123"`
}

type UserSummary struct {
	ID                   uuid.UUID `json:"id"`
	Username             string    `json:"username"`
	Role                 string    `json:"role"`
	Balance              float64   `json:"balance"`
	AllowedCategories   []string  `json:"allowed_categories"`
	AllowedManufacturers []string  `json:"allowed_manufacturers"`
}

type LoginResponse struct {
	Token string      `json:"token"`
	User  UserSummary `json:"user"`
}

type CreateUserRequest struct {
	Username             string   `json:"username" example:"john_doe"`
	Password             string   `json:"password" example:"secret123"`
	Role                 string   `json:"role" example:"user"`
	Balance              float64  `json:"balance" example:"1000.00"`
	AllowedCategories   []string `json:"allowed_categories" example:"[\"laptop\"]"`
	AllowedManufacturers []string `json:"allowed_manufacturers" example:"[\"Dell\"]"`
}

type UpdateBalanceRequest struct {
	Amount float64 `json:"amount" example:"500.00"`
}

type UpdateFiltersRequest struct {
	AllowedCategories   []string `json:"allowed_categories" example:"[\"laptop\"]"`
	AllowedManufacturers []string `json:"allowed_manufacturers" example:"[\"Apple\", \"Dell\"]"`
}

type CreateProductRequest struct {
	Category      string  `json:"category" example:"laptop"`
	Manufacturer  string  `json:"manufacturer" example:"Apple"`
	Model         string  `json:"model" example:"MacBook Air M3"`
	Price         float64 `json:"price" example:"1099.00"`
	StockQuantity int     `json:"stock_quantity" example:"10"`
}

type UpdateStockRequest struct {
	StockQuantity int `json:"stock_quantity" example:"25"`
}

type CreateOrderRequest struct {
	ProductID uuid.UUID `json:"product_id" example:"c0000000-0000-0000-0000-000000000001"`
	Quantity  int       `json:"quantity" example:"1"`
}

type OrderResponse struct {
	OrderID          uuid.UUID `json:"order_id"`
	ProductID        uuid.UUID `json:"product_id"`
	ProductModel     string    `json:"product_model"`
	Quantity         int       `json:"quantity"`
	UnitPrice        float64   `json:"unit_price"`
	TotalPrice       float64   `json:"total_price"`
	RemainingBalance float64   `json:"remaining_balance"`
	CreatedAt        time.Time `json:"created_at"`
}

// Envelope structures for Swagger documentation and JSON API

type SuccessEnvelope struct {
	Success bool `json:"success" example:"true"`
	Data    any  `json:"data"`
}

type ErrorDetails struct {
	Code    string `json:"code" example:"BAD_REQUEST"`
	Message string `json:"message" example:"Detailed error description"`
	Details any    `json:"details,omitempty"`
}

type ErrorEnvelope struct {
	Success bool         `json:"success" example:"false"`
	Error   ErrorDetails `json:"error"`
}

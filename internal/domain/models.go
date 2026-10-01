package domain

import (
	"errors"
	"math"
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
	ID                   uuid.UUID  `json:"id" binding:"required" format:"uuid"`
	Username             string     `json:"username" binding:"required" minLength:"1"`
	PasswordHash         string     `json:"-"`
	Role                 string     `json:"role" binding:"required" enums:"admin,user"`
	Balance              float64    `json:"balance" binding:"required" format:"double" minimum:"0"`
	AllowedCategories   []string   `json:"allowed_categories"`
	AllowedManufacturers []string   `json:"allowed_manufacturers"`
	CreatedAt            time.Time  `json:"created_at" binding:"required" format:"date-time"`
	UpdatedAt            time.Time  `json:"updated_at" binding:"required" format:"date-time"`
	DeletedAt            *time.Time `json:"deleted_at,omitempty" format:"date-time"`
}

type Product struct {
	ID            uuid.UUID `json:"id" binding:"required" format:"uuid" example:"c0000000-0000-0000-0000-000000000001"`
	Category      string    `json:"category" binding:"required" example:"laptop"`
	Manufacturer  string    `json:"manufacturer" binding:"required" example:"Apple"`
	Model         string    `json:"model" binding:"required" example:"MacBook Pro 16 M3"`
	Price         float64   `json:"price" binding:"required" format:"double" example:"2499.00" minimum:"0"`
	StockQuantity int       `json:"stock_quantity" binding:"required" example:"15" minimum:"0"`
	CreatedAt     time.Time `json:"created_at" binding:"required" format:"date-time"`
	UpdatedAt     time.Time `json:"updated_at" binding:"required" format:"date-time"`
}

type Order struct {
	ID           uuid.UUID `json:"id" binding:"required" format:"uuid"`
	UserID       uuid.UUID `json:"user_id" binding:"required" format:"uuid"`
	ProductID    uuid.UUID `json:"product_id" binding:"required" format:"uuid"`
	ProductModel string    `json:"product_model" binding:"required"`
	UnitPrice    float64   `json:"unit_price" binding:"required" format:"double" minimum:"0"`
	Quantity     int       `json:"quantity" binding:"required" minimum:"1"`
	TotalPrice   float64   `json:"total_price" binding:"required" format:"double" minimum:"0"`
	Status       string    `json:"status" binding:"required" enums:"PROCESSED,CANCELLED,FAILED"`
	CreatedAt    time.Time `json:"created_at" binding:"required" format:"date-time"`
}

// DTOs

type LoginRequest struct {
	Username string `json:"username" binding:"required" example:"admin" minLength:"1"`
	Password string `json:"password" binding:"required" example:"admin123"`
}

// DollarsToCents converts dollar amount to integer cents, rounding to avoid floating-point drift
func DollarsToCents(dollars float64) int64 {
	return int64(math.Round(dollars * 100))
}

// CentsToDollars converts integer cents back to float64 dollars
func CentsToDollars(cents int64) float64 {
	return float64(cents) / 100.0
}

type UserSummary struct {
	ID                   uuid.UUID `json:"id" binding:"required" format:"uuid" example:"b0000000-0000-0000-0000-000000000002"`
	Username             string    `json:"username" binding:"required" example:"userA" minLength:"1"`
	Role                 string    `json:"role" binding:"required" example:"user" enums:"admin,user"`
	Balance              float64   `json:"balance" binding:"required" example:"1000.00" format:"double" minimum:"0"`
	AllowedCategories   []string  `json:"allowed_categories" binding:"required" example:"laptop"`
	AllowedManufacturers []string  `json:"allowed_manufacturers" binding:"required" example:"Dell"`
}

type LoginResponse struct {
	Token string      `json:"token" binding:"required"`
	User  UserSummary `json:"user" binding:"required"`
}

type CreateUserRequest struct {
	Username             string   `json:"username" binding:"required" example:"john_doe" minLength:"1"`
	Password             string   `json:"password" binding:"required" example:"secret123"`
	Role                 string   `json:"role" binding:"required" example:"user" enums:"admin,user"`
	Balance              float64  `json:"balance" binding:"required" example:"1000.00" format:"double" minimum:"0"`
	AllowedCategories   []string `json:"allowed_categories" example:"laptop"`
	AllowedManufacturers []string `json:"allowed_manufacturers" example:"Dell"`
}

type TopUpBalanceRequest struct {
	IncrementAmount float64 `json:"increment_amount" binding:"required" example:"500.00" format:"double" minimum:"0.01"`
}

type UpdateFiltersRequest struct {
	AllowedCategories   []string `json:"allowed_categories" binding:"required" example:"laptop"`
	AllowedManufacturers []string `json:"allowed_manufacturers" binding:"required" example:"Apple,Dell"`
}

type CreateProductRequest struct {
	Category      string  `json:"category" binding:"required" example:"laptop"`
	Manufacturer  string  `json:"manufacturer" binding:"required" example:"Apple"`
	Model         string  `json:"model" binding:"required" example:"MacBook Air M3"`
	Price         float64 `json:"price" binding:"required" example:"1099.00" format:"double" minimum:"0"`
	StockQuantity int     `json:"stock_quantity" binding:"required" example:"10" minimum:"0"`
}

type UpdateStockRequest struct {
	StockQuantity int `json:"stock_quantity" binding:"required,gte=0" example:"25" minimum:"0" maximum:"2147483647"`
}

type CreateOrderRequest struct {
	ProductID uuid.UUID `json:"product_id" binding:"required" example:"c0000000-0000-0000-0000-000000000001" format:"uuid"`
	Quantity  int       `json:"quantity" binding:"required" example:"1" minimum:"1"`
}

type OrderResponse struct {
	OrderID          uuid.UUID `json:"order_id" binding:"required" format:"uuid" example:"d0000000-0000-0000-0000-000000000001"`
	UserID           uuid.UUID `json:"user_id,omitempty" format:"uuid" example:"b0000000-0000-0000-0000-000000000002"`
	ProductID        uuid.UUID `json:"product_id" binding:"required" format:"uuid" example:"c0000000-0000-0000-0000-000000000001"`
	ProductModel     string    `json:"product_model" binding:"required" example:"MacBook Pro 16 M3"`
	Quantity         int       `json:"quantity" binding:"required" example:"1" minimum:"1"`
	UnitPrice        float64   `json:"unit_price" binding:"required" example:"2499.00" format:"double" minimum:"0"`
	TotalPrice       float64   `json:"total_price" binding:"required" example:"4998.00" format:"double" minimum:"0"`
	Status           string    `json:"status" binding:"required" example:"PROCESSED" enums:"PROCESSED,CANCELLED,FAILED"`
	RemainingBalance float64   `json:"remaining_balance,omitempty" example:"500.00" format:"double" minimum:"0"`
	CreatedAt        time.Time `json:"created_at" binding:"required" format:"date-time"`
}

type UpdateOrderStatusRequest struct {
	Status string `json:"status" binding:"required" example:"CANCELLED" enums:"CANCELLED"`
}

type PaginationMetadata struct {
	TotalCount int `json:"total_count" binding:"required" example:"45" minimum:"0"`
	Page       int `json:"page" binding:"required" example:"1" minimum:"1"`
	PageSize   int `json:"page_size" binding:"required" example:"20" minimum:"1" maximum:"100"`
	TotalPages int `json:"total_pages" binding:"required" example:"3" minimum:"0"`
}

type ProductFilterParams struct {
	Category     string
	Manufacturer string
	SortBy       string
	Order        string
	Page         int
	PageSize     int
}

// Envelope structures for Swagger documentation and JSON API

type SuccessEnvelope struct {
	Success    bool                `json:"success" binding:"required" example:"true"`
	Data       any                 `json:"data" binding:"required"`
	Pagination *PaginationMetadata `json:"pagination,omitempty"`
	TotalCount *int                `json:"total_count,omitempty" example:"45"`
	Page       *int                `json:"page,omitempty" example:"1"`
	PageSize   *int                `json:"page_size,omitempty" example:"20"`
	TotalPages *int                `json:"total_pages,omitempty" example:"3"`
	RequestID  string              `json:"requestId,omitempty" example:"c56a4180-65aa-42ec-a945-5fd21dec0538"`
}

type FieldViolation struct {
	Field string `json:"field" binding:"required" example:"quantity"`
	Issue string `json:"issue" binding:"required" example:"quantity must be greater than 0"`
}

type ErrorDetails struct {
	Code    string           `json:"code" binding:"required" example:"BAD_REQUEST"`
	Message string           `json:"message" binding:"required" example:"Detailed error description"`
	Details []FieldViolation `json:"details,omitempty"`
}

type ErrorEnvelope struct {
	Success   bool         `json:"success" binding:"required" example:"false"`
	Error     ErrorDetails `json:"error" binding:"required"`
	RequestID string       `json:"requestId,omitempty" example:"c56a4180-65aa-42ec-a945-5fd21dec0538"`
}

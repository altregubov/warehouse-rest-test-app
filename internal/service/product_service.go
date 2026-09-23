package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
	"github.com/altregubov/warehouse-rest-test-app/internal/repository"
	"github.com/google/uuid"
)

type ProductService interface {
	ListUserProducts(ctx context.Context, userID uuid.UUID, category string) ([]domain.Product, error)
	CreateProduct(ctx context.Context, req *domain.CreateProductRequest) (*domain.Product, error)
	UpdateStock(ctx context.Context, productID uuid.UUID, stockQuantity int) (*domain.Product, error)
}

type productService struct {
	prodRepo repository.ProductRepository
	userRepo repository.UserRepository
}

func NewProductService(prodRepo repository.ProductRepository, userRepo repository.UserRepository) ProductService {
	return &productService{
		prodRepo: prodRepo,
		userRepo: userRepo,
	}
}

func (s *productService) ListUserProducts(ctx context.Context, userID uuid.UUID, category string) ([]domain.Product, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	return s.prodRepo.List(ctx, category, user.AllowedCategories, user.AllowedManufacturers)
}

func (s *productService) CreateProduct(ctx context.Context, req *domain.CreateProductRequest) (*domain.Product, error) {
	category := strings.TrimSpace(req.Category)
	manufacturer := strings.TrimSpace(req.Manufacturer)
	model := strings.TrimSpace(req.Model)

	if category == "" || manufacturer == "" || model == "" {
		return nil, fmt.Errorf("%w: category, manufacturer, and model are required", domain.ErrInvalidInput)
	}

	if req.Price < 0 {
		return nil, fmt.Errorf("%w: price cannot be negative", domain.ErrInvalidInput)
	}

	if req.StockQuantity < 0 {
		return nil, fmt.Errorf("%w: stock_quantity cannot be negative", domain.ErrInvalidInput)
	}

	product := &domain.Product{
		ID:            uuid.New(),
		Category:      category,
		Manufacturer:  manufacturer,
		Model:         model,
		Price:         req.Price,
		StockQuantity: req.StockQuantity,
	}

	if err := s.prodRepo.Create(ctx, product); err != nil {
		return nil, err
	}

	return product, nil
}

func (s *productService) UpdateStock(ctx context.Context, productID uuid.UUID, stockQuantity int) (*domain.Product, error) {
	if stockQuantity < 0 {
		return nil, fmt.Errorf("%w: stock_quantity cannot be negative", domain.ErrInvalidInput)
	}

	return s.prodRepo.UpdateStock(ctx, productID, stockQuantity)
}

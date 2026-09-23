package service

import (
	"context"
	"fmt"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
	"github.com/altregubov/warehouse-rest-test-app/internal/repository"
	"github.com/google/uuid"
)

type OrderService interface {
	CreateOrder(ctx context.Context, userID, productID uuid.UUID, quantity int) (*domain.OrderResponse, error)
}

type orderService struct {
	orderRepo repository.OrderRepository
}

func NewOrderService(orderRepo repository.OrderRepository) OrderService {
	return &orderService{orderRepo: orderRepo}
}

func (s *orderService) CreateOrder(ctx context.Context, userID, productID uuid.UUID, quantity int) (*domain.OrderResponse, error) {
	if quantity <= 0 {
		return nil, fmt.Errorf("%w: quantity must be at least 1", domain.ErrInvalidInput)
	}

	return s.orderRepo.CreateOrderTx(ctx, userID, productID, quantity)
}

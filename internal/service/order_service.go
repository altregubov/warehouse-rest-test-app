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
	ListUserOrders(ctx context.Context, userID uuid.UUID) ([]*domain.OrderResponse, error)
	GetOrderByID(ctx context.Context, orderID, userID uuid.UUID, isAdmin bool) (*domain.OrderResponse, error)
	ListAllOrders(ctx context.Context) ([]*domain.OrderResponse, error)
	UpdateOrderStatus(ctx context.Context, orderID uuid.UUID, status string) (*domain.OrderResponse, error)
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

func (s *orderService) ListUserOrders(ctx context.Context, userID uuid.UUID) ([]*domain.OrderResponse, error) {
	return s.orderRepo.ListByUserID(ctx, userID)
}

func (s *orderService) GetOrderByID(ctx context.Context, orderID, userID uuid.UUID, isAdmin bool) (*domain.OrderResponse, error) {
	order, err := s.orderRepo.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if !isAdmin && order.UserID != userID {
		return nil, domain.ErrNotFound
	}
	return order, nil
}

func (s *orderService) ListAllOrders(ctx context.Context) ([]*domain.OrderResponse, error) {
	return s.orderRepo.ListAll(ctx)
}

func (s *orderService) UpdateOrderStatus(ctx context.Context, orderID uuid.UUID, status string) (*domain.OrderResponse, error) {
	validStatuses := map[string]bool{
		"CANCELLED": true,
	}
	if !validStatuses[status] {
		return nil, fmt.Errorf("%w: invalid order status %s (admin can only set status to CANCELLED)", domain.ErrInvalidInput, status)
	}
	return s.orderRepo.UpdateStatus(ctx, orderID, status)
}

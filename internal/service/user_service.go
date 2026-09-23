package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
	"github.com/altregubov/warehouse-rest-test-app/internal/repository"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type UserService interface {
	GetProfile(ctx context.Context, userID uuid.UUID) (*domain.UserSummary, error)
	CreateUser(ctx context.Context, req *domain.CreateUserRequest) (*domain.UserSummary, error)
	UpdateBalance(ctx context.Context, userID uuid.UUID, amount float64) (*domain.UserSummary, error)
	UpdateFilters(ctx context.Context, userID uuid.UUID, categories, manufacturers []string) (*domain.UserSummary, error)
}

type userService struct {
	userRepo repository.UserRepository
}

func NewUserService(userRepo repository.UserRepository) UserService {
	return &userService{userRepo: userRepo}
}

func (s *userService) GetProfile(ctx context.Context, userID uuid.UUID) (*domain.UserSummary, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	return toUserSummary(user), nil
}

func (s *userService) CreateUser(ctx context.Context, req *domain.CreateUserRequest) (*domain.UserSummary, error) {
	username := strings.TrimSpace(req.Username)
	if username == "" || len(req.Password) < 4 {
		return nil, fmt.Errorf("%w: username must not be empty and password must be at least 4 characters", domain.ErrInvalidInput)
	}

	role := strings.ToLower(strings.TrimSpace(req.Role))
	if role != domain.RoleAdmin && role != domain.RoleUser {
		return nil, fmt.Errorf("%w: role must be 'admin' or 'user'", domain.ErrInvalidInput)
	}

	if req.Balance < 0 {
		return nil, fmt.Errorf("%w: balance cannot be negative", domain.ErrInvalidInput)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	user := &domain.User{
		ID:                   uuid.New(),
		Username:             username,
		PasswordHash:         string(hash),
		Role:                 role,
		Balance:              req.Balance,
		AllowedCategories:   req.AllowedCategories,
		AllowedManufacturers: req.AllowedManufacturers,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}

	return toUserSummary(user), nil
}

func (s *userService) UpdateBalance(ctx context.Context, userID uuid.UUID, amount float64) (*domain.UserSummary, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	newBalance := user.Balance + amount
	if newBalance < 0 {
		return nil, fmt.Errorf("%w: resulting balance cannot be negative", domain.ErrInvalidInput)
	}

	updatedUser, err := s.userRepo.UpdateBalance(ctx, userID, newBalance)
	if err != nil {
		return nil, err
	}

	return toUserSummary(updatedUser), nil
}

func (s *userService) UpdateFilters(ctx context.Context, userID uuid.UUID, categories, manufacturers []string) (*domain.UserSummary, error) {
	if categories == nil {
		categories = []string{}
	}
	if manufacturers == nil {
		manufacturers = []string{}
	}

	updatedUser, err := s.userRepo.UpdateFilters(ctx, userID, categories, manufacturers)
	if err != nil {
		return nil, err
	}

	return toUserSummary(updatedUser), nil
}

func toUserSummary(u *domain.User) *domain.UserSummary {
	if u == nil {
		return nil
	}
	allowedCategories := u.AllowedCategories
	if allowedCategories == nil {
		allowedCategories = []string{}
	}
	allowedManufacturers := u.AllowedManufacturers
	if allowedManufacturers == nil {
		allowedManufacturers = []string{}
	}
	return &domain.UserSummary{
		ID:                   u.ID,
		Username:             u.Username,
		Role:                 u.Role,
		Balance:              u.Balance,
		AllowedCategories:   allowedCategories,
		AllowedManufacturers: allowedManufacturers,
	}
}

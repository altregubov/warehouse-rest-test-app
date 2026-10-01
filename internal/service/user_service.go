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
	GetUserByID(ctx context.Context, userID uuid.UUID) (*domain.UserSummary, error)
	ListUsers(ctx context.Context, role string, page, pageSize int) ([]*domain.UserSummary, int, error)
	CreateUser(ctx context.Context, req *domain.CreateUserRequest) (*domain.UserSummary, error)
	TopUpBalance(ctx context.Context, userID uuid.UUID, incrementAmount float64) (*domain.UserSummary, error)
	UpdateFilters(ctx context.Context, userID uuid.UUID, req *domain.UpdateFiltersRequest) (*domain.UserSummary, error)
	DeleteUser(ctx context.Context, id uuid.UUID) error
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

func (s *userService) GetUserByID(ctx context.Context, userID uuid.UUID) (*domain.UserSummary, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return toUserSummary(user), nil
}

func (s *userService) ListUsers(ctx context.Context, role string, page, pageSize int) ([]*domain.UserSummary, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	users, total, err := s.userRepo.List(ctx, role, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}

	summaries := make([]*domain.UserSummary, len(users))
	for i, u := range users {
		summaries[i] = toUserSummary(u)
	}
	return summaries, total, nil
}

func normalizeStringSlice(items []string) []string {
	if items == nil {
		return []string{}
	}
	result := make([]string, 0, len(items))
	seen := make(map[string]bool)
	for _, item := range items {
		norm := strings.ToLower(strings.TrimSpace(item))
		if norm != "" && !seen[norm] {
			seen[norm] = true
			result = append(result, norm)
		}
	}
	return result
}

const BcryptCost = 12

func (s *userService) CreateUser(ctx context.Context, req *domain.CreateUserRequest) (*domain.UserSummary, error) {
	username := strings.TrimSpace(req.Username)
	if username == "" {
		return nil, fmt.Errorf("%w: username must not be empty", domain.ErrInvalidInput)
	}
	if strings.TrimSpace(req.Password) == "" {
		return nil, fmt.Errorf("%w: password must not be empty", domain.ErrInvalidInput)
	}

	role := strings.ToLower(strings.TrimSpace(req.Role))
	if role != domain.RoleAdmin && role != domain.RoleUser {
		return nil, fmt.Errorf("%w: role must be 'admin' or 'user'", domain.ErrInvalidInput)
	}

	if req.Balance < 0 {
		return nil, fmt.Errorf("%w: balance cannot be negative", domain.ErrInvalidInput)
	}

	normCats := normalizeStringSlice(req.AllowedCategories)
	normMfgs := normalizeStringSlice(req.AllowedManufacturers)

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	balCents := domain.DollarsToCents(req.Balance)
	user := &domain.User{
		ID:                   uuid.New(),
		Username:             username,
		PasswordHash:         string(hash),
		Role:                 role,
		Balance:              domain.CentsToDollars(balCents),
		AllowedCategories:   normCats,
		AllowedManufacturers: normMfgs,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}

	return toUserSummary(user), nil
}

func (s *userService) TopUpBalance(ctx context.Context, userID uuid.UUID, incrementAmount float64) (*domain.UserSummary, error) {
	if incrementAmount < 0.01 {
		return nil, fmt.Errorf("%w: increment amount must be at least 0.01", domain.ErrInvalidInput)
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	currBalCents := domain.DollarsToCents(user.Balance)
	incCents := domain.DollarsToCents(incrementAmount)
	newBalCents := currBalCents + incCents

	newBalance := domain.CentsToDollars(newBalCents)
	updatedUser, err := s.userRepo.UpdateBalance(ctx, userID, newBalance)
	if err != nil {
		return nil, err
	}

	return toUserSummary(updatedUser), nil
}

func (s *userService) UpdateFilters(ctx context.Context, userID uuid.UUID, req *domain.UpdateFiltersRequest) (*domain.UserSummary, error) {
	normCats := normalizeStringSlice(req.AllowedCategories)
	normMfgs := normalizeStringSlice(req.AllowedManufacturers)

	updatedUser, err := s.userRepo.UpdateFilters(ctx, userID, normCats, normMfgs)
	if err != nil {
		return nil, err
	}

	return toUserSummary(updatedUser), nil
}

func (s *userService) DeleteUser(ctx context.Context, id uuid.UUID) error {
	return s.userRepo.SoftDelete(ctx, id)
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

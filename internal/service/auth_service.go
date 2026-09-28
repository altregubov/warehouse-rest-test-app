package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
	"github.com/altregubov/warehouse-rest-test-app/internal/repository"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type JWTClaims struct {
	UserID   uuid.UUID `json:"sub"`
	Username string    `json:"username"`
	Role     string    `json:"role"`
	jwt.RegisteredClaims
}

type AuthService interface {
	Login(ctx context.Context, username, password, expectedRole string) (*domain.LoginResponse, error)
	GenerateToken(user *domain.User) (string, error)
	ValidateToken(tokenString string) (*JWTClaims, error)
}

type authService struct {
	userRepo  repository.UserRepository
	jwtSecret []byte
}

func NewAuthService(userRepo repository.UserRepository, jwtSecret string) AuthService {
	return &authService{
		userRepo:  userRepo,
		jwtSecret: []byte(jwtSecret),
	}
}

const dummyPasswordHash = "$2a$10$RUP6Lknor1aYWPyngT8WjOkiwFpkibEmguyv7e5gTbKae/hn5OAKW"

func (s *authService) Login(ctx context.Context, username, password, expectedRole string) (*domain.LoginResponse, error) {
	user, err := s.userRepo.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			// Perform dummy hash comparison to ensure constant-time execution and prevent user enumeration
			_ = bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(password))
			return nil, domain.ErrInvalidCredentials
		}
		return nil, err
	}

	// Always evaluate bcrypt hash comparison
	pwdErr := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	// Unify wrong password and unauthorized role into identical ErrInvalidCredentials
	if pwdErr != nil || user.Role != expectedRole {
		return nil, domain.ErrInvalidCredentials
	}

	token, err := s.GenerateToken(user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	return &domain.LoginResponse{
		Token: token,
		User: domain.UserSummary{
			ID:                   user.ID,
			Username:             user.Username,
			Role:                 user.Role,
			Balance:              user.Balance,
			AllowedCategories:   user.AllowedCategories,
			AllowedManufacturers: user.AllowedManufacturers,
		},
	}, nil
}

func (s *authService) GenerateToken(user *domain.User) (string, error) {
	claims := JWTClaims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID.String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

func (s *authService) ValidateToken(tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.jwtSecret, nil
	})

	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token claims")
	}

	return claims, nil
}

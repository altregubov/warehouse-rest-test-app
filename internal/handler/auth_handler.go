package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
	"github.com/altregubov/warehouse-rest-test-app/internal/service"
)

type AuthHandler struct {
	authService service.AuthService
}

func NewAuthHandler(authService service.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

// AdminLogin godoc
// @Summary Admin authentication
// @Description Authenticate an admin user and return a JWT token with admin claims
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body domain.LoginRequest true "Admin credentials"
// @Success 200 {object} domain.SuccessEnvelope{data=domain.LoginResponse} "Login successful"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid request payload"
// @Failure 401 {object} domain.ErrorEnvelope "Invalid username or password"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Router /api/admin/login [post]
func (h *AuthHandler) AdminLogin(w http.ResponseWriter, r *http.Request) {
	var req domain.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to parse JSON body")
		return
	}

	resp, err := h.authService.Login(r.Context(), req.Username, req.Password, domain.RoleAdmin)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCredentials) || errors.Is(err, domain.ErrForbiddenRole) {
			Error(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid username or password")
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
		return
	}

	JSON(w, http.StatusOK, resp)
}

// UserLogin godoc
// @Summary User authentication
// @Description Authenticate a regular user and return a JWT token with user claims
// @Tags Auth
// @Accept json
// @Produce json
// @Param request body domain.LoginRequest true "User credentials"
// @Success 200 {object} domain.SuccessEnvelope{data=domain.LoginResponse} "Login successful"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid request payload"
// @Failure 401 {object} domain.ErrorEnvelope "Invalid username or password"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Router /api/user/login [post]
func (h *AuthHandler) UserLogin(w http.ResponseWriter, r *http.Request) {
	var req domain.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to parse JSON body")
		return
	}

	resp, err := h.authService.Login(r.Context(), req.Username, req.Password, domain.RoleUser)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCredentials) || errors.Is(err, domain.ErrForbiddenRole) {
			Error(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid username or password")
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Internal server error")
		return
	}

	JSON(w, http.StatusOK, resp)
}

package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
	"github.com/altregubov/warehouse-rest-test-app/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type AdminHandler struct {
	userService    service.UserService
	productService service.ProductService
}

func NewAdminHandler(userService service.UserService, productService service.ProductService) *AdminHandler {
	return &AdminHandler{
		userService:    userService,
		productService: productService,
	}
}

// CreateUser godoc
// @Summary Create a user account
// @Description Create a new admin or regular user account
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body domain.CreateUserRequest true "User details"
// @Success 201 {object} domain.SuccessEnvelope{data=domain.UserSummary} "User created"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid input"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 409 {object} domain.ErrorEnvelope "Username already exists"
// @Router /api/admin/users [post]
func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to parse JSON body")
		return
	}

	user, err := h.userService.CreateUser(r.Context(), &req)
	if err != nil {
		if errors.Is(err, domain.ErrUsernameTaken) {
			Error(w, http.StatusConflict, "USERNAME_TAKEN", "Username already exists")
			return
		}
		if errors.Is(err, domain.ErrInvalidInput) {
			Error(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create user")
		return
	}

	JSON(w, http.StatusCreated, user)
}

// UpdateBalance godoc
// @Summary Update or top up user balance
// @Description Adjust user account balance by a specified amount (e.g. +500.00)
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "User ID (UUID)"
// @Param request body domain.UpdateBalanceRequest true "Balance adjustment payload"
// @Success 200 {object} domain.SuccessEnvelope{data=domain.UserSummary} "Balance updated"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid input or negative balance"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 404 {object} domain.ErrorEnvelope "User not found"
// @Router /api/admin/users/{id}/balance [patch]
func (h *AdminHandler) UpdateBalance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		Error(w, http.StatusBadRequest, "INVALID_ID", "Invalid user ID format (UUID expected)")
		return
	}

	var req domain.UpdateBalanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to parse JSON body")
		return
	}

	updatedUser, err := h.userService.UpdateBalance(r.Context(), userID, req.Amount)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			Error(w, http.StatusNotFound, "NOT_FOUND", "User not found")
			return
		}
		if errors.Is(err, domain.ErrInvalidInput) {
			Error(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update balance")
		return
	}

	JSON(w, http.StatusOK, updatedUser)
}

// UpdateFilters godoc
// @Summary Configure catalog access filters for a user
// @Description Set allowed categories and manufacturers for a user. Empty array removes restrictions.
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "User ID (UUID)"
// @Param request body domain.UpdateFiltersRequest true "User filter permissions"
// @Success 200 {object} domain.SuccessEnvelope{data=domain.UserSummary} "Filters updated"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid input"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 404 {object} domain.ErrorEnvelope "User not found"
// @Router /api/admin/users/{id}/filters [put]
func (h *AdminHandler) UpdateFilters(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		Error(w, http.StatusBadRequest, "INVALID_ID", "Invalid user ID format (UUID expected)")
		return
	}

	var req domain.UpdateFiltersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to parse JSON body")
		return
	}

	updatedUser, err := h.userService.UpdateFilters(r.Context(), userID, req.AllowedCategories, req.AllowedManufacturers)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			Error(w, http.StatusNotFound, "NOT_FOUND", "User not found")
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update filters")
		return
	}

	JSON(w, http.StatusOK, updatedUser)
}

// CreateProduct godoc
// @Summary Add a new product to warehouse
// @Description Add a new product item with arbitrary category, manufacturer, model, price, and stock
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body domain.CreateProductRequest true "Product details"
// @Success 201 {object} domain.SuccessEnvelope{data=domain.Product} "Product created"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid input"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Router /api/admin/products [post]
func (h *AdminHandler) CreateProduct(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to parse JSON body")
		return
	}

	product, err := h.productService.CreateProduct(r.Context(), &req)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidInput) {
			Error(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to create product")
		return
	}

	JSON(w, http.StatusCreated, product)
}

// UpdateStock godoc
// @Summary Update product stock quantity
// @Description Set stock quantity for a warehouse product
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Product ID (UUID)"
// @Param request body domain.UpdateStockRequest true "Stock update payload"
// @Success 200 {object} domain.SuccessEnvelope{data=domain.Product} "Stock updated"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid input"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 404 {object} domain.ErrorEnvelope "Product not found"
// @Router /api/admin/products/{id}/stock [patch]
func (h *AdminHandler) UpdateStock(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	prodID, err := uuid.Parse(idStr)
	if err != nil {
		Error(w, http.StatusBadRequest, "INVALID_ID", "Invalid product ID format (UUID expected)")
		return
	}

	var req domain.UpdateStockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to parse JSON body")
		return
	}

	product, err := h.productService.UpdateStock(r.Context(), prodID, req.StockQuantity)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			Error(w, http.StatusNotFound, "NOT_FOUND", "Product not found")
			return
		}
		if errors.Is(err, domain.ErrInvalidInput) {
			Error(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update product stock")
		return
	}

	JSON(w, http.StatusOK, product)
}

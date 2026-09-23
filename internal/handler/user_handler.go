package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
	"github.com/altregubov/warehouse-rest-test-app/internal/middleware"
	"github.com/altregubov/warehouse-rest-test-app/internal/service"
)

type UserHandler struct {
	userService    service.UserService
	productService service.ProductService
	orderService   service.OrderService
}

func NewUserHandler(userService service.UserService, productService service.ProductService, orderService service.OrderService) *UserHandler {
	return &UserHandler{
		userService:    userService,
		productService: productService,
		orderService:   orderService,
	}
}

// GetProfile godoc
// @Summary Get authenticated user profile
// @Description Returns the profile, current balance, and assigned filter permissions for the logged in user
// @Tags User
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} domain.SuccessEnvelope{data=domain.UserSummary} "User profile"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 404 {object} domain.ErrorEnvelope "User not found"
// @Router /api/user/profile [get]
func (h *UserHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "User identity not found in context")
		return
	}

	profile, err := h.userService.GetProfile(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			Error(w, http.StatusNotFound, "NOT_FOUND", "User profile not found")
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to retrieve profile")
		return
	}

	JSON(w, http.StatusOK, profile)
}

// ListProducts godoc
// @Summary Browse warehouse catalog
// @Description Browse warehouse items with optional category query filter and strict permission filtering
// @Tags User
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param category query string false "Filter by category (e.g. laptop, smartphone)"
// @Success 200 {object} domain.SuccessEnvelope{data=[]domain.Product} "List of allowed products"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Router /api/user/products [get]
func (h *UserHandler) ListProducts(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "User identity not found in context")
		return
	}

	categoryParam := r.URL.Query().Get("category")

	products, err := h.productService.ListUserProducts(r.Context(), userID, categoryParam)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			Error(w, http.StatusNotFound, "NOT_FOUND", "User not found")
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list products")
		return
	}

	JSON(w, http.StatusOK, products)
}

// CreateOrder godoc
// @Summary Purchase product from warehouse
// @Description Atomically purchases a product item, decrements stock, and deducts user balance
// @Tags User
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param request body domain.CreateOrderRequest true "Purchase order request"
// @Success 201 {object} domain.SuccessEnvelope{data=domain.OrderResponse} "Order placed successfully"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid input, insufficient stock or balance"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Product disallowed by user filters"
// @Failure 404 {object} domain.ErrorEnvelope "Product or user not found"
// @Router /api/user/orders [post]
func (h *UserHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "User identity not found in context")
		return
	}

	var req domain.CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to parse JSON body")
		return
	}

	orderResp, err := h.orderService.CreateOrder(r.Context(), userID, req.ProductID, req.Quantity)
	if err != nil {
		if errors.Is(err, domain.ErrProductDisallowed) {
			Error(w, http.StatusForbidden, "PRODUCT_DISALLOWED", "Product is restricted by your account filters")
			return
		}
		if errors.Is(err, domain.ErrInsufficientStock) {
			Error(w, http.StatusBadRequest, "INSUFFICIENT_STOCK", "Product does not have sufficient stock")
			return
		}
		if errors.Is(err, domain.ErrInsufficientBalance) {
			Error(w, http.StatusBadRequest, "INSUFFICIENT_FUNDS", "Account balance is insufficient for this purchase")
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			Error(w, http.StatusNotFound, "NOT_FOUND", "Product or user not found")
			return
		}
		if errors.Is(err, domain.ErrInvalidInput) {
			Error(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to process order")
		return
	}

	JSON(w, http.StatusCreated, orderResp)
}

package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
	"github.com/altregubov/warehouse-rest-test-app/internal/middleware"
	"github.com/altregubov/warehouse-rest-test-app/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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
// @Param Idempotency-Key header string false "Unique idempotency key to prevent double processing"
// @Param request body domain.CreateOrderRequest true "Purchase order request"
// @Success 201 {object} domain.SuccessEnvelope{data=domain.OrderResponse} "Order placed successfully"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid input, insufficient stock or balance"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Product disallowed by user filters"
// @Failure 404 {object} domain.ErrorEnvelope "Product or user not found"
// @Failure 409 {object} domain.ErrorEnvelope "Idempotency conflict or concurrent request in flight"
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

// ListOrders godoc
// @Summary List customer purchase history
// @Description Returns all orders placed by the authenticated user
// @Tags User
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} domain.SuccessEnvelope{data=[]domain.OrderResponse} "List of orders"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Router /api/user/orders [get]
func (h *UserHandler) ListOrders(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "User identity not found in context")
		return
	}

	orders, err := h.orderService.ListUserOrders(r.Context(), userID)
	if err != nil {
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to retrieve orders")
		return
	}

	JSON(w, http.StatusOK, orders)
}

// GetOrder godoc
// @Summary Get customer order details
// @Description Returns details for a specific order placed by the authenticated user
// @Tags User
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Order ID (UUID)"
// @Success 200 {object} domain.SuccessEnvelope{data=domain.OrderResponse} "Order details"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 404 {object} domain.ErrorEnvelope "Order not found"
// @Router /api/user/orders/{id} [get]
func (h *UserHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "User identity not found in context")
		return
	}

	idStr := chi.URLParam(r, "id")
	orderID, err := uuid.Parse(idStr)
	if err != nil {
		Error(w, http.StatusBadRequest, "INVALID_ID", "Invalid order ID format")
		return
	}

	order, err := h.orderService.GetOrderByID(r.Context(), orderID, userID, false)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			Error(w, http.StatusNotFound, "NOT_FOUND", "Order not found")
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to retrieve order")
		return
	}

	JSON(w, http.StatusOK, order)
}

package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

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
// @Produce json
// @Security BearerAuth
// @Success 200 {object} domain.SuccessEnvelope{data=domain.UserSummary} "User profile"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 404 {object} domain.ErrorEnvelope "User not found"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
// @Router /api/user/profile [get]
// @ID getUserProfile
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
// @Description Browse warehouse items with optional category/manufacturer filters, sorting, and pagination
// @Tags User
// @Produce json
// @Security BearerAuth
// @Param category query string false "Filter by category (e.g. laptop, smartphone)"
// @Param manufacturer query string false "Filter by manufacturer (e.g. Apple, Dell)"
// @Param sort_by query string false "Sort field (price, created_at, model)" Enums(price, created_at, model)
// @Param order query string false "Sort order (asc, desc)" Enums(asc, desc)
// @Param page query int false "Page number (default: 1, min: 1)" default(1) minimum(1)
// @Param page_size query int false "Page size (default: 20, min: 1, max: 100)" default(20) minimum(1) maximum(100)
// @Success 200 {object} domain.SuccessEnvelope{data=[]domain.Product} "List of allowed products with pagination"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid pagination, sorting, or filter parameters"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 404 {object} domain.ErrorEnvelope "User not found"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
// @Router /api/user/products [get]
// @ID listUserProducts
func (h *UserHandler) ListProducts(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		Error(w, http.StatusUnauthorized, "UNAUTHORIZED", "User identity not found in context")
		return
	}

	query := r.URL.Query()
	categoryParam := query.Get("category")
	manufacturerParam := query.Get("manufacturer")

	page := 1
	if pageStr := query.Get("page"); pageStr != "" {
		p, err := strconv.Atoi(pageStr)
		if err != nil || p < 1 {
			Error(w, http.StatusBadRequest, "INVALID_INPUT", "page must be an integer >= 1")
			return
		}
		page = p
	}

	pageSize := 20
	pageSizeStr := query.Get("page_size")
	if pageSizeStr == "" {
		pageSizeStr = query.Get("limit")
	}
	if pageSizeStr != "" {
		ps, err := strconv.Atoi(pageSizeStr)
		if err != nil || ps < 1 || ps > 100 {
			Error(w, http.StatusBadRequest, "INVALID_INPUT", "page_size must be an integer between 1 and 100")
			return
		}
		pageSize = ps
	}

	sortBy := strings.ToLower(strings.TrimSpace(query.Get("sort_by")))
	if sortBy != "" && sortBy != "price" && sortBy != "created_at" && sortBy != "model" {
		Error(w, http.StatusBadRequest, "INVALID_INPUT", "sort_by must be one of: price, created_at, model")
		return
	}

	order := strings.ToLower(strings.TrimSpace(query.Get("order")))
	if order != "" && order != "asc" && order != "desc" {
		Error(w, http.StatusBadRequest, "INVALID_INPUT", "order must be asc or desc")
		return
	}

	params := domain.ProductFilterParams{
		Category:     categoryParam,
		Manufacturer: manufacturerParam,
		SortBy:       sortBy,
		Order:        order,
		Page:         page,
		PageSize:     pageSize,
	}

	products, totalCount, err := h.productService.ListUserProducts(r.Context(), userID, params)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			Error(w, http.StatusNotFound, "NOT_FOUND", "User not found")
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list products")
		return
	}

	JSONPaginated(w, http.StatusOK, products, page, pageSize, totalCount)
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
// @Failure 400 {object} domain.ErrorEnvelope "Malformed JSON syntax or schema validation failure"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 404 {object} domain.ErrorEnvelope "Product or user not found"
// @Failure 422 {object} domain.ErrorEnvelope "Domain business rule violation (INSUFFICIENT_FUNDS, INSUFFICIENT_STOCK, FILTER_RESTRICTION)"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
// @Router /api/user/orders [post]
// @ID createOrder
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

	if req.Quantity <= 0 {
		Error(w, http.StatusBadRequest, "INVALID_INPUT", "quantity must be greater than 0")
		return
	}

	orderResp, err := h.orderService.CreateOrder(r.Context(), userID, req.ProductID, req.Quantity)
	if err != nil {
		if errors.Is(err, domain.ErrProductDisallowed) {
			Error(w, 422, "FILTER_RESTRICTION", "Product is restricted by your account filters")
			return
		}
		if errors.Is(err, domain.ErrInsufficientStock) {
			Error(w, 422, "INSUFFICIENT_STOCK", "Product does not have sufficient stock")
			return
		}
		if errors.Is(err, domain.ErrInsufficientBalance) {
			Error(w, 422, "INSUFFICIENT_FUNDS", "Account balance is insufficient for this purchase")
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
// @Produce json
// @Security BearerAuth
// @Success 200 {object} domain.SuccessEnvelope{data=[]domain.OrderResponse} "List of orders"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
// @Router /api/user/orders [get]
// @ID listUserOrders
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
// @Produce json
// @Security BearerAuth
// @Param id path string true "Order ID (UUID)" Format(uuid)
// @Success 200 {object} domain.SuccessEnvelope{data=domain.OrderResponse} "Order details"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid order ID format"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 404 {object} domain.ErrorEnvelope "Order not found"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
// @Router /api/user/orders/{id} [get]
// @ID getUserOrderById
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

package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
	"github.com/altregubov/warehouse-rest-test-app/internal/service"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type AdminHandler struct {
	userService    service.UserService
	productService service.ProductService
	orderService   service.OrderService
}

func NewAdminHandler(userService service.UserService, productService service.ProductService, orderService service.OrderService) *AdminHandler {
	return &AdminHandler{
		userService:    userService,
		productService: productService,
		orderService:   orderService,
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
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
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

// TopUpBalance godoc
// @Summary Top up user balance by an increment
// @Description Increases the user account balance by a specified increment amount (minimum 0.01)
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param Idempotency-Key header string false "Unique idempotency key to prevent double processing"
// @Param id path string true "User ID (UUID)" Format(uuid)
// @Param request body domain.TopUpBalanceRequest true "Balance top-up payload"
// @Success 200 {object} domain.SuccessEnvelope{data=domain.UserSummary} "Balance topped up"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid input"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 404 {object} domain.ErrorEnvelope "User not found"
// @Failure 409 {object} domain.ErrorEnvelope "Idempotency conflict or concurrent request in flight"
// @Failure 422 {object} domain.ErrorEnvelope "Unprocessable entity / validation failure"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
// @Router /api/admin/users/{id}/balance/top-up [post]
func (h *AdminHandler) TopUpBalance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		Error(w, http.StatusBadRequest, "INVALID_ID", "Invalid user ID format (UUID expected)")
		return
	}

	var req struct {
		IncrementAmount *float64 `json:"increment_amount"`
		Amount          *float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to parse JSON body")
		return
	}

	amount := 0.0
	if req.IncrementAmount != nil {
		amount = *req.IncrementAmount
	} else if req.Amount != nil {
		amount = *req.Amount
	} else {
		Error(w, http.StatusBadRequest, "INVALID_INPUT", "increment_amount is required")
		return
	}

	if amount < 0.01 {
		ErrorWithDetails(w, 422, "INVALID_INPUT", "increment_amount must be at least 0.01", []domain.FieldViolation{
			{Field: "increment_amount", Issue: "must be at least 0.01"},
		})
		return
	}

	updatedUser, err := h.userService.TopUpBalance(r.Context(), userID, amount)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			Error(w, http.StatusNotFound, "NOT_FOUND", "User not found")
			return
		}
		if errors.Is(err, domain.ErrInvalidInput) {
			Error(w, 422, "INVALID_INPUT", err.Error())
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to top up balance")
		return
	}

	JSON(w, http.StatusOK, updatedUser)
}

// SetBalance godoc
// @Summary Set absolute user balance
// @Description Replaces the user account balance with a specified new balance (minimum 0.00)
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param Idempotency-Key header string false "Unique idempotency key to prevent double processing"
// @Param id path string true "User ID (UUID)" Format(uuid)
// @Param request body domain.SetBalanceRequest true "Absolute balance payload"
// @Success 200 {object} domain.SuccessEnvelope{data=domain.UserSummary} "Balance set"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid input"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 404 {object} domain.ErrorEnvelope "User not found"
// @Failure 409 {object} domain.ErrorEnvelope "Idempotency conflict or concurrent request in flight"
// @Failure 422 {object} domain.ErrorEnvelope "Unprocessable entity / validation failure"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
// @Router /api/admin/users/{id}/balance [put]
func (h *AdminHandler) SetBalance(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		Error(w, http.StatusBadRequest, "INVALID_ID", "Invalid user ID format (UUID expected)")
		return
	}

	var req struct {
		NewBalance *float64 `json:"new_balance"`
		Amount     *float64 `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to parse JSON body")
		return
	}

	newBalance := 0.0
	if req.NewBalance != nil {
		newBalance = *req.NewBalance
	} else if req.Amount != nil {
		newBalance = *req.Amount
	} else {
		Error(w, http.StatusBadRequest, "INVALID_INPUT", "new_balance is required")
		return
	}

	if newBalance < 0 {
		Error(w, 422, "INVALID_INPUT", "new_balance cannot be negative")
		return
	}

	updatedUser, err := h.userService.SetBalance(r.Context(), userID, newBalance)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			Error(w, http.StatusNotFound, "NOT_FOUND", "User not found")
			return
		}
		if errors.Is(err, domain.ErrInvalidInput) {
			Error(w, 422, "INVALID_INPUT", err.Error())
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to set balance")
		return
	}

	JSON(w, http.StatusOK, updatedUser)
}

// UpdateBalance godoc
// @Summary Update or top up user balance (Legacy)
// @Description Adjust user account balance by a specified amount (e.g. +500.00)
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param Idempotency-Key header string false "Unique idempotency key to prevent double processing"
// @Param id path string true "User ID (UUID)" Format(uuid)
// @Param request body domain.UpdateBalanceRequest true "Balance adjustment payload"
// @Success 200 {object} domain.SuccessEnvelope{data=domain.UserSummary} "Balance updated"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid input or negative balance"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 404 {object} domain.ErrorEnvelope "User not found"
// @Failure 409 {object} domain.ErrorEnvelope "Idempotency conflict or concurrent request in flight"
// @Failure 422 {object} domain.ErrorEnvelope "Unprocessable entity / validation failure"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
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
// @Param id path string true "User ID (UUID)" Format(uuid)
// @Param request body domain.UpdateFiltersRequest true "User filter permissions"
// @Success 200 {object} domain.SuccessEnvelope{data=domain.UserSummary} "Filters updated"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid input"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 404 {object} domain.ErrorEnvelope "User not found"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
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

	updatedUser, err := h.userService.UpdateFilters(r.Context(), userID, &req)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			Error(w, http.StatusNotFound, "NOT_FOUND", "User not found")
			return
		}
		if errors.Is(err, domain.ErrInvalidInput) {
			Error(w, http.StatusBadRequest, "INVALID_INPUT", err.Error())
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
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
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
// @Param id path string true "Product ID (UUID)" Format(uuid)
// @Param request body domain.UpdateStockRequest true "Stock update payload"
// @Success 200 {object} domain.SuccessEnvelope{data=domain.Product} "Stock updated"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid input"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 404 {object} domain.ErrorEnvelope "Product not found"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
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

	if req.StockQuantity < 0 {
		Error(w, http.StatusBadRequest, "INVALID_INPUT", "stock_quantity cannot be negative")
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

// ListUsers godoc
// @Summary List users
// @Description Query paginated list of users with optional role filtering
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param role query string false "Filter by role (admin or user)"
// @Param page query int false "Page number (default 1)"
// @Param page_size query int false "Items per page (default 20, max 100)"
// @Success 200 {object} domain.SuccessEnvelope{data=[]domain.UserSummary} "List of users"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid query parameters"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
// @Router /api/admin/users [get]
func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	role := r.URL.Query().Get("role")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))

	users, _, err := h.userService.ListUsers(r.Context(), role, page, pageSize)
	if err != nil {
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list users")
		return
	}

	JSON(w, http.StatusOK, users)
}

// GetUser godoc
// @Summary Get user by ID
// @Description Retrieve a specific user account by UUID
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "User ID (UUID)" Format(uuid)
// @Success 200 {object} domain.SuccessEnvelope{data=domain.UserSummary} "User details"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid ID"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 404 {object} domain.ErrorEnvelope "User not found"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
// @Router /api/admin/users/{id} [get]
func (h *AdminHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		Error(w, http.StatusBadRequest, "INVALID_ID", "Invalid user ID format (UUID expected)")
		return
	}

	user, err := h.userService.GetUserByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			Error(w, http.StatusNotFound, "NOT_FOUND", "User not found")
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to get user")
		return
	}

	JSON(w, http.StatusOK, user)
}

// DeleteUser godoc
// @Summary Soft-delete a user account
// @Description Marks user as deactivated/deleted while preserving immutable historical order records
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "User ID (UUID)" Format(uuid)
// @Success 200 {object} domain.SuccessEnvelope{data=string} "User deactivated successfully"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid ID"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 404 {object} domain.ErrorEnvelope "User not found"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
// @Router /api/admin/users/{id} [delete]
func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		Error(w, http.StatusBadRequest, "INVALID_ID", "Invalid user ID format (UUID expected)")
		return
	}

	if err := h.userService.DeleteUser(r.Context(), userID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			Error(w, http.StatusNotFound, "NOT_FOUND", "User not found")
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to delete user")
		return
	}

	JSON(w, http.StatusOK, map[string]string{"message": "User deactivated successfully"})
}

// ListProducts godoc
// @Summary List all warehouse products (Admin)
// @Description Retrieve full unrestricted product inventory for administrators
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} domain.SuccessEnvelope{data=[]domain.Product} "List of all products"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
// @Router /api/admin/products [get]
func (h *AdminHandler) ListProducts(w http.ResponseWriter, r *http.Request) {
	products, err := h.productService.ListAllProducts(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list products")
		return
	}

	JSON(w, http.StatusOK, products)
}

// ListOrders godoc
// @Summary List all orders (Admin)
// @Description Retrieve all orders across the system for administrative auditing and fulfillment
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Success 200 {object} domain.SuccessEnvelope{data=[]domain.OrderResponse} "List of all orders"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
// @Router /api/admin/orders [get]
func (h *AdminHandler) ListOrders(w http.ResponseWriter, r *http.Request) {
	orders, err := h.orderService.ListAllOrders(r.Context())
	if err != nil {
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to list orders")
		return
	}

	JSON(w, http.StatusOK, orders)
}

// UpdateOrderStatus godoc
// @Summary Update order fulfillment status
// @Description Update the fulfillment status of an order (e.g. PROCESSING, SHIPPED, DELIVERED, CANCELLED)
// @Tags Admin
// @Accept json
// @Produce json
// @Security BearerAuth
// @Param id path string true "Order ID (UUID)" Format(uuid)
// @Param request body domain.UpdateOrderStatusRequest true "Order status payload"
// @Success 200 {object} domain.SuccessEnvelope{data=domain.OrderResponse} "Order status updated"
// @Failure 400 {object} domain.ErrorEnvelope "Invalid input"
// @Failure 401 {object} domain.ErrorEnvelope "Unauthorized"
// @Failure 403 {object} domain.ErrorEnvelope "Forbidden"
// @Failure 404 {object} domain.ErrorEnvelope "Order not found"
// @Failure 422 {object} domain.ErrorEnvelope "Invalid status transition"
// @Failure 500 {object} domain.ErrorEnvelope "Internal server error"
// @Failure 503 {object} domain.ErrorEnvelope "Service unavailable"
// @Router /api/admin/orders/{id}/status [patch]
func (h *AdminHandler) UpdateOrderStatus(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	orderID, err := uuid.Parse(idStr)
	if err != nil {
		Error(w, http.StatusBadRequest, "INVALID_ID", "Invalid order ID format (UUID expected)")
		return
	}

	var req domain.UpdateOrderStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		Error(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to parse JSON body")
		return
	}

	order, err := h.orderService.UpdateOrderStatus(r.Context(), orderID, req.Status)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			Error(w, http.StatusNotFound, "NOT_FOUND", "Order not found")
			return
		}
		if errors.Is(err, domain.ErrInvalidInput) {
			Error(w, 422, "INVALID_STATUS", err.Error())
			return
		}
		Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", "Failed to update order status")
		return
	}

	JSON(w, http.StatusOK, order)
}

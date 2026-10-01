package tests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
	"github.com/altregubov/warehouse-rest-test-app/internal/middleware"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

const baseURL = "http://localhost:8080"

type clientHelper struct {
	client *http.Client
	token  string
}

func newClient(token string) *clientHelper {
	return &clientHelper{
		client: &http.Client{Timeout: 5 * time.Second},
		token:  token,
	}
}

func (c *clientHelper) request(method, path string, body any) (*http.Response, []byte, error) {
	return c.requestWithHeaders(method, path, body, nil)
}

func (c *clientHelper) requestWithHeaders(method, path string, body any, headers map[string]string) (*http.Response, []byte, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, baseURL+path, bodyReader)
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	return resp, respBytes, err
}

func login(t *testing.T, endpoint, username, password string) (string, int) {
	c := newClient("")
	payload := domain.LoginRequest{Username: username, Password: password}
	resp, body, err := c.request(http.MethodPost, endpoint, payload)
	if err != nil {
		t.Fatalf("Login request failed: %v", err)
	}

	if resp.StatusCode == http.StatusOK {
		var res struct {
			Success bool                 `json:"success"`
			Data    domain.LoginResponse `json:"data"`
		}
		if err := json.Unmarshal(body, &res); err != nil {
			t.Fatalf("Failed to decode login response: %v", err)
		}
		return res.Data.Token, resp.StatusCode
	}

	return "", resp.StatusCode
}

func TestSwaggerDocumentation(t *testing.T) {
	c := newClient("")
	resp, body, err := c.request(http.MethodGet, "/swagger/index.html", nil)
	if err != nil {
		t.Fatalf("Swagger request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 for Swagger UI, got %d", resp.StatusCode)
	}
	if !bytes.Contains(body, []byte("swagger-ui")) {
		t.Errorf("Expected Swagger UI content in response")
	}

	// Swagger JSON spec
	respSpec, _, err := c.request(http.MethodGet, "/swagger/doc.json", nil)
	if err != nil {
		t.Fatalf("Swagger doc.json failed: %v", err)
	}
	if respSpec.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 for doc.json, got %d", respSpec.StatusCode)
	}
}

func TestSegregatedAuthenticationAndRBAC(t *testing.T) {
	// 1. Admin login on /api/admin/login -> Success (200)
	adminToken, status := login(t, "/api/admin/login", "admin", "admin123")
	if status != http.StatusOK || adminToken == "" {
		t.Fatalf("Admin login failed, got status %d", status)
	}

	// 2. Regular user login on /api/admin/login -> 401 Unauthorized (unified response, no oracle)
	c := newClient("")
	respAdminFail, bodyAdminFail, _ := c.request(http.MethodPost, "/api/admin/login", domain.LoginRequest{Username: "userA", Password: "user123"})
	if respAdminFail.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401 when regular user logs into admin endpoint, got %d", respAdminFail.StatusCode)
	}

	// 3. User login on /api/user/login -> Success (200)
	userAToken, status := login(t, "/api/user/login", "userA", "user123")
	if status != http.StatusOK || userAToken == "" {
		t.Fatalf("UserA login failed, got status %d", status)
	}

	// 4. Admin login on /api/user/login -> 401 Unauthorized (unified response, no oracle)
	respUserFail, bodyUserFail, _ := c.request(http.MethodPost, "/api/user/login", domain.LoginRequest{Username: "admin", Password: "admin123"})
	if respUserFail.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401 when admin logs into user endpoint, got %d", respUserFail.StatusCode)
	}

	// 5. Invalid password -> 401 Unauthorized
	respWrongPass, bodyWrongPass, _ := c.request(http.MethodPost, "/api/user/login", domain.LoginRequest{Username: "userA", Password: "wrongpass"})
	if respWrongPass.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401 for wrong password, got %d", respWrongPass.StatusCode)
	}

	// 6. Non-existent user -> 401 Unauthorized
	respNonExistent, bodyNonExistent, _ := c.request(http.MethodPost, "/api/user/login", domain.LoginRequest{Username: "nobody_here", Password: "password123"})
	if respNonExistent.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401 for non-existent user, got %d", respNonExistent.StatusCode)
	}

	// 7. Verify all failure bodies match exactly: code INVALID_CREDENTIALS and message "Invalid username or password"
	for name, b := range map[string][]byte{
		"admin_login_wrong_role": bodyAdminFail,
		"user_login_wrong_role":  bodyUserFail,
		"wrong_password":         bodyWrongPass,
		"non_existent_user":      bodyNonExistent,
	} {
		var errEnv domain.ErrorEnvelope
		if err := json.Unmarshal(b, &errEnv); err != nil {
			t.Errorf("%s: failed to unmarshal error envelope: %v", name, err)
		}
		if errEnv.Error.Code != "INVALID_CREDENTIALS" {
			t.Errorf("%s: expected code INVALID_CREDENTIALS, got %s", name, errEnv.Error.Code)
		}
		if errEnv.Error.Message != "Invalid username or password" {
			t.Errorf("%s: expected message 'Invalid username or password', got %s", name, errEnv.Error.Message)
		}
	}

	// 6. User token attempting admin route -> 403 Forbidden
	userClient := newClient(userAToken)
	resp, _, err := userClient.request(http.MethodPost, "/api/admin/products", domain.CreateProductRequest{
		Category: "laptop", Manufacturer: "Test", Model: "Test", Price: 100, StockQuantity: 1,
	})
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected 403 when user accesses admin route, got %d", resp.StatusCode)
	}

	// 7. Admin token attempting user route -> 403 Forbidden
	adminClient := newClient(adminToken)
	resp, _, err = adminClient.request(http.MethodGet, "/api/user/profile", nil)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected 403 when admin accesses user route, got %d", resp.StatusCode)
	}
}

func TestCatalogFiltering(t *testing.T) {
	// User A: Unrestricted (sees all categories & manufacturers)
	tokenA, _ := login(t, "/api/user/login", "userA", "user123")
	clientA := newClient(tokenA)

	respA, bodyA, err := clientA.request(http.MethodGet, "/api/user/products", nil)
	if err != nil {
		t.Fatalf("User A products failed: %v", err)
	}
	if respA.StatusCode != http.StatusOK {
		t.Fatalf("User A products expected 200, got %d", respA.StatusCode)
	}

	var resA struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyA, &resA)
	if len(resA.Data) < 5 {
		t.Errorf("Expected at least 5 products for unrestricted User A, got %d", len(resA.Data))
	}

	// User B: Restricted to ["laptop"]
	tokenB, _ := login(t, "/api/user/login", "userB", "user123")
	clientB := newClient(tokenB)

	respB, bodyB, err := clientB.request(http.MethodGet, "/api/user/products", nil)
	if err != nil {
		t.Fatalf("User B products failed: %v", err)
	}
	if respB.StatusCode != http.StatusOK {
		t.Fatalf("User B products expected 200, got %d", respB.StatusCode)
	}

	var resB struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyB, &resB)
	for _, p := range resB.Data {
		if p.Category != "laptop" {
			t.Errorf("User B should only see laptops, but saw category %s (%s)", p.Category, p.Model)
		}
	}

	// User B requesting disallowed category ?category=smartphone -> returns empty list []
	respBQuery, bodyBQuery, _ := clientB.request(http.MethodGet, "/api/user/products?category=smartphone", nil)
	if respBQuery.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 with empty list for disallowed category query, got %d", respBQuery.StatusCode)
	}
	var resBQuery struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyBQuery, &resBQuery)
	if len(resBQuery.Data) != 0 {
		t.Errorf("Expected 0 products for disallowed category query, got %d", len(resBQuery.Data))
	}

	// User C: Restricted to ["Apple"]
	tokenC, _ := login(t, "/api/user/login", "userC", "user123")
	clientC := newClient(tokenC)

	respC, bodyC, err := clientC.request(http.MethodGet, "/api/user/products", nil)
	if err != nil {
		t.Fatalf("User C products failed: %v", err)
	}
	if respC.StatusCode != http.StatusOK {
		t.Fatalf("User C products expected 200, got %d", respC.StatusCode)
	}

	var resC struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyC, &resC)
	for _, p := range resC.Data {
		if p.Manufacturer != "Apple" {
			t.Errorf("User C should only see Apple, but saw %s", p.Manufacturer)
		}
	}
}

func TestAtomicOrderPlacement(t *testing.T) {
	tokenA, _ := login(t, "/api/user/login", "userA", "user123")
	clientA := newClient(tokenA)

	// Reset userA balance to guarantee sufficient funds regardless of prior test runs
	adminToken, _ := login(t, "/api/admin/login", "admin", "admin123")
	adminClient := newClient(adminToken)

	// Fetch profile to verify initial balance and get user ID
	respProf, bodyProf, _ := clientA.request(http.MethodGet, "/api/user/profile", nil)
	if respProf.StatusCode != http.StatusOK {
		t.Fatalf("Failed to fetch profile: %d", respProf.StatusCode)
	}
	var profA struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyProf, &profA)

	initialBalance := profA.Data.Balance

	// Fetch available products
	_, bodyProds, _ := clientA.request(http.MethodGet, "/api/user/products?category=laptop", nil)
	var prodList struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyProds, &prodList)
	if len(prodList.Data) == 0 {
		t.Fatalf("No laptops available for test")
	}
	testProduct := prodList.Data[0]

	// 1. Successful purchase
	orderPayload := domain.CreateOrderRequest{
		ProductID: testProduct.ID,
		Quantity:  1,
	}
	respOrder, bodyOrder, err := clientA.request(http.MethodPost, "/api/user/orders", orderPayload)
	if err != nil {
		t.Fatalf("Order request failed: %v", err)
	}
	if respOrder.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 Created for order, got %d: %s", respOrder.StatusCode, string(bodyOrder))
	}

	var orderResp struct {
		Data domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyOrder, &orderResp)
	if orderResp.Data.TotalPrice != testProduct.Price {
		t.Errorf("Expected total price %f, got %f", testProduct.Price, orderResp.Data.TotalPrice)
	}

	// Verify balance was deducted
	_, bodyProfAfter, _ := clientA.request(http.MethodGet, "/api/user/profile", nil)
	var profAfter struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyProfAfter, &profAfter)
	expectedBalance := initialBalance - testProduct.Price
	if fmt.Sprintf("%.2f", profAfter.Data.Balance) != fmt.Sprintf("%.2f", expectedBalance) {
		t.Errorf("Expected balance %.2f, got %.2f", expectedBalance, profAfter.Data.Balance)
	}

	// 2. Disallowed purchase: User B (only laptops) attempts to purchase a smartphone -> 422 FILTER_RESTRICTION
	tokenB, _ := login(t, "/api/user/login", "userB", "user123")
	clientB := newClient(tokenB)

	// Fetch smartphone ID using User A
	_, bodySmartphones, _ := clientA.request(http.MethodGet, "/api/user/products?category=smartphone", nil)
	var smartphoneList struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodySmartphones, &smartphoneList)
	if len(smartphoneList.Data) > 0 {
		smartphone := smartphoneList.Data[0]
		respDisallowed, bodyDisallowed, _ := clientB.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
			ProductID: smartphone.ID,
			Quantity:  1,
		})
		if respDisallowed.StatusCode != 422 {
			t.Errorf("Expected 422 when User B orders smartphone, got %d", respDisallowed.StatusCode)
		}
		var errDisallowed domain.ErrorEnvelope
		_ = json.Unmarshal(bodyDisallowed, &errDisallowed)
		if errDisallowed.Error.Code != "FILTER_RESTRICTION" {
			t.Errorf("Expected code FILTER_RESTRICTION, got %s", errDisallowed.Error.Code)
		}
	}

	// 3. Insufficient balance: user with balance lower than product price -> 422 INSUFFICIENT_FUNDS
	_, _, _ = adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/products/%s/stock", testProduct.ID), domain.UpdateStockRequest{
		StockQuantity: 10,
	})
	lowBalUsername := fmt.Sprintf("lowbal_%d", time.Now().UnixNano())
	_, _, _ = adminClient.request(http.MethodPost, "/api/admin/users", domain.CreateUserRequest{
		Username: lowBalUsername,
		Password: "Password123!",
		Role:     "user",
		Balance:  10.00,
	})
	lowToken, _ := login(t, "/api/user/login", lowBalUsername, "Password123!")
	clientLow := newClient(lowToken)
	respInsufficientFunds, bodyFunds, _ := clientLow.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
		ProductID: testProduct.ID,
		Quantity:  1, // price is > 10.00, stock is available
	})
	if respInsufficientFunds.StatusCode != 422 {
		t.Errorf("Expected 422 for insufficient balance, got %d", respInsufficientFunds.StatusCode)
	}
	var errFunds domain.ErrorEnvelope
	_ = json.Unmarshal(bodyFunds, &errFunds)
	if errFunds.Error.Code != "INSUFFICIENT_FUNDS" {
		t.Errorf("Expected code INSUFFICIENT_FUNDS, got %s", errFunds.Error.Code)
	}

	// 4. Insufficient stock: attempt to purchase quantity exceeding warehouse stock -> 422 INSUFFICIENT_STOCK
	respInsufficientStock, bodyStock, _ := clientA.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
		ProductID: testProduct.ID,
		Quantity:  999999, // exceeds stock
	})
	if respInsufficientStock.StatusCode != 422 {
		t.Errorf("Expected 422 for insufficient stock, got %d", respInsufficientStock.StatusCode)
	}
	var errStock domain.ErrorEnvelope
	_ = json.Unmarshal(bodyStock, &errStock)
	if errStock.Error.Code != "INSUFFICIENT_STOCK" {
		t.Errorf("Expected code INSUFFICIENT_STOCK, got %s", errStock.Error.Code)
	}

	// 5. Schema validation failure: quantity <= 0 -> 400 Bad Request
	respInvalidQty, bodyInvalidQty, _ := clientA.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
		ProductID: testProduct.ID,
		Quantity:  0,
	})
	if respInvalidQty.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for non-positive quantity, got %d", respInvalidQty.StatusCode)
	}
	var errQty domain.ErrorEnvelope
	_ = json.Unmarshal(bodyInvalidQty, &errQty)
	if errQty.Error.Code != "INVALID_INPUT" {
		t.Errorf("Expected code INVALID_INPUT for bad quantity, got %s", errQty.Error.Code)
	}
}

func TestAdminManagementEndpoints(t *testing.T) {
	adminToken, _ := login(t, "/api/admin/login", "admin", "admin123")
	adminClient := newClient(adminToken)

	// 1. Create a new product
	createProdReq := domain.CreateProductRequest{
		Category:      "tablet",
		Manufacturer:  "Apple",
		Model:         "iPad Pro 13 M4",
		Price:         1299.00,
		StockQuantity: 10,
	}
	respCreateProd, bodyCreateProd, err := adminClient.request(http.MethodPost, "/api/admin/products", createProdReq)
	if err != nil {
		t.Fatalf("Create product failed: %v", err)
	}
	if respCreateProd.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 Created for product creation, got %d: %s", respCreateProd.StatusCode, string(bodyCreateProd))
	}
	var newProd struct {
		Data domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyCreateProd, &newProd)

	// 2. Update stock of new product
	respStock, bodyStock, _ := adminClient.request(
		http.MethodPatch,
		fmt.Sprintf("/api/admin/products/%s/stock", newProd.Data.ID),
		domain.UpdateStockRequest{StockQuantity: 25},
	)
	if respStock.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for stock update, got %d: %s", respStock.StatusCode, string(bodyStock))
	}

	// 3. Create a new user
	newUsername := fmt.Sprintf("testuser_%d", time.Now().UnixNano())
	createUserReq := domain.CreateUserRequest{
		Username:             newUsername,
		Password:             "password123",
		Role:                 "user",
		Balance:              100.00,
		AllowedCategories:   []string{"tablet"},
		AllowedManufacturers: []string{"Apple"},
	}
	respCreateUser, bodyCreateUser, _ := adminClient.request(http.MethodPost, "/api/admin/users", createUserReq)
	if respCreateUser.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 for user creation, got %d: %s", respCreateUser.StatusCode, string(bodyCreateUser))
	}
	var newUser struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyCreateUser, &newUser)

	// 4. Verify legacy PATCH /api/admin/users/{id}/balance is rejected (404/405)
	respBalance, _, _ := adminClient.request(
		http.MethodPatch,
		fmt.Sprintf("/api/admin/users/%s/balance", newUser.Data.ID),
		map[string]float64{"amount": 500.00},
	)
	if respBalance.StatusCode != http.StatusNotFound && respBalance.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("Expected 404 or 405 for removed PATCH balance endpoint, got %d", respBalance.StatusCode)
	}

	// 4a. Dedicated TopUpBalance POST /api/admin/users/{id}/balance/top-up (+250)
	respTopUp, bodyTopUp, _ := adminClient.request(
		http.MethodPost,
		fmt.Sprintf("/api/admin/users/%s/balance/top-up", newUser.Data.ID),
		domain.TopUpBalanceRequest{IncrementAmount: 250.00},
	)
	if respTopUp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for dedicated balance top-up, got %d: %s", respTopUp.StatusCode, string(bodyTopUp))
	}
	var topUpResult struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyTopUp, &topUpResult)
	if topUpResult.Data.Balance != 350.00 {
		t.Errorf("Expected balance 350.00, got %f", topUpResult.Data.Balance)
	}

	// 4b. Invalid TopUpBalance (< 0.01) -> 422
	respInvalidTopUp, _, _ := adminClient.request(
		http.MethodPost,
		fmt.Sprintf("/api/admin/users/%s/balance/top-up", newUser.Data.ID),
		domain.TopUpBalanceRequest{IncrementAmount: 0.00},
	)
	if respInvalidTopUp.StatusCode != 422 {
		t.Errorf("Expected 422 for invalid top-up amount, got %d", respInvalidTopUp.StatusCode)
	}

	// 4c. Verify removed PUT /api/admin/users/{id}/balance is rejected (404/405)
	respSetBalance, _, _ := adminClient.request(
		http.MethodPut,
		fmt.Sprintf("/api/admin/users/%s/balance", newUser.Data.ID),
		map[string]float64{"new_balance": 1500.00},
	)
	if respSetBalance.StatusCode != http.StatusNotFound && respSetBalance.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("Expected 404 or 405 for removed PUT balance endpoint, got %d", respSetBalance.StatusCode)
	}

	// 5. Update user filters
	respFilters, bodyFilters, _ := adminClient.request(
		http.MethodPut,
		fmt.Sprintf("/api/admin/users/%s/filters", newUser.Data.ID),
		domain.UpdateFiltersRequest{
			AllowedCategories:   []string{"tablet", "laptop"},
			AllowedManufacturers: []string{"Apple", "Dell"},
		},
	)
	if respFilters.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for filter update, got %d: %s", respFilters.StatusCode, string(bodyFilters))
	}
}

func TestResourceDiscoveryAndOrderLifecycle(t *testing.T) {
	adminToken, _ := login(t, "/api/admin/login", "admin", "admin123")
	adminClient := newClient(adminToken)

	// Create a dedicated user for this lifecycle test
	testUsername := fmt.Sprintf("lifecycle_%d", time.Now().UnixNano())
	respCreate, bodyCreate, _ := adminClient.request(http.MethodPost, "/api/admin/users", domain.CreateUserRequest{
		Username: testUsername,
		Password: "password123",
		Role:     domain.RoleUser,
		Balance:  10000.00,
	})
	if respCreate.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 for test user creation, got %d: %s", respCreate.StatusCode, string(bodyCreate))
	}
	var createdUser struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyCreate, &createdUser)
	targetUserID := createdUser.Data.ID

	userToken, _ := login(t, "/api/user/login", testUsername, "password123")
	userClient := newClient(userToken)

	// 1. Admin lists users
	respUsers, bodyUsers, _ := adminClient.request(http.MethodGet, "/api/admin/users?role=user", nil)
	if respUsers.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for admin user list, got %d: %s", respUsers.StatusCode, string(bodyUsers))
	}
	var usersResult struct {
		Data []domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyUsers, &usersResult)
	if len(usersResult.Data) == 0 {
		t.Fatalf("Expected non-empty users list")
	}

	// 2. Admin gets user by ID
	respUser, bodyUser, _ := adminClient.request(http.MethodGet, fmt.Sprintf("/api/admin/users/%s", targetUserID), nil)
	if respUser.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for admin get user by ID, got %d: %s", respUser.StatusCode, string(bodyUser))
	}

	// 3. Admin lists all products
	respProds, bodyProds, _ := adminClient.request(http.MethodGet, "/api/admin/products", nil)
	if respProds.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for admin products list, got %d: %s", respProds.StatusCode, string(bodyProds))
	}
	var prodsResult struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyProds, &prodsResult)
	if len(prodsResult.Data) == 0 {
		t.Fatalf("Expected non-empty products list")
	}
	var targetProduct domain.Product
	hasStock := false
	for _, p := range prodsResult.Data {
		if p.StockQuantity > 0 {
			targetProduct = p
			hasStock = true
			break
		}
	}
	if !hasStock {
		targetProduct = prodsResult.Data[0]
		_, _, _ = adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/products/%s/stock", targetProduct.ID), domain.UpdateStockRequest{
			StockQuantity: 10,
		})
	}
	firstProduct := targetProduct

	// 4. User places an order
	respOrder, bodyOrder, _ := userClient.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
		ProductID: firstProduct.ID,
		Quantity:  1,
	})
	if respOrder.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 for order creation, got %d: %s", respOrder.StatusCode, string(bodyOrder))
	}
	var orderResult struct {
		Data domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyOrder, &orderResult)
	if orderResult.Data.Status != "PROCESSED" {
		t.Errorf("Expected order status 'PROCESSED', got %s", orderResult.Data.Status)
	}
	orderID := orderResult.Data.OrderID

	// 5. User lists personal orders
	respUserOrders, bodyUserOrders, _ := userClient.request(http.MethodGet, "/api/user/orders", nil)
	if respUserOrders.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for user orders list, got %d: %s", respUserOrders.StatusCode, string(bodyUserOrders))
	}
	var userOrdersResult struct {
		Data []domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyUserOrders, &userOrdersResult)
	found := false
	for _, o := range userOrdersResult.Data {
		if o.OrderID == orderID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected created order %s in user orders list", orderID)
	}

	// 6. User gets order by ID
	respSingleOrder, bodySingleOrder, _ := userClient.request(http.MethodGet, fmt.Sprintf("/api/user/orders/%s", orderID), nil)
	if respSingleOrder.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for user get order by ID, got %d: %s", respSingleOrder.StatusCode, string(bodySingleOrder))
	}

	// 7. Admin lists all orders
	respAdminOrders, bodyAdminOrders, _ := adminClient.request(http.MethodGet, "/api/admin/orders", nil)
	if respAdminOrders.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for admin orders list, got %d: %s", respAdminOrders.StatusCode, string(bodyAdminOrders))
	}

	// 8. Admin updates order status to CANCELLED
	respStatus, bodyStatus, _ := adminClient.request(
		http.MethodPatch,
		fmt.Sprintf("/api/admin/orders/%s/status", orderID),
		domain.UpdateOrderStatusRequest{Status: "CANCELLED"},
	)
	if respStatus.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for status update, got %d: %s", respStatus.StatusCode, string(bodyStatus))
	}
	var updatedStatusResult struct {
		Data domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyStatus, &updatedStatusResult)
	if updatedStatusResult.Data.Status != "CANCELLED" {
		t.Errorf("Expected status 'CANCELLED', got %s", updatedStatusResult.Data.Status)
	}

	// 9. Admin invalid status update -> 422
	respInvalidStatus, _, _ := adminClient.request(
		http.MethodPatch,
		fmt.Sprintf("/api/admin/orders/%s/status", orderID),
		domain.UpdateOrderStatusRequest{Status: "INVALID_STATUS"},
	)
	if respInvalidStatus.StatusCode != 422 {
		t.Errorf("Expected 422 for invalid status transition, got %d", respInvalidStatus.StatusCode)
	}
}

func TestSoftDeleteAndRestrictCascade(t *testing.T) {
	adminToken, _ := login(t, "/api/admin/login", "admin", "admin123")
	adminClient := newClient(adminToken)

	// 1. Create a user
	username := fmt.Sprintf("audit_user_%d", time.Now().UnixNano())
	respCreate, bodyCreate, _ := adminClient.request(http.MethodPost, "/api/admin/users", domain.CreateUserRequest{
		Username: username,
		Password: "password123",
		Role:     domain.RoleUser,
		Balance:  5000.00,
	})
	if respCreate.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to create audit user: %s", string(bodyCreate))
	}
	var createdUser struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyCreate, &createdUser)
	userID := createdUser.Data.ID

	// 2. User places order
	userToken, _ := login(t, "/api/user/login", username, "password123")
	userClient := newClient(userToken)

	_, bodyProds, _ := userClient.request(http.MethodGet, "/api/user/products", nil)
	var prods struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyProds, &prods)
	if len(prods.Data) == 0 {
		t.Fatalf("No products available")
	}

	var targetProd domain.Product
	found := false
	for _, p := range prods.Data {
		if p.StockQuantity > 0 {
			targetProd = p
			found = true
			break
		}
	}
	if !found {
		targetProd = prods.Data[0]
		_, _, _ = adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/products/%s/stock", targetProd.ID), domain.UpdateStockRequest{
			StockQuantity: 10,
		})
	}

	respOrder, bodyOrder, _ := userClient.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
		ProductID: targetProd.ID,
		Quantity:  1,
	})
	if respOrder.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to create order: %s", string(bodyOrder))
	}
	var orderResult struct {
		Data domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyOrder, &orderResult)
	orderID := orderResult.Data.OrderID

	// 3. Verify direct SQL DELETE on users fails with foreign key violation (ON DELETE RESTRICT)
	db, err := sql.Open("postgres", "postgres://postgres:postgres@localhost:5432/warehouse_db?sslmode=disable")
	if err == nil {
		defer db.Close()
		_, err = db.Exec("DELETE FROM users WHERE id = $1", userID)
		if err == nil {
			t.Errorf("Expected foreign key violation error on DELETE FROM users, but got nil")
		}
	}

	// 4. Admin soft-deletes user via API
	respDelete, bodyDelete, _ := adminClient.request(http.MethodDelete, fmt.Sprintf("/api/admin/users/%s", userID), nil)
	if respDelete.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for soft delete, got %d: %s", respDelete.StatusCode, string(bodyDelete))
	}

	// 5. Verify user cannot log in
	_, statusLogin := login(t, "/api/user/login", username, "password123")
	if statusLogin != http.StatusUnauthorized {
		t.Errorf("Expected 401 for deactivated user login, got %d", statusLogin)
	}

	// 6. Verify user is not found in admin GetUser
	respGet, _, _ := adminClient.request(http.MethodGet, fmt.Sprintf("/api/admin/users/%s", userID), nil)
	if respGet.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for soft-deleted user, got %d", respGet.StatusCode)
	}

	// 7. Verify order still exists and is queryable in admin orders list
	respOrders, bodyOrders, _ := adminClient.request(http.MethodGet, "/api/admin/orders", nil)
	if respOrders.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for admin orders list, got %d: %s", respOrders.StatusCode, string(bodyOrders))
	}
	var allOrders struct {
		Data []domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyOrders, &allOrders)
	orderFound := false
	for _, o := range allOrders.Data {
		if o.OrderID == orderID {
			orderFound = true
			break
		}
	}
	if !orderFound {
		t.Errorf("Order %s was lost after user soft deletion; expected to remain for auditability", orderID)
	}
}

func TestConcurrentDeterministicLockOrdering(t *testing.T) {
	adminToken, _ := login(t, "/api/admin/login", "admin", "admin123")
	adminClient := newClient(adminToken)

	// Create test user with generous balance
	username := fmt.Sprintf("lock_test_%d", time.Now().UnixNano())
	respCreate, bodyCreate, _ := adminClient.request(http.MethodPost, "/api/admin/users", domain.CreateUserRequest{
		Username: username,
		Password: "password123",
		Role:     domain.RoleUser,
		Balance:  50000.00,
	})
	if respCreate.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to create user: %s", string(bodyCreate))
	}
	var createdUser struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyCreate, &createdUser)
	userID := createdUser.Data.ID

	// Create a dedicated product with ample stock
	respProd, bodyProd, _ := adminClient.request(http.MethodPost, "/api/admin/products", domain.CreateProductRequest{
		Category:      "hardware",
		Manufacturer:  "TestCorp",
		Model:         fmt.Sprintf("LockModel_%d", time.Now().UnixNano()),
		Price:         10.00,
		StockQuantity: 1000,
	})
	if respProd.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to create product: %s", string(bodyProd))
	}
	var createdProd struct {
		Data domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyProd, &createdProd)
	productID := createdProd.Data.ID

	userToken, _ := login(t, "/api/user/login", username, "password123")

	const concurrency = 15
	var wg sync.WaitGroup
	errCh := make(chan error, concurrency*2)

	// Launch concurrent checkouts and admin balance/stock updates
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c := newClient(userToken)
			resp, body, err := c.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
				ProductID: productID,
				Quantity:  1,
			})
			if err != nil {
				errCh <- fmt.Errorf("checkout %d network error: %w", idx, err)
				return
			}
			// Status should be 201 Created or clean business error, never 500 deadlock
			if resp.StatusCode == http.StatusInternalServerError {
				errCh <- fmt.Errorf("checkout %d hit 500 internal error (potential deadlock): %s", idx, string(body))
			}
		}(i)

		if i%3 == 0 {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				c := newClient(adminToken)
				resp, body, err := c.request(
					http.MethodPost,
					fmt.Sprintf("/api/admin/users/%s/balance/top-up", userID),
					domain.TopUpBalanceRequest{IncrementAmount: 5.00},
				)
				if err != nil {
					errCh <- fmt.Errorf("admin top-up %d network error: %w", idx, err)
					return
				}
				if resp.StatusCode == http.StatusInternalServerError {
					errCh <- fmt.Errorf("admin top-up %d hit 500 (potential deadlock): %s", idx, string(body))
				}
			}(i)
		}
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("Concurrency deadlock error: %v", err)
	}
}

func TestMonetaryPrecisionAndReconciliation(t *testing.T) {
	adminToken, _ := login(t, "/api/admin/login", "admin", "admin123")
	adminClient := newClient(adminToken)

	// Create user with starting balance of exactly $0.30
	username := fmt.Sprintf("money_user_%d", time.Now().UnixNano())
	respCreate, bodyCreate, _ := adminClient.request(http.MethodPost, "/api/admin/users", domain.CreateUserRequest{
		Username: username,
		Password: "password123",
		Role:     domain.RoleUser,
		Balance:  0.30,
	})
	if respCreate.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to create test user: %s", string(bodyCreate))
	}
	var createdUser struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyCreate, &createdUser)
	userID := createdUser.Data.ID

	// Create product priced at $0.10
	respProd, bodyProd, _ := adminClient.request(http.MethodPost, "/api/admin/products", domain.CreateProductRequest{
		Category:      "hardware",
		Manufacturer:  "PrecisionCorp",
		Model:         fmt.Sprintf("MicroPart_%d", time.Now().UnixNano()),
		Price:         0.10,
		StockQuantity: 10,
	})
	if respProd.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to create micro-part product: %s", string(bodyProd))
	}
	var createdProd struct {
		Data domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyProd, &createdProd)
	productID := createdProd.Data.ID

	userToken, _ := login(t, "/api/user/login", username, "password123")
	userClient := newClient(userToken)

	// Buy 3 items ($0.10 * 3 = $0.30)
	// In naive float64, 0.10 * 3 = 0.30000000000000004, causing failure if compared with 0.30
	respOrder, bodyOrder, err := userClient.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
		ProductID: productID,
		Quantity:  3,
	})
	if err != nil {
		t.Fatalf("Order request failed: %v", err)
	}
	if respOrder.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 Created for 3x $0.10 order with $0.30 balance, got %d: %s", respOrder.StatusCode, string(bodyOrder))
	}

	var orderResp struct {
		Data domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyOrder, &orderResp)
	if orderResp.Data.TotalPrice != 0.30 {
		t.Errorf("Expected TotalPrice 0.30, got %.4f", orderResp.Data.TotalPrice)
	}
	if orderResp.Data.RemainingBalance != 0.00 {
		t.Errorf("Expected RemainingBalance 0.00, got %.4f", orderResp.Data.RemainingBalance)
	}

	// Verify profile balance in database is exactly 0.00 without floating point drift
	_, bodyProf, _ := userClient.request(http.MethodGet, "/api/user/profile", nil)
	var prof struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyProf, &prof)
	if prof.Data.Balance != 0.00 {
		t.Errorf("Expected user balance 0.00, got %.4f", prof.Data.Balance)
	}

	// Fractional top-ups: add 0.10 then 0.20 -> exactly 0.30
	adminClient.request(http.MethodPost, fmt.Sprintf("/api/admin/users/%s/balance/top-up", userID), domain.TopUpBalanceRequest{
		IncrementAmount: 0.10,
	})
	respTopUp, bodyTopUp, _ := adminClient.request(http.MethodPost, fmt.Sprintf("/api/admin/users/%s/balance/top-up", userID), domain.TopUpBalanceRequest{
		IncrementAmount: 0.20,
	})
	if respTopUp.StatusCode != http.StatusOK {
		t.Fatalf("Failed top-up: %s", string(bodyTopUp))
	}
	var topUpRes struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyTopUp, &topUpRes)
	if topUpRes.Data.Balance != 0.30 {
		t.Errorf("Expected balance 0.30 after 0.10 + 0.20 additions, got %.4f", topUpRes.Data.Balance)
	}
}

func TestSchemaValidationAndOpenAPIContracts(t *testing.T) {
	tokenAdmin, _ := login(t, "/api/admin/login", "admin", "admin123")
	adminClient := newClient(tokenAdmin)

	tokenUser, _ := login(t, "/api/user/login", "userA", "user123")
	userClient := newClient(tokenUser)

	// 1. Path variable UUID validation: invalid UUID rejected as 400
	respInvalidID, _, _ := adminClient.request(http.MethodGet, "/api/admin/users/not-a-valid-uuid", nil)
	if respInvalidID.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid UUID in path, got %d", respInvalidID.StatusCode)
	}

	// 2. Role enum validation: invalid role rejected as 400
	respInvalidRole, _, _ := adminClient.request(http.MethodPost, "/api/admin/users", map[string]any{
		"username": "super_user_1",
		"password": "validpassword",
		"role":     "superadmin",
		"balance":  100.0,
	})
	if respInvalidRole.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid role enum, got %d", respInvalidRole.StatusCode)
	}

	// 3. String length constraints: empty password rejected as 400
	respEmptyPass, _, _ := adminClient.request(http.MethodPost, "/api/admin/users", map[string]any{
		"username": "valid_user_empty_pass",
		"password": "",
		"role":     "user",
		"balance":  100.0,
	})
	if respEmptyPass.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for empty password, got %d", respEmptyPass.StatusCode)
	}

	// 4. Numeric boundary violations: negative balance rejected as 400
	respNegBal, _, _ := adminClient.request(http.MethodPost, "/api/admin/users", map[string]any{
		"username": "valid_user_neg_bal",
		"password": "validpassword",
		"role":     "user",
		"balance":  -50.0,
	})
	if respNegBal.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for negative balance, got %d", respNegBal.StatusCode)
	}

	// 5. Numeric boundary violations: negative price or stock in product creation
	respNegPrice, _, _ := adminClient.request(http.MethodPost, "/api/admin/products", map[string]any{
		"category":       "electronics",
		"manufacturer":   "Sony",
		"model":          "WH-1000XM5",
		"price":          -10.0,
		"stock_quantity": 5,
	})
	if respNegPrice.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for negative product price, got %d", respNegPrice.StatusCode)
	}

	// 6. Numeric boundary: quantity < 1 rejected as 400
	respZeroQty, _, _ := userClient.request(http.MethodPost, "/api/user/orders", map[string]any{
		"product_id": uuid.New().String(),
		"quantity":   0,
	})
	if respZeroQty.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for order quantity 0, got %d", respZeroQty.StatusCode)
	}

	// 7. OpenAPI specification audit: verify swagger schema metadata
	respDoc, bodyDoc, _ := adminClient.request(http.MethodGet, "/swagger/doc.json", nil)
	if respDoc.StatusCode != http.StatusOK {
		t.Fatalf("Failed to fetch /swagger/doc.json: %d", respDoc.StatusCode)
	}

	var swaggerDoc struct {
		Definitions map[string]struct {
			Required   []string               `json:"required"`
			Properties map[string]interface{} `json:"properties"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal(bodyDoc, &swaggerDoc); err != nil {
		t.Fatalf("Failed to parse swagger doc: %v", err)
	}

	checkDefinitions := []string{
		"CreateUserRequest",
		"CreateProductRequest",
		"CreateOrderRequest",
		"LoginRequest",
		"UpdateStockRequest",
	}

	for _, defName := range checkDefinitions {
		def, ok := swaggerDoc.Definitions[defName]
		if !ok {
			t.Errorf("Definition %s not found in Swagger docs", defName)
			continue
		}
		if len(def.Required) == 0 {
			t.Errorf("Definition %s has empty 'required' array", defName)
		}
	}
}

func TestHistoricalOrderSnapshotImmutability(t *testing.T) {
	db, err := sql.Open("postgres", "postgres://postgres:postgres@localhost:5432/warehouse_db?sslmode=disable")
	if err != nil {
		t.Fatalf("Failed to connect to db: %v", err)
	}
	defer db.Close()

	tokenAdmin, _ := login(t, "/api/admin/login", "admin", "admin123")
	adminClient := newClient(tokenAdmin)

	tokenUser, _ := login(t, "/api/user/login", "userA", "user123")
	userClient := newClient(tokenUser)

	// Create a brand new distinct product
	prodReq := domain.CreateProductRequest{
		Category:      "laptop",
		Manufacturer:  "HistoricalCorp",
		Model:         "RetroBook 2024 Original",
		Price:         1200.00,
		StockQuantity: 10,
	}
	respProd, bodyProd, _ := adminClient.request(http.MethodPost, "/api/admin/products", prodReq)
	if respProd.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to create product: %s", string(bodyProd))
	}
	var createdProd struct {
		Data domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyProd, &createdProd)
	prodID := createdProd.Data.ID

	// Top up userA balance
	_, bodyProf, _ := userClient.request(http.MethodGet, "/api/user/profile", nil)
	var profA struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyProf, &profA)
	_, _, _ = adminClient.request(http.MethodPost, fmt.Sprintf("/api/admin/users/%s/balance/top-up", profA.Data.ID), domain.TopUpBalanceRequest{
		IncrementAmount: 10000.00,
	})

	// Place order for 2 units
	respOrder, bodyOrder, _ := userClient.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
		ProductID: prodID,
		Quantity:  2,
	})
	if respOrder.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to place order: %s", string(bodyOrder))
	}
	var placedOrder struct {
		Data domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyOrder, &placedOrder)
	orderID := placedOrder.Data.OrderID

	if placedOrder.Data.ProductModel != "RetroBook 2024 Original" {
		t.Errorf("Expected model RetroBook 2024 Original, got %s", placedOrder.Data.ProductModel)
	}
	if placedOrder.Data.UnitPrice != 1200.00 {
		t.Errorf("Expected unit price 1200.00, got %f", placedOrder.Data.UnitPrice)
	}

	// Directly mutate the product's model and price in the database
	_, err = db.Exec("UPDATE products SET model = 'RetroBook 2026 Altered', price = 3500.00 WHERE id = $1", prodID)
	if err != nil {
		t.Fatalf("Failed to update product in database: %v", err)
	}

	// 1. Verify GET /api/user/orders/{id} returns historical snapshots
	respGet, bodyGet, _ := userClient.request(http.MethodGet, fmt.Sprintf("/api/user/orders/%s", orderID), nil)
	if respGet.StatusCode != http.StatusOK {
		t.Fatalf("Failed to get order: %s", string(bodyGet))
	}
	var getOrder struct {
		Data domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyGet, &getOrder)
	if getOrder.Data.ProductModel != "RetroBook 2024 Original" {
		t.Errorf("GET /api/user/orders/{id}: expected model RetroBook 2024 Original, got %s", getOrder.Data.ProductModel)
	}
	if getOrder.Data.UnitPrice != 1200.00 {
		t.Errorf("GET /api/user/orders/{id}: expected unit price 1200.00, got %f", getOrder.Data.UnitPrice)
	}

	// 2. Verify GET /api/user/orders returns historical snapshots
	_, bodyList, _ := userClient.request(http.MethodGet, "/api/user/orders", nil)
	var listOrders struct {
		Data []domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyList, &listOrders)
	var foundUserOrder *domain.OrderResponse
	for _, o := range listOrders.Data {
		if o.OrderID == orderID {
			foundUserOrder = &o
			break
		}
	}
	if foundUserOrder == nil {
		t.Fatalf("Order not found in user orders list")
	}
	if foundUserOrder.ProductModel != "RetroBook 2024 Original" || foundUserOrder.UnitPrice != 1200.00 {
		t.Errorf("User orders list snapshot mismatch: got model %s, price %f", foundUserOrder.ProductModel, foundUserOrder.UnitPrice)
	}

	// 3. Verify GET /api/admin/orders returns historical snapshots
	_, bodyAdminList, _ := adminClient.request(http.MethodGet, "/api/admin/orders", nil)
	var adminOrders struct {
		Data []domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyAdminList, &adminOrders)
	var foundAdminOrder *domain.OrderResponse
	for _, o := range adminOrders.Data {
		if o.OrderID == orderID {
			foundAdminOrder = &o
			break
		}
	}
	if foundAdminOrder == nil {
		t.Fatalf("Order not found in admin orders list")
	}
	if foundAdminOrder.ProductModel != "RetroBook 2024 Original" || foundAdminOrder.UnitPrice != 1200.00 {
		t.Errorf("Admin orders list snapshot mismatch: got model %s, price %f", foundAdminOrder.ProductModel, foundAdminOrder.UnitPrice)
	}

	// 4. Verify PATCH /api/admin/orders/{id}/status preserves historical snapshots
	respStatus, bodyStatus, _ := adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/orders/%s/status", orderID), domain.UpdateOrderStatusRequest{
		Status: "CANCELLED",
	})
	if respStatus.StatusCode != http.StatusOK {
		t.Fatalf("Failed to update order status: %s", string(bodyStatus))
	}
	var statusOrder struct {
		Data domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyStatus, &statusOrder)
	if statusOrder.Data.ProductModel != "RetroBook 2024 Original" || statusOrder.Data.UnitPrice != 1200.00 {
		t.Errorf("Status update snapshot mismatch: got model %s, price %f", statusOrder.Data.ProductModel, statusOrder.Data.UnitPrice)
	}
}

func TestCatalogFilterCaseSensitivityAndAccessDenial(t *testing.T) {
	tokenAdmin, _ := login(t, "/api/admin/login", "admin", "admin123")
	adminClient := newClient(tokenAdmin)

	testUsername := fmt.Sprintf("case_test_user_%d", time.Now().UnixNano())
	testPassword := "password123"

	// Create a test user with mixed-casing filters: lowercase "apple" and uppercase "LAPTOP"
	createReq := domain.CreateUserRequest{
		Username:             testUsername,
		Password:             testPassword,
		Role:                 "user",
		Balance:              5000.00,
		AllowedCategories:   []string{"LAPTOP"},
		AllowedManufacturers: []string{"apple"},
	}
	respCreate, bodyCreate, _ := adminClient.request(http.MethodPost, "/api/admin/users", createReq)
	if respCreate.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to create user: %s", string(bodyCreate))
	}
	var createdUser struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyCreate, &createdUser)
	userID := createdUser.Data.ID

	tokenCaseUser, _ := login(t, "/api/user/login", testUsername, testPassword)
	caseClient := newClient(tokenCaseUser)

	// 1. Verify case-insensitive catalog filtering:
	// Allowed: "apple" (db has "Apple"), "LAPTOP" (db has "laptop").
	// Must return Apple laptop (MacBook Pro), but NOT Dell laptop or Apple smartphone.
	respProds, bodyProds, _ := caseClient.request(http.MethodGet, "/api/user/products", nil)
	if respProds.StatusCode != http.StatusOK {
		t.Fatalf("Failed to get products: %s", string(bodyProds))
	}
	var prodList struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyProds, &prodList)
	if len(prodList.Data) == 0 {
		t.Fatalf("Expected case-insensitive matching to return Apple laptops, got 0 items")
	}
	for _, p := range prodList.Data {
		if !strings.EqualFold(p.Category, "laptop") || !strings.EqualFold(p.Manufacturer, "apple") {
			t.Errorf("Product %s (%s, %s) does not match filters", p.Model, p.Category, p.Manufacturer)
		}
	}

	// 2. Query with uppercase query param ?category=LAPTOP and lowercase ?category=laptop
	_, bodyCatUpper, _ := caseClient.request(http.MethodGet, "/api/user/products?category=LAPTOP", nil)
	var listUpper struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyCatUpper, &listUpper)
	if len(listUpper.Data) == 0 {
		t.Errorf("Expected products for ?category=LAPTOP, got 0")
	}

	// 3. User with filtered access places order for allowed Apple laptop -> 201 Created
	// Ensure stock is available
	appleLaptop := prodList.Data[0]
	_, _, _ = adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/products/%s/stock", appleLaptop.ID), domain.UpdateStockRequest{
		StockQuantity: 10,
	})
	respOrder, bodyOrder, _ := caseClient.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
		ProductID: appleLaptop.ID,
		Quantity:  1,
	})
	if respOrder.StatusCode != http.StatusCreated {
		t.Errorf("Expected 201 for allowed product order, got %d: %s", respOrder.StatusCode, string(bodyOrder))
	}

	// 4. Configure user with filters restricted to a different category (e.g. "smartphone")
	respRestricted, bodyRestricted, _ := adminClient.request(
		http.MethodPut,
		fmt.Sprintf("/api/admin/users/%s/filters", userID),
		domain.UpdateFiltersRequest{
			AllowedCategories:   []string{"smartphone"},
			AllowedManufacturers: []string{"Samsung"},
		},
	)
	if respRestricted.StatusCode != http.StatusOK {
		t.Fatalf("Failed to update filters: %s", string(bodyRestricted))
	}
	var restrictedSummary struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyRestricted, &restrictedSummary)
	if len(restrictedSummary.Data.AllowedCategories) != 1 || restrictedSummary.Data.AllowedCategories[0] != "smartphone" {
		t.Errorf("Expected allowed_categories=['smartphone'], got %v", restrictedSummary.Data.AllowedCategories)
	}

	// Verify catalog exploration for laptops returns 0 products
	_, bodyCatQuery, _ := caseClient.request(http.MethodGet, "/api/user/products?category=laptop", nil)
	var catProds struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyCatQuery, &catProds)
	if len(catProds.Data) != 0 {
		t.Errorf("Expected 0 products for disallowed category query, got %d", len(catProds.Data))
	}

	// Verify order attempt for Apple laptop is rejected with 422 FILTER_RESTRICTION
	respDeniedOrder, bodyDeniedOrder, _ := caseClient.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
		ProductID: appleLaptop.ID,
		Quantity:  1,
	})
	if respDeniedOrder.StatusCode != 422 {
		t.Errorf("Expected 422 FILTER_RESTRICTION for disallowed product order, got %d: %s", respDeniedOrder.StatusCode, string(bodyDeniedOrder))
	}
	var errDenied domain.ErrorEnvelope
	_ = json.Unmarshal(bodyDeniedOrder, &errDenied)
	if errDenied.Error.Code != "FILTER_RESTRICTION" {
		t.Errorf("Expected code FILTER_RESTRICTION, got %s", errDenied.Error.Code)
	}

	// 5. Restore user to empty filters -> full access to all entries by default
	respOpenFilters, bodyOpenFilters, _ := adminClient.request(
		http.MethodPut,
		fmt.Sprintf("/api/admin/users/%s/filters", userID),
		domain.UpdateFiltersRequest{
			AllowedCategories:   []string{},
			AllowedManufacturers: []string{},
		},
	)
	if respOpenFilters.StatusCode != http.StatusOK {
		t.Fatalf("Failed to restore open filters: %s", string(bodyOpenFilters))
	}
	_, bodyFullList, _ := caseClient.request(http.MethodGet, "/api/user/products", nil)
	var fullProds struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyFullList, &fullProds)
	if len(fullProds.Data) < 3 {
		t.Errorf("Expected full catalog for user with empty filters, got %d products", len(fullProds.Data))
	}

	// Verify user can now order the Apple laptop without filter restriction
	respAllowedOrder, bodyAllowedOrder, _ := caseClient.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
		ProductID: appleLaptop.ID,
		Quantity:  1,
	})
	if respAllowedOrder.StatusCode != http.StatusCreated {
		t.Errorf("Expected 201 Created for open access order, got %d: %s", respAllowedOrder.StatusCode, string(bodyAllowedOrder))
	}
}

func TestStatusCodesAlignment404And409(t *testing.T) {
	adminToken, _ := login(t, "/api/admin/login", "admin", "admin123")
	adminClient := newClient(adminToken)

	randomUUID := uuid.New()

	// 1. POST /api/admin/users duplicate username -> 409 Conflict
	username := fmt.Sprintf("status_user_%d", time.Now().UnixNano())
	respCreate1, _, _ := adminClient.request(http.MethodPost, "/api/admin/users", domain.CreateUserRequest{
		Username: username,
		Password: "password123",
		Role:     domain.RoleUser,
		Balance:  100.0,
	})
	if respCreate1.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to create first user: %d", respCreate1.StatusCode)
	}

	respCreateDup, bodyDup, _ := adminClient.request(http.MethodPost, "/api/admin/users", domain.CreateUserRequest{
		Username: username,
		Password: "password123",
		Role:     domain.RoleUser,
		Balance:  100.0,
	})
	if respCreateDup.StatusCode != http.StatusConflict {
		t.Errorf("Expected 409 Conflict for duplicate username, got %d", respCreateDup.StatusCode)
	}
	var errDup domain.ErrorEnvelope
	_ = json.Unmarshal(bodyDup, &errDup)
	if errDup.Error.Code != "USERNAME_TAKEN" {
		t.Errorf("Expected USERNAME_TAKEN error code, got %s", errDup.Error.Code)
	}

	// 2. 404 on balance endpoints for non-existent user
	respTopUp, bodyTopUp, _ := adminClient.request(
		http.MethodPost,
		fmt.Sprintf("/api/admin/users/%s/balance/top-up", randomUUID),
		domain.TopUpBalanceRequest{IncrementAmount: 50.0},
	)
	if respTopUp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for top-up of non-existent user, got %d: %s", respTopUp.StatusCode, string(bodyTopUp))
	}

	// 3. 404 on filters endpoint for non-existent user
	respFilters, bodyFilters, _ := adminClient.request(
		http.MethodPut,
		fmt.Sprintf("/api/admin/users/%s/filters", randomUUID),
		domain.UpdateFiltersRequest{AllowedCategories: []string{"laptop"}},
	)
	if respFilters.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for filters update on non-existent user, got %d: %s", respFilters.StatusCode, string(bodyFilters))
	}

	// 4. 404 on get user for non-existent user
	respGetUser, bodyGetUser, _ := adminClient.request(
		http.MethodGet,
		fmt.Sprintf("/api/admin/users/%s", randomUUID),
		nil,
	)
	if respGetUser.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for get non-existent user, got %d: %s", respGetUser.StatusCode, string(bodyGetUser))
	}

	// 5. 404 on delete user for non-existent user
	respDelUser, bodyDelUser, _ := adminClient.request(
		http.MethodDelete,
		fmt.Sprintf("/api/admin/users/%s", randomUUID),
		nil,
	)
	if respDelUser.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for delete non-existent user, got %d: %s", respDelUser.StatusCode, string(bodyDelUser))
	}

	// 6. 404 on stock update for non-existent product
	respStock, bodyStock, _ := adminClient.request(
		http.MethodPatch,
		fmt.Sprintf("/api/admin/products/%s/stock", randomUUID),
		domain.UpdateStockRequest{StockQuantity: 10},
	)
	if respStock.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for stock update on non-existent product, got %d: %s", respStock.StatusCode, string(bodyStock))
	}

	// 7. 404 on order status update for non-existent order
	respOrderStatus, bodyOrderStatus, _ := adminClient.request(
		http.MethodPatch,
		fmt.Sprintf("/api/admin/orders/%s/status", randomUUID),
		domain.UpdateOrderStatusRequest{Status: "CANCELLED"},
	)
	if respOrderStatus.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for status update on non-existent order, got %d: %s", respOrderStatus.StatusCode, string(bodyOrderStatus))
	}

	// 8. User ordering non-existent product SKU -> 404
	userToken, _ := login(t, "/api/user/login", username, "password123")
	userClient := newClient(userToken)
	respOrderMissing, bodyOrderMissing, _ := userClient.request(
		http.MethodPost,
		"/api/user/orders",
		domain.CreateOrderRequest{
			ProductID: randomUUID,
			Quantity:  1,
		},
	)
	if respOrderMissing.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for ordering non-existent product, got %d: %s", respOrderMissing.StatusCode, string(bodyOrderMissing))
	}

	// 9. User fetching non-existent order -> 404
	respGetOrderMissing, bodyGetOrderMissing, _ := userClient.request(
		http.MethodGet,
		fmt.Sprintf("/api/user/orders/%s", randomUUID),
		nil,
	)
	if respGetOrderMissing.StatusCode != http.StatusNotFound {
		t.Errorf("Expected 404 for fetching non-existent order, got %d: %s", respGetOrderMissing.StatusCode, string(bodyGetOrderMissing))
	}
}

func TestCatalogPaginationSortingAndFiltering(t *testing.T) {
	// Login userA (unrestricted, ALL access)
	tokenA, _ := login(t, "/api/user/login", "userA", "user123")
	clientA := newClient(tokenA)

	// 1. Default pagination & response envelope metadata
	respDef, bodyDef, err := clientA.request(http.MethodGet, "/api/user/products", nil)
	if err != nil || respDef.StatusCode != http.StatusOK {
		t.Fatalf("Failed to fetch default products: %d", respDef.StatusCode)
	}

	var envDef struct {
		Success    bool                       `json:"success"`
		Data       []domain.Product           `json:"data"`
		Pagination *domain.PaginationMetadata `json:"pagination"`
		TotalCount int                        `json:"total_count"`
		Page       int                        `json:"page"`
		PageSize   int                        `json:"page_size"`
		TotalPages int                        `json:"total_pages"`
	}
	if err := json.Unmarshal(bodyDef, &envDef); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if envDef.Pagination == nil {
		t.Fatalf("Expected pagination metadata in envelope")
	}
	if envDef.Pagination.Page != 1 || envDef.Pagination.PageSize != 20 {
		t.Errorf("Expected page 1 and page_size 20, got page %d, size %d", envDef.Pagination.Page, envDef.Pagination.PageSize)
	}
	if envDef.Pagination.TotalCount < 5 {
		t.Errorf("Expected at least 5 products, got %d", envDef.Pagination.TotalCount)
	}

	// 2. Custom page and page_size pagination (page=1&page_size=2 and page=2&page_size=2)
	_, bodyP1, _ := clientA.request(http.MethodGet, "/api/user/products?page=1&page_size=2&sort_by=price&order=asc", nil)
	var envP1 struct {
		Data       []domain.Product          `json:"data"`
		Pagination domain.PaginationMetadata `json:"pagination"`
	}
	_ = json.Unmarshal(bodyP1, &envP1)
	if len(envP1.Data) != 2 {
		t.Fatalf("Expected 2 items on page 1, got %d", len(envP1.Data))
	}
	if envP1.Pagination.Page != 1 || envP1.Pagination.PageSize != 2 {
		t.Errorf("Unexpected pagination metadata on page 1: %+v", envP1.Pagination)
	}

	_, bodyP2, _ := clientA.request(http.MethodGet, "/api/user/products?page=2&page_size=2&sort_by=price&order=asc", nil)
	var envP2 struct {
		Data       []domain.Product          `json:"data"`
		Pagination domain.PaginationMetadata `json:"pagination"`
	}
	_ = json.Unmarshal(bodyP2, &envP2)
	if len(envP2.Data) != 2 {
		t.Fatalf("Expected 2 items on page 2, got %d", len(envP2.Data))
	}
	if envP2.Pagination.Page != 2 || envP2.Pagination.PageSize != 2 {
		t.Errorf("Unexpected pagination metadata on page 2: %+v", envP2.Pagination)
	}

	// Items on page 1 and page 2 must be disjoint
	if envP1.Data[0].ID == envP2.Data[0].ID || envP1.Data[1].ID == envP2.Data[1].ID {
		t.Errorf("Overlapping items between page 1 and page 2")
	}

	// 3. Sorting: Price ASC vs Price DESC
	_, bodyPriceAsc, _ := clientA.request(http.MethodGet, "/api/user/products?sort_by=price&order=asc&page_size=100", nil)
	var envPriceAsc struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyPriceAsc, &envPriceAsc)
	for i := 1; i < len(envPriceAsc.Data); i++ {
		if envPriceAsc.Data[i].Price < envPriceAsc.Data[i-1].Price {
			t.Errorf("Products not in price ASC order: %f < %f", envPriceAsc.Data[i].Price, envPriceAsc.Data[i-1].Price)
		}
	}

	_, bodyPriceDesc, _ := clientA.request(http.MethodGet, "/api/user/products?sort_by=price&order=desc&page_size=100", nil)
	var envPriceDesc struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyPriceDesc, &envPriceDesc)
	for i := 1; i < len(envPriceDesc.Data); i++ {
		if envPriceDesc.Data[i].Price > envPriceDesc.Data[i-1].Price {
			t.Errorf("Products not in price DESC order: %f > %f", envPriceDesc.Data[i].Price, envPriceDesc.Data[i-1].Price)
		}
	}

	// 4. Sorting: Model ASC
	_, bodyModelAsc, _ := clientA.request(http.MethodGet, "/api/user/products?sort_by=model&order=asc&page_size=100", nil)
	var envModelAsc struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyModelAsc, &envModelAsc)
	for i := 1; i < len(envModelAsc.Data); i++ {
		if envModelAsc.Data[i].Model < envModelAsc.Data[i-1].Model {
			t.Errorf("Products not in model ASC order: %s < %s", envModelAsc.Data[i].Model, envModelAsc.Data[i-1].Model)
		}
	}

	// 5. Manufacturer filtering
	_, bodyApple, _ := clientA.request(http.MethodGet, "/api/user/products?manufacturer=Apple", nil)
	var envApple struct {
		Data       []domain.Product          `json:"data"`
		Pagination domain.PaginationMetadata `json:"pagination"`
	}
	_ = json.Unmarshal(bodyApple, &envApple)
	if len(envApple.Data) == 0 {
		t.Fatalf("Expected Apple products, got 0")
	}
	for _, p := range envApple.Data {
		if !strings.EqualFold(p.Manufacturer, "Apple") {
			t.Errorf("Expected manufacturer Apple, got %s", p.Manufacturer)
		}
	}

	// 6. Combined category and manufacturer filtering
	_, bodyCombined, _ := clientA.request(http.MethodGet, "/api/user/products?category=laptop&manufacturer=Apple", nil)
	var envCombined struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyCombined, &envCombined)
	for _, p := range envCombined.Data {
		if !strings.EqualFold(p.Category, "laptop") || !strings.EqualFold(p.Manufacturer, "Apple") {
			t.Errorf("Expected Apple laptop, got %s %s", p.Manufacturer, p.Category)
		}
	}

	// 7. Validation boundary errors (400 Bad Request)
	respInvPage, _, _ := clientA.request(http.MethodGet, "/api/user/products?page=0", nil)
	if respInvPage.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for page=0, got %d", respInvPage.StatusCode)
	}

	respInvSize0, _, _ := clientA.request(http.MethodGet, "/api/user/products?page_size=0", nil)
	if respInvSize0.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for page_size=0, got %d", respInvSize0.StatusCode)
	}

	respInvSize101, _, _ := clientA.request(http.MethodGet, "/api/user/products?page_size=101", nil)
	if respInvSize101.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for page_size=101, got %d", respInvSize101.StatusCode)
	}

	respInvSort, _, _ := clientA.request(http.MethodGet, "/api/user/products?sort_by=unknown", nil)
	if respInvSort.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid sort_by, got %d", respInvSort.StatusCode)
	}

	respInvOrder, _, _ := clientA.request(http.MethodGet, "/api/user/products?order=diagonal", nil)
	if respInvOrder.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid order, got %d", respInvOrder.StatusCode)
	}
}

// TestDistributedTracingAndStructuredErrors tests Issue #17:
// 1. Automatic generation and echoing of X-Request-ID in header and JSON envelope requestId
// 2. Custom X-Request-ID preservation
// 3. Typed ErrorDetails.details containing FieldViolation objects ({field, issue})
func TestDistributedTracingAndStructuredErrors(t *testing.T) {
	client := newClient("")

	// 1. Without client X-Request-ID: API generates UUID, returns in header and body
	resp, body, err := client.request(http.MethodPost, "/api/user/login", map[string]string{
		"username": "user1",
		"password": "wrongpassword",
	})
	if err != nil {
		t.Fatalf("Login request failed: %v", err)
	}

	headerReqID := resp.Header.Get("X-Request-ID")
	if headerReqID == "" {
		t.Errorf("Expected X-Request-ID header in response, got empty")
	}
	if _, err := uuid.Parse(headerReqID); err != nil {
		t.Errorf("Expected valid UUID in X-Request-ID header, got %s: %v", headerReqID, err)
	}

	var errEnv domain.ErrorEnvelope
	if err := json.Unmarshal(body, &errEnv); err != nil {
		t.Fatalf("Failed to parse error envelope: %v", err)
	}
	if errEnv.RequestID != headerReqID {
		t.Errorf("Expected envelope requestId %s to match header %s", errEnv.RequestID, headerReqID)
	}

	// 2. With custom client X-Request-ID
	customReqID := "custom-trace-uuid-12345"
	respCustom, bodyCustom, err := client.requestWithHeaders(http.MethodPost, "/api/user/login", map[string]string{
		"username": "user1",
		"password": "wrongpassword",
	}, map[string]string{
		"X-Request-ID": customReqID,
	})
	if err != nil {
		t.Fatalf("Custom request failed: %v", err)
	}

	if respCustom.Header.Get("X-Request-ID") != customReqID {
		t.Errorf("Expected custom X-Request-ID %s, got %s", customReqID, respCustom.Header.Get("X-Request-ID"))
	}
	var errEnvCustom domain.ErrorEnvelope
	if err := json.Unmarshal(bodyCustom, &errEnvCustom); err != nil {
		t.Fatalf("Failed to parse custom error envelope: %v", err)
	}
	if errEnvCustom.RequestID != customReqID {
		t.Errorf("Expected envelope requestId %s to match custom %s", errEnvCustom.RequestID, customReqID)
	}

	// 3. Admin login and verify structured FieldViolations in 422 response
	adminToken, code := login(t, "/api/admin/login", "admin", "admin123")
	if code != http.StatusOK {
		t.Fatalf("Admin login failed with status %d", code)
	}
	adminClient := newClient(adminToken)

	userUUID := uuid.New().String()
	resp422, body422, err := adminClient.request(http.MethodPost, fmt.Sprintf("/api/admin/users/%s/balance/top-up", userUUID), map[string]float64{
		"increment_amount": 0.005,
	})
	if err != nil {
		t.Fatalf("Top up request failed: %v", err)
	}
	if resp422.StatusCode != 422 {
		t.Fatalf("Expected 422 Unprocessable Entity, got %d. Body: %s", resp422.StatusCode, string(body422))
	}

	var valErrEnv domain.ErrorEnvelope
	if err := json.Unmarshal(body422, &valErrEnv); err != nil {
		t.Fatalf("Failed to unmarshal 422 response: %v", err)
	}
	if len(valErrEnv.Error.Details) == 0 {
		t.Errorf("Expected structured details in 422 error, got empty")
	} else {
		violation := valErrEnv.Error.Details[0]
		if violation.Field != "increment_amount" {
			t.Errorf("Expected violation field 'increment_amount', got '%s'", violation.Field)
		}
	}
}

// TestCleanOpenAPIArrayExamples tests Issue #18:
// Verifies that Swagger/OpenAPI specification contains clean native JSON arrays for array field examples,
// without stringified brackets or escaped quotes.
func TestCleanOpenAPIArrayExamples(t *testing.T) {
	client := newClient("")
	resp, body, err := client.request(http.MethodGet, "/swagger/doc.json", nil)
	if err != nil {
		t.Fatalf("Failed to fetch Swagger JSON: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK from Swagger doc endpoint, got %d", resp.StatusCode)
	}

	var spec struct {
		Definitions map[string]struct {
			Properties map[string]struct {
				Type    string `json:"type"`
				Example any    `json:"example"`
			} `json:"properties"`
		} `json:"definitions"`
	}

	if err := json.Unmarshal(body, &spec); err != nil {
		t.Fatalf("Failed to parse Swagger JSON spec: %v", err)
	}

	toStringSlice := func(v any) []string {
		if v == nil {
			return nil
		}
		if s, ok := v.([]any); ok {
			res := make([]string, len(s))
			for i, elem := range s {
				res[i] = fmt.Sprintf("%v", elem)
			}
			return res
		}
		if s, ok := v.([]string); ok {
			return s
		}
		return nil
	}

	// 1. CreateUserRequest allowed_categories and allowed_manufacturers
	createReq, ok := spec.Definitions["CreateUserRequest"]
	if !ok {
		t.Fatalf("CreateUserRequest definition not found in swagger spec")
	}

	catProp, ok := createReq.Properties["allowed_categories"]
	catEx := toStringSlice(catProp.Example)
	if !ok || len(catEx) == 0 {
		t.Fatalf("allowed_categories property or example missing in CreateUserRequest")
	}
	if catEx[0] != "laptop" {
		t.Errorf("Expected CreateUserRequest.allowed_categories example ['laptop'], got %v", catEx)
	}

	mfgProp, ok := createReq.Properties["allowed_manufacturers"]
	mfgEx := toStringSlice(mfgProp.Example)
	if !ok || len(mfgEx) == 0 {
		t.Fatalf("allowed_manufacturers property or example missing in CreateUserRequest")
	}
	if mfgEx[0] != "Dell" {
		t.Errorf("Expected CreateUserRequest.allowed_manufacturers example ['Dell'], got %v", mfgEx)
	}

	// 2. UpdateFiltersRequest allowed_categories and allowed_manufacturers
	filterReq, ok := spec.Definitions["UpdateFiltersRequest"]
	if !ok {
		t.Fatalf("UpdateFiltersRequest definition not found in swagger spec")
	}

	filterCatProp, ok := filterReq.Properties["allowed_categories"]
	filterCatEx := toStringSlice(filterCatProp.Example)
	if !ok || len(filterCatEx) == 0 {
		t.Fatalf("allowed_categories property or example missing in UpdateFiltersRequest")
	}
	if filterCatEx[0] != "laptop" {
		t.Errorf("Expected UpdateFiltersRequest.allowed_categories example ['laptop'], got %v", filterCatEx)
	}

	filterMfgProp, ok := filterReq.Properties["allowed_manufacturers"]
	filterMfgEx := toStringSlice(filterMfgProp.Example)
	if !ok || len(filterMfgEx) < 2 {
		t.Fatalf("allowed_manufacturers property or example missing/incomplete in UpdateFiltersRequest: %v", filterMfgEx)
	}
	if filterMfgEx[0] != "Apple" || filterMfgEx[1] != "Dell" {
		t.Errorf("Expected UpdateFiltersRequest.allowed_manufacturers example ['Apple', 'Dell'], got %v", filterMfgEx)
	}

	// 3. String-level sanity check: ensure no "[" or escaped \" exist in raw examples in swagger.json
	for _, prop := range []struct {
		name string
		ex   []string
	}{
		{"CreateUser.allowed_categories", catEx},
		{"CreateUser.allowed_manufacturers", mfgEx},
		{"UpdateFilters.allowed_categories", filterCatEx},
		{"UpdateFilters.allowed_manufacturers", filterMfgEx},
	} {
		for _, val := range prop.ex {
			if strings.Contains(val, "[") || strings.Contains(val, "]") || strings.Contains(val, "\"") {
				t.Errorf("Field %s contains corrupted stringified array artifact: %s", prop.name, val)
			}
		}
	}
}

// TestJWTSecurityAndComplexityPolicy tests Issue #19:
// 1. JWT verification explicitly validates algorithm (HS256 pinned), issuer, and audience
// 2. Rejecting tokens signed with alg:none, wrong issuer, or wrong audience
// 3. User creation accepts passwords without minLength 8 or complexity constraints
func TestJWTSecurityAndComplexityPolicy(t *testing.T) {
	jwtSecret := []byte("warehouse-secret-key-change-in-production")
	validUserID := "b0000000-0000-0000-0000-000000000002"

	// 1. Test alg: none rejection
	noneClaims := jwt.MapClaims{
		"sub":      validUserID,
		"username": "userA",
		"role":     "user",
		"iss":      "warehouse-api",
		"aud":      "warehouse-clients",
		"exp":      time.Now().Add(1 * time.Hour).Unix(),
	}
	noneToken := jwt.NewWithClaims(jwt.SigningMethodNone, noneClaims)
	noneTokenStr, err := noneToken.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("Failed to generate none token: %v", err)
	}

	noneClient := newClient(noneTokenStr)
	respNone, _, _ := noneClient.request(http.MethodGet, "/api/user/profile", nil)
	if respNone.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized for alg:none token, got %d", respNone.StatusCode)
	}

	// 2. Test wrong issuer rejection
	badIssClaims := jwt.MapClaims{
		"sub":      validUserID,
		"username": "userA",
		"role":     "user",
		"iss":      "evil-issuer",
		"aud":      "warehouse-clients",
		"exp":      time.Now().Add(1 * time.Hour).Unix(),
	}
	badIssToken := jwt.NewWithClaims(jwt.SigningMethodHS256, badIssClaims)
	badIssStr, err := badIssToken.SignedString(jwtSecret)
	if err != nil {
		t.Fatalf("Failed to sign bad issuer token: %v", err)
	}

	badIssClient := newClient(badIssStr)
	respBadIss, _, _ := badIssClient.request(http.MethodGet, "/api/user/profile", nil)
	if respBadIss.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized for bad issuer, got %d", respBadIss.StatusCode)
	}

	// 3. Test wrong audience rejection
	badAudClaims := jwt.MapClaims{
		"sub":      validUserID,
		"username": "userA",
		"role":     "user",
		"iss":      "warehouse-api",
		"aud":      "evil-audience",
		"exp":      time.Now().Add(1 * time.Hour).Unix(),
	}
	badAudToken := jwt.NewWithClaims(jwt.SigningMethodHS256, badAudClaims)
	badAudStr, err := badAudToken.SignedString(jwtSecret)
	if err != nil {
		t.Fatalf("Failed to sign bad audience token: %v", err)
	}

	badAudClient := newClient(badAudStr)
	respBadAud, _, _ := badAudClient.request(http.MethodGet, "/api/user/profile", nil)
	if respBadAud.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized for bad audience, got %d", respBadAud.StatusCode)
	}

	// 4. Test that passwords without minLength: 8 or complexity constraints are accepted
	adminToken, code := login(t, "/api/admin/login", "admin", "admin123")
	if code != http.StatusOK {
		t.Fatalf("Admin login failed: %d", code)
	}
	adminClient := newClient(adminToken)

	// 4a. Short password (< 8 chars) accepted
	respShort, _, _ := adminClient.request(http.MethodPost, "/api/admin/users", domain.CreateUserRequest{
		Username: fmt.Sprintf("short_%d", time.Now().UnixNano()),
		Password: "pass12", // 6 chars
		Role:     "user",
		Balance:  100.0,
	})
	if respShort.StatusCode != http.StatusCreated {
		t.Errorf("Expected 201 for password < 8 chars, got %d", respShort.StatusCode)
	}

	// 4b. Letters only (no digits) accepted
	respNoDigits, _, _ := adminClient.request(http.MethodPost, "/api/admin/users", domain.CreateUserRequest{
		Username: fmt.Sprintf("nodigits_%d", time.Now().UnixNano()),
		Password: "passwordonly", // no digits
		Role:     "user",
		Balance:  100.0,
	})
	if respNoDigits.StatusCode != http.StatusCreated {
		t.Errorf("Expected 201 for password without digits, got %d", respNoDigits.StatusCode)
	}

	// 4c. Digits only (no letters) accepted
	respNoLetters, _, _ := adminClient.request(http.MethodPost, "/api/admin/users", domain.CreateUserRequest{
		Username: fmt.Sprintf("noletters_%d", time.Now().UnixNano()),
		Password: "12345678", // no letters
		Role:     "user",
		Balance:  100.0,
	})
	if respNoLetters.StatusCode != http.StatusCreated {
		t.Errorf("Expected 201 for password without letters, got %d", respNoLetters.StatusCode)
	}

	// 4d. Empty password rejected as 400
	respEmpty, _, _ := adminClient.request(http.MethodPost, "/api/admin/users", map[string]any{
		"username": fmt.Sprintf("empty_%d", time.Now().UnixNano()),
		"password": "",
		"role":     "user",
		"balance":  100.0,
	})
	if respEmpty.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for empty password, got %d", respEmpty.StatusCode)
	}
}

// TestUpdateStockQuantityBoundaryValidation tests Issue #20:
// 1. Negative stock updates return 400 Bad Request instead of 500
// 2. Non-negative stock update (e.g. 0 or positive) succeeds with 200 OK
// 3. OpenAPI schema domain.UpdateStockRequest defines minimum: 0 and maximum: 2147483647
func TestUpdateStockQuantityBoundaryValidation(t *testing.T) {
	adminToken, code := login(t, "/api/admin/login", "admin", "admin123")
	if code != http.StatusOK {
		t.Fatalf("Admin login failed: %d", code)
	}
	adminClient := newClient(adminToken)

	// Create a test product
	createProdReq := domain.CreateProductRequest{
		Category:      "laptop",
		Manufacturer:  "TestMfg",
		Model:         fmt.Sprintf("StockTestModel_%d", time.Now().UnixNano()),
		Price:         999.00,
		StockQuantity: 10,
	}
	respCreate, bodyCreate, err := adminClient.request(http.MethodPost, "/api/admin/products", createProdReq)
	if err != nil {
		t.Fatalf("Failed to create product: %v", err)
	}
	if respCreate.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 Created, got %d: %s", respCreate.StatusCode, string(bodyCreate))
	}

	var prodEnvelope domain.SuccessEnvelope
	if err := json.Unmarshal(bodyCreate, &prodEnvelope); err != nil {
		t.Fatalf("Failed to unmarshal product response: %v", err)
	}
	prodData, ok := prodEnvelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("Failed to extract product data from envelope")
	}
	prodID := prodData["id"].(string)

	// 1. Negative stock update (-1) must return 400 Bad Request (NOT 500)
	respNeg, bodyNeg, _ := adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/products/%s/stock", prodID), map[string]int{
		"stock_quantity": -1,
	})
	if respNeg.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for stock_quantity: -1, got %d. Body: %s", respNeg.StatusCode, string(bodyNeg))
	}

	// 2. Negative stock update (-100) must return 400 Bad Request
	respNeg100, bodyNeg100, _ := adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/products/%s/stock", prodID), map[string]int{
		"stock_quantity": -100,
	})
	if respNeg100.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for stock_quantity: -100, got %d. Body: %s", respNeg100.StatusCode, string(bodyNeg100))
	}

	// 3. Stock update to 0 must succeed (200 OK)
	respZero, bodyZero, _ := adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/products/%s/stock", prodID), map[string]int{
		"stock_quantity": 0,
	})
	if respZero.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK for stock_quantity: 0, got %d. Body: %s", respZero.StatusCode, string(bodyZero))
	}

	// 4. Stock update to positive 50 must succeed (200 OK)
	respPos, bodyPos, _ := adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/products/%s/stock", prodID), map[string]int{
		"stock_quantity": 50,
	})
	if respPos.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 OK for stock_quantity: 50, got %d. Body: %s", respPos.StatusCode, string(bodyPos))
	}

	// 5. Verify OpenAPI spec defines minimum: 0 and maximum: 2147483647 for stock_quantity
	respDoc, bodyDoc, err := adminClient.request(http.MethodGet, "/swagger/doc.json", nil)
	if err != nil {
		t.Fatalf("Failed to fetch Swagger JSON: %v", err)
	}
	if respDoc.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK from Swagger doc endpoint, got %d", respDoc.StatusCode)
	}

	var specDoc struct {
		Definitions map[string]struct {
			Properties map[string]struct {
				Minimum *float64 `json:"minimum"`
				Maximum *float64 `json:"maximum"`
			} `json:"properties"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal(bodyDoc, &specDoc); err != nil {
		t.Fatalf("Failed to parse Swagger JSON: %v", err)
	}

	updateStockDef, ok := specDoc.Definitions["UpdateStockRequest"]
	if !ok {
		t.Fatalf("UpdateStockRequest definition not found in swagger doc")
	}
	stockProp, ok := updateStockDef.Properties["stock_quantity"]
	if !ok {
		t.Fatalf("stock_quantity property not found in UpdateStockRequest")
	}
	if stockProp.Minimum == nil || *stockProp.Minimum != 0 {
		t.Errorf("Expected minimum: 0 on stock_quantity, got %v", stockProp.Minimum)
	}
	if stockProp.Maximum == nil || *stockProp.Maximum != 2147483647 {
		t.Errorf("Expected maximum: 2147483647 on stock_quantity, got %v", stockProp.Maximum)
	}
}

// TestOperationalMiddlewareConfiguration tests Issue #22:
// 1. CORS pre-flight OPTIONS request returns 200, allows headers including X-Request-ID and Idempotency-Key
// 2. Panic recovery middleware catches unhandled exceptions and returns standard 500 ErrorEnvelope with requestId
func TestOperationalMiddlewareConfiguration(t *testing.T) {
	// 1. Test CORS Pre-flight on live server
	client := newClient("")
	corsHeaders := map[string]string{
		"Origin":                         "http://localhost:3000",
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": "authorization,content-type,idempotency-key,x-request-id",
	}
	respCors, _, err := client.requestWithHeaders(http.MethodOptions, "/api/user/orders", nil, corsHeaders)
	if err != nil {
		t.Fatalf("CORS preflight request failed: %v", err)
	}
	if respCors.StatusCode != http.StatusOK && respCors.StatusCode != http.StatusNoContent {
		t.Errorf("Expected 200/204 for CORS preflight, got %d", respCors.StatusCode)
	}
	originHeader := respCors.Header.Get("Access-Control-Allow-Origin")
	if originHeader != "*" && originHeader != "http://localhost:3000" {
		t.Errorf("Expected Access-Control-Allow-Origin, got '%s'", originHeader)
	}
	allowHeaders := strings.ToLower(respCors.Header.Get("Access-Control-Allow-Headers"))
	if !strings.Contains(allowHeaders, "x-request-id") || !strings.Contains(allowHeaders, "idempotency-key") {
		t.Errorf("Expected allow-headers to contain x-request-id and idempotency-key, got '%s'", allowHeaders)
	}

	// 2. Test Panic Recovery middleware unit contract
	panickingHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("simulated catastrophic panic")
	})

	recoveryStack := middleware.Tracing()(middleware.Recoverer()(panickingHandler))

	req := httptest.NewRequest(http.MethodGet, "/test/panic", nil)
	req.Header.Set("X-Request-ID", "test-panic-trace-id")
	rec := httptest.NewRecorder()

	recoveryStack.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 Internal Server Error from panic recovery, got %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Errorf("Expected application/json Content-Type, got '%s'", rec.Header().Get("Content-Type"))
	}

	var errEnv domain.ErrorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &errEnv); err != nil {
		t.Fatalf("Failed to unmarshal recovered error envelope: %v", err)
	}
	if errEnv.Success != false {
		t.Errorf("Expected success == false in error envelope")
	}
	if errEnv.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("Expected error code 'INTERNAL_ERROR', got '%s'", errEnv.Error.Code)
	}
	if errEnv.RequestID != "test-panic-trace-id" {
		t.Errorf("Expected requestId 'test-panic-trace-id', got '%s'", errEnv.RequestID)
	}
}

func TestCleanSwaggerSchemaDefinitionNames(t *testing.T) {
	client := newClient("")
	respDoc, bodyDoc, err := client.request(http.MethodGet, "/swagger/doc.json", nil)
	if err != nil || respDoc.StatusCode != http.StatusOK {
		t.Fatalf("Failed to fetch /swagger/doc.json: status=%v, err=%v", respDoc.StatusCode, err)
	}

	var specDoc struct {
		Definitions map[string]interface{} `json:"definitions"`
	}
	if err := json.Unmarshal(bodyDoc, &specDoc); err != nil {
		t.Fatalf("Failed to parse Swagger JSON: %v", err)
	}

	if len(specDoc.Definitions) == 0 {
		t.Fatalf("No definitions found in Swagger spec")
	}

	for defName := range specDoc.Definitions {
		if strings.Contains(defName, ".") {
			t.Errorf("Swagger definition name '%s' contains dot (should be clean unqualified struct name)", defName)
		}
		if strings.Contains(defName, "domain") {
			t.Errorf("Swagger definition name '%s' contains package name 'domain'", defName)
		}
		if strings.Contains(defName, "github_com") || strings.Contains(defName, "/") {
			t.Errorf("Swagger definition name '%s' contains package path", defName)
		}
	}

	expectedCleanDefinitions := []string{
		"CreateOrderRequest",
		"CreateProductRequest",
		"CreateUserRequest",
		"LoginRequest",
		"LoginResponse",
		"OrderResponse",
		"Product",
		"TopUpBalanceRequest",
		"UpdateStockRequest",
		"UserSummary",
	}

	for _, expected := range expectedCleanDefinitions {
		if _, exists := specDoc.Definitions[expected]; !exists {
			t.Errorf("Expected clean Swagger definition '%s' not found", expected)
		}
	}
}

func TestOpenAPIStructuralHygiene(t *testing.T) {
	client := newClient("")
	respDoc, bodyDoc, err := client.request(http.MethodGet, "/swagger/doc.json", nil)
	if err != nil || respDoc.StatusCode != http.StatusOK {
		t.Fatalf("Failed to fetch /swagger/doc.json: status=%v, err=%v", respDoc.StatusCode, err)
	}

	var specDoc struct {
		Schemes []string `json:"schemes"`
		Paths   map[string]map[string]struct {
			OperationID string   `json:"operationId"`
			Consumes    []string `json:"consumes"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(bodyDoc, &specDoc); err != nil {
		t.Fatalf("Failed to parse Swagger JSON: %v", err)
	}

	// 1. Schemes verification
	hasHTTP := false
	hasHTTPS := false
	for _, scheme := range specDoc.Schemes {
		if scheme == "http" {
			hasHTTP = true
		}
		if scheme == "https" {
			hasHTTPS = true
		}
	}
	if !hasHTTP || !hasHTTPS {
		t.Errorf("Expected schemes to include 'http' and 'https', got %v", specDoc.Schemes)
	}

	// 2. Operation IDs and Consumes on GET/DELETE
	totalOperations := 0
	seenOpIDs := make(map[string]string)
	for path, methods := range specDoc.Paths {
		for method, op := range methods {
			totalOperations++
			if op.OperationID == "" {
				t.Errorf("Operation %s %s missing operationId", strings.ToUpper(method), path)
			} else {
				if prevPath, exists := seenOpIDs[op.OperationID]; exists {
					t.Errorf("Duplicate operationId '%s' for %s %s (already used by %s)", op.OperationID, strings.ToUpper(method), path, prevPath)
				}
				seenOpIDs[op.OperationID] = fmt.Sprintf("%s %s", strings.ToUpper(method), path)
			}

			if strings.EqualFold(method, "get") && len(op.Consumes) > 0 {
				t.Errorf("GET %s declares consumes: %v (GET must not declare consumes)", path, op.Consumes)
			}
			if strings.EqualFold(method, "delete") && len(op.Consumes) > 0 {
				t.Errorf("DELETE %s declares consumes: %v (DELETE must not declare consumes)", path, op.Consumes)
			}
		}
	}

	if totalOperations < 15 {
		t.Errorf("Expected at least 15 operations, found %d", totalOperations)
	}
}

func TestOrderThreeStatusesAndCancellationRefund(t *testing.T) {
	adminToken, status := login(t, "/api/admin/login", "admin", "admin123")
	if status != http.StatusOK || adminToken == "" {
		t.Fatalf("Admin login failed: %d", status)
	}
	adminClient := newClient(adminToken)

	// 1. Create a dedicated test user with 1000.00 balance
	username := fmt.Sprintf("refunduser_%d", time.Now().UnixNano())
	respCreateUser, bodyCreateUser, _ := adminClient.request(http.MethodPost, "/api/admin/users", domain.CreateUserRequest{
		Username: username,
		Password: "Password123!",
		Role:     "user",
		Balance:  1000.00,
	})
	if respCreateUser.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to create user: %d: %s", respCreateUser.StatusCode, string(bodyCreateUser))
	}

	// 2. Create a dedicated product with 10 stock and 150.00 price
	respCreateProd, bodyCreateProd, _ := adminClient.request(http.MethodPost, "/api/admin/products", domain.CreateProductRequest{
		Category:      "hardware",
		Manufacturer:  "RefundCo",
		Model:         fmt.Sprintf("RefundModel_%d", time.Now().UnixNano()),
		Price:         150.00,
		StockQuantity: 10,
	})
	if respCreateProd.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to create product: %d: %s", respCreateProd.StatusCode, string(bodyCreateProd))
	}
	var createdProd struct {
		Data domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyCreateProd, &createdProd)
	productID := createdProd.Data.ID

	// 3. User logs in and places order for 2 units (total = 300.00)
	userToken, status := login(t, "/api/user/login", username, "Password123!")
	if status != http.StatusOK || userToken == "" {
		t.Fatalf("User login failed: %d", status)
	}
	userClient := newClient(userToken)

	respOrder, bodyOrder, _ := userClient.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
		ProductID: productID,
		Quantity:  2,
	})
	if respOrder.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 for order creation, got %d: %s", respOrder.StatusCode, string(bodyOrder))
	}
	var orderResult struct {
		Data domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyOrder, &orderResult)

	// Verify order status is PROCESSED upon completion
	if orderResult.Data.Status != "PROCESSED" {
		t.Errorf("Expected completed order status 'PROCESSED', got '%s'", orderResult.Data.Status)
	}
	if orderResult.Data.TotalPrice != 300.00 {
		t.Errorf("Expected total price 300.00, got %f", orderResult.Data.TotalPrice)
	}
	if orderResult.Data.RemainingBalance != 700.00 {
		t.Errorf("Expected remaining balance 700.00, got %f", orderResult.Data.RemainingBalance)
	}
	orderID := orderResult.Data.OrderID

	// Verify product stock is decremented to 8
	respProdAfter, bodyProdAfter, _ := adminClient.request(http.MethodGet, "/api/admin/products", nil)
	if respProdAfter.StatusCode == http.StatusOK {
		var prods struct {
			Data []domain.Product `json:"data"`
		}
		_ = json.Unmarshal(bodyProdAfter, &prods)
		for _, p := range prods.Data {
			if p.ID == productID && p.StockQuantity != 8 {
				t.Errorf("Expected stock quantity 8 after order, got %d", p.StockQuantity)
			}
		}
	}

	// 4. Verify invalid status transitions (e.g. SHIPPED, DELIVERED) return 422
	respInvalid1, _, _ := adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/orders/%s/status", orderID), domain.UpdateOrderStatusRequest{
		Status: "SHIPPED",
	})
	if respInvalid1.StatusCode != 422 {
		t.Errorf("Expected 422 when updating status to obsolete 'SHIPPED', got %d", respInvalid1.StatusCode)
	}

	respInvalid2, _, _ := adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/orders/%s/status", orderID), domain.UpdateOrderStatusRequest{
		Status: "DELIVERED",
	})
	if respInvalid2.StatusCode != 422 {
		t.Errorf("Expected 422 when updating status to obsolete 'DELIVERED', got %d", respInvalid2.StatusCode)
	}

	respInvalid3, _, _ := adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/orders/%s/status", orderID), domain.UpdateOrderStatusRequest{
		Status: "CREATED",
	})
	if respInvalid3.StatusCode != 422 {
		t.Errorf("Expected 422 when updating status to obsolete 'CREATED', got %d", respInvalid3.StatusCode)
	}

	// 5. Admin cancels the order -> expect 200 OK, status CANCELLED
	respCancel, bodyCancel, _ := adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/orders/%s/status", orderID), domain.UpdateOrderStatusRequest{
		Status: "CANCELLED",
	})
	if respCancel.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for order cancellation, got %d: %s", respCancel.StatusCode, string(bodyCancel))
	}
	var cancelResult struct {
		Data domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyCancel, &cancelResult)
	if cancelResult.Data.Status != "CANCELLED" {
		t.Errorf("Expected order status 'CANCELLED', got '%s'", cancelResult.Data.Status)
	}

	// 6. Verify user balance was refunded back to 1000.00
	respProfile, bodyProfile, _ := userClient.request(http.MethodGet, "/api/user/profile", nil)
	if respProfile.StatusCode != http.StatusOK {
		t.Fatalf("Failed to fetch user profile: %d", respProfile.StatusCode)
	}
	var profileResult struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyProfile, &profileResult)
	if profileResult.Data.Balance != 1000.00 {
		t.Errorf("Expected refunded balance 1000.00, got %f", profileResult.Data.Balance)
	}

	// 7. Verify product stock was restored back to 10
	respProdFinal, bodyProdFinal, _ := adminClient.request(http.MethodGet, "/api/admin/products", nil)
	if respProdFinal.StatusCode == http.StatusOK {
		var prods struct {
			Data []domain.Product `json:"data"`
		}
		_ = json.Unmarshal(bodyProdFinal, &prods)
		for _, p := range prods.Data {
			if p.ID == productID && p.StockQuantity != 10 {
				t.Errorf("Expected restocked quantity 10 after cancellation, got %d", p.StockQuantity)
			}
		}
	}

	// 8. Attempting to cancel or fail an already cancelled order must fail with 422
	respReCancel, _, _ := adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/orders/%s/status", orderID), domain.UpdateOrderStatusRequest{
		Status: "CANCELLED",
	})
	if respReCancel.StatusCode != 422 {
		t.Errorf("Expected 422 when re-cancelling already cancelled order, got %d", respReCancel.StatusCode)
	}

	respCancelToFail, _, _ := adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/orders/%s/status", orderID), domain.UpdateOrderStatusRequest{
		Status: "FAILED",
	})
	if respCancelToFail.StatusCode != 422 {
		t.Errorf("Expected 422 when transitioning cancelled order to FAILED, got %d", respCancelToFail.StatusCode)
	}

	// Verify balance is still 1000.00 (no double refund)
	respProfileAgain, bodyProfileAgain, _ := userClient.request(http.MethodGet, "/api/user/profile", nil)
	if respProfileAgain.StatusCode == http.StatusOK {
		_ = json.Unmarshal(bodyProfileAgain, &profileResult)
		if profileResult.Data.Balance != 1000.00 {
			t.Errorf("Balance changed after rejected re-cancellation: %f", profileResult.Data.Balance)
		}
	}

	// 9. Place a second order and transition to 'FAILED' (when order cannot be fulfilled)
	respOrder2, bodyOrder2, _ := userClient.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
		ProductID: productID,
		Quantity:  2,
	})
	if respOrder2.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to create second order: %d: %s", respOrder2.StatusCode, string(bodyOrder2))
	}
	var orderResult2 struct {
		Data domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyOrder2, &orderResult2)
	if orderResult2.Data.Status != "PROCESSED" {
		t.Errorf("Expected second order status 'PROCESSED', got %s", orderResult2.Data.Status)
	}
	orderID2 := orderResult2.Data.OrderID

	// Admin transitions order to FAILED -> expect 200 OK, status FAILED
	respFail, bodyFail, _ := adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/orders/%s/status", orderID2), domain.UpdateOrderStatusRequest{
		Status: "FAILED",
	})
	if respFail.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for order failure, got %d: %s", respFail.StatusCode, string(bodyFail))
	}
	var failResult struct {
		Data domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyFail, &failResult)
	if failResult.Data.Status != "FAILED" {
		t.Errorf("Expected order status 'FAILED', got '%s'", failResult.Data.Status)
	}

	// Verify user balance was refunded back to 1000.00 after failure
	respProfileFail, bodyProfileFail, _ := userClient.request(http.MethodGet, "/api/user/profile", nil)
	if respProfileFail.StatusCode == http.StatusOK {
		_ = json.Unmarshal(bodyProfileFail, &profileResult)
		if profileResult.Data.Balance != 1000.00 {
			t.Errorf("Expected refunded balance 1000.00 after FAILED order, got %f", profileResult.Data.Balance)
		}
	}

	// Verify product stock was restored back to 10 after failure
	respProdFinal2, bodyProdFinal2, _ := adminClient.request(http.MethodGet, "/api/admin/products", nil)
	if respProdFinal2.StatusCode == http.StatusOK {
		var prods struct {
			Data []domain.Product `json:"data"`
		}
		_ = json.Unmarshal(bodyProdFinal2, &prods)
		for _, p := range prods.Data {
			if p.ID == productID && p.StockQuantity != 10 {
				t.Errorf("Expected restocked quantity 10 after order failure, got %d", p.StockQuantity)
			}
		}
	}

	// Re-failing or cancelling an already failed order must fail with 422
	respReFail, _, _ := adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/orders/%s/status", orderID2), domain.UpdateOrderStatusRequest{
		Status: "FAILED",
	})
	if respReFail.StatusCode != 422 {
		t.Errorf("Expected 422 when re-failing already failed order, got %d", respReFail.StatusCode)
	}

	respFailCancel, _, _ := adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/orders/%s/status", orderID2), domain.UpdateOrderStatusRequest{
		Status: "CANCELLED",
	})
	if respFailCancel.StatusCode != 422 {
		t.Errorf("Expected 422 when cancelling already failed order, got %d", respFailCancel.StatusCode)
	}
}

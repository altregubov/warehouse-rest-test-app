package tests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
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

	_, _, _ = adminClient.request(http.MethodPut, fmt.Sprintf("/api/admin/users/%s/balance", profA.Data.ID), domain.SetBalanceRequest{
		NewBalance: 5000.00,
	})
	initialBalance := 5000.00

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

	// 3. Insufficient balance: set low balance with valid in-stock quantity -> 422 INSUFFICIENT_FUNDS
	_, _, _ = adminClient.request(http.MethodPatch, fmt.Sprintf("/api/admin/products/%s/stock", testProduct.ID), domain.UpdateStockRequest{
		StockQuantity: 10,
	})
	_, _, _ = adminClient.request(http.MethodPut, fmt.Sprintf("/api/admin/users/%s/balance", profA.Data.ID), domain.SetBalanceRequest{
		NewBalance: 10.00,
	})
	respInsufficientFunds, bodyFunds, _ := clientA.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
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
	// Top up balance first so balance check passes
	_, _, _ = adminClient.request(http.MethodPut, fmt.Sprintf("/api/admin/users/%s/balance", profA.Data.ID), domain.SetBalanceRequest{
		NewBalance: 5000000.00,
	})
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
		Password:             "user123",
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

	// 4. Update user balance (+500 via legacy PATCH)
	respBalance, bodyBalance, _ := adminClient.request(
		http.MethodPatch,
		fmt.Sprintf("/api/admin/users/%s/balance", newUser.Data.ID),
		domain.UpdateBalanceRequest{Amount: 500.00},
	)
	if respBalance.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for balance topup, got %d: %s", respBalance.StatusCode, string(bodyBalance))
	}
	var updatedUserBalance struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyBalance, &updatedUserBalance)
	if updatedUserBalance.Data.Balance != 600.00 {
		t.Errorf("Expected balance 600.00, got %f", updatedUserBalance.Data.Balance)
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
	if topUpResult.Data.Balance != 850.00 {
		t.Errorf("Expected balance 850.00, got %f", topUpResult.Data.Balance)
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

	// 4c. Dedicated SetBalance PUT /api/admin/users/{id}/balance (new_balance = 1500)
	respSetBalance, bodySetBalance, _ := adminClient.request(
		http.MethodPut,
		fmt.Sprintf("/api/admin/users/%s/balance", newUser.Data.ID),
		domain.SetBalanceRequest{NewBalance: 1500.00},
	)
	if respSetBalance.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for dedicated set balance, got %d: %s", respSetBalance.StatusCode, string(bodySetBalance))
	}
	var setResult struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodySetBalance, &setResult)
	if setResult.Data.Balance != 1500.00 {
		t.Errorf("Expected balance 1500.00, got %f", setResult.Data.Balance)
	}

	// 4d. Invalid SetBalance (< 0) -> 422
	respInvalidSet, _, _ := adminClient.request(
		http.MethodPut,
		fmt.Sprintf("/api/admin/users/%s/balance", newUser.Data.ID),
		domain.SetBalanceRequest{NewBalance: -50.00},
	)
	if respInvalidSet.StatusCode != 422 {
		t.Errorf("Expected 422 for negative balance set, got %d", respInvalidSet.StatusCode)
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
	if orderResult.Data.Status != "CREATED" {
		t.Errorf("Expected order status 'CREATED', got %s", orderResult.Data.Status)
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

	// 8. Admin updates order status to SHIPPED
	respStatus, bodyStatus, _ := adminClient.request(
		http.MethodPatch,
		fmt.Sprintf("/api/admin/orders/%s/status", orderID),
		domain.UpdateOrderStatusRequest{Status: "SHIPPED"},
	)
	if respStatus.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for status update, got %d: %s", respStatus.StatusCode, string(bodyStatus))
	}
	var updatedStatusResult struct {
		Data domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyStatus, &updatedStatusResult)
	if updatedStatusResult.Data.Status != "SHIPPED" {
		t.Errorf("Expected status 'SHIPPED', got %s", updatedStatusResult.Data.Status)
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

func TestIdempotencyKeySupport(t *testing.T) {
	adminToken, _ := login(t, "/api/admin/login", "admin", "admin123")
	adminClient := newClient(adminToken)

	// Create user
	username := fmt.Sprintf("idemp_user_%d", time.Now().UnixNano())
	respCreate, bodyCreate, _ := adminClient.request(http.MethodPost, "/api/admin/users", domain.CreateUserRequest{
		Username: username,
		Password: "password123",
		Role:     domain.RoleUser,
		Balance:  1000.00,
	})
	if respCreate.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to create test user: %s", string(bodyCreate))
	}
	var createdUser struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyCreate, &createdUser)
	userID := createdUser.Data.ID

	// Create product
	respProd, bodyProd, _ := adminClient.request(http.MethodPost, "/api/admin/products", domain.CreateProductRequest{
		Category:      "laptop",
		Manufacturer:  "IdempTech",
		Model:         fmt.Sprintf("IdempBook_%d", time.Now().UnixNano()),
		Price:         200.00,
		StockQuantity: 10,
	})
	if respProd.StatusCode != http.StatusCreated {
		t.Fatalf("Failed to create test product: %s", string(bodyProd))
	}
	var createdProd struct {
		Data domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyProd, &createdProd)
	productID := createdProd.Data.ID

	userToken, _ := login(t, "/api/user/login", username, "password123")
	userClient := newClient(userToken)

	orderKey := fmt.Sprintf("order-idemp-key-%d", time.Now().UnixNano())
	orderPayload := domain.CreateOrderRequest{
		ProductID: productID,
		Quantity:  1,
	}

	// 1. First order execution
	resp1, body1, err := userClient.requestWithHeaders(http.MethodPost, "/api/user/orders", orderPayload, map[string]string{
		"Idempotency-Key": orderKey,
	})
	if err != nil {
		t.Fatalf("Initial order request failed: %v", err)
	}
	if resp1.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 Created for first order, got %d: %s", resp1.StatusCode, string(body1))
	}
	var order1 struct {
		Data domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(body1, &order1)
	if order1.Data.OrderID == [16]byte{} {
		t.Fatalf("Expected valid order ID from first order")
	}

	// 2. Simulated network retry (identical request & key)
	resp2, body2, err := userClient.requestWithHeaders(http.MethodPost, "/api/user/orders", orderPayload, map[string]string{
		"Idempotency-Key": orderKey,
	})
	if err != nil {
		t.Fatalf("Replayed order request failed: %v", err)
	}
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 Created on replayed order, got %d: %s", resp2.StatusCode, string(body2))
	}
	if resp2.Header.Get("Idempotent-Replayed") != "true" {
		t.Errorf("Expected Idempotent-Replayed: true header, got %q", resp2.Header.Get("Idempotent-Replayed"))
	}
	var order2 struct {
		Data domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(body2, &order2)
	if order2.Data.OrderID != order1.Data.OrderID {
		t.Errorf("Replayed order ID %s does not match original %s", order2.Data.OrderID, order1.Data.OrderID)
	}

	// Verify balance was only deducted ONCE (1000 - 200 = 800)
	_, bodyProf, _ := userClient.request(http.MethodGet, "/api/user/profile", nil)
	var prof struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyProf, &prof)
	if prof.Data.Balance != 800.00 {
		t.Errorf("Expected user balance to be 800.00 after retry, got %.2f", prof.Data.Balance)
	}

	// Verify only 1 order exists for user
	_, bodyOrders, _ := userClient.request(http.MethodGet, "/api/user/orders", nil)
	var ordersList struct {
		Data []domain.OrderResponse `json:"data"`
	}
	_ = json.Unmarshal(bodyOrders, &ordersList)
	if len(ordersList.Data) != 1 {
		t.Errorf("Expected exactly 1 order recorded for user, got %d", len(ordersList.Data))
	}

	// 3. Conflicting payload with same idempotency key -> 409 Conflict
	conflictPayload := domain.CreateOrderRequest{
		ProductID: productID,
		Quantity:  2,
	}
	respConflict, bodyConflict, _ := userClient.requestWithHeaders(http.MethodPost, "/api/user/orders", conflictPayload, map[string]string{
		"Idempotency-Key": orderKey,
	})
	if respConflict.StatusCode != http.StatusConflict {
		t.Errorf("Expected 409 Conflict for mismatched payload under same key, got %d: %s", respConflict.StatusCode, string(bodyConflict))
	}

	// 4. Balance adjustment idempotency
	topUpKey := fmt.Sprintf("topup-idemp-key-%d", time.Now().UnixNano())
	topUpPayload := domain.TopUpBalanceRequest{IncrementAmount: 150.00}
	topUpPath := fmt.Sprintf("/api/admin/users/%s/balance/top-up", userID)

	respTopUp1, bodyTopUp1, _ := adminClient.requestWithHeaders(http.MethodPost, topUpPath, topUpPayload, map[string]string{
		"Idempotency-Key": topUpKey,
	})
	if respTopUp1.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK for balance top-up, got %d: %s", respTopUp1.StatusCode, string(bodyTopUp1))
	}
	var topUp1 struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyTopUp1, &topUp1)
	if topUp1.Data.Balance != 950.00 {
		t.Errorf("Expected balance 950.00 after initial top-up, got %.2f", topUp1.Data.Balance)
	}

	// Replay balance top-up
	respTopUp2, bodyTopUp2, _ := adminClient.requestWithHeaders(http.MethodPost, topUpPath, topUpPayload, map[string]string{
		"Idempotency-Key": topUpKey,
	})
	if respTopUp2.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 OK for replayed balance top-up, got %d: %s", respTopUp2.StatusCode, string(bodyTopUp2))
	}
	if respTopUp2.Header.Get("Idempotent-Replayed") != "true" {
		t.Errorf("Expected Idempotent-Replayed header on replayed top-up")
	}
	var topUp2 struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyTopUp2, &topUp2)
	if topUp2.Data.Balance != 950.00 {
		t.Errorf("Expected balance to remain 950.00 after replayed top-up, got %.2f", topUp2.Data.Balance)
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

	// 3. String length constraints: empty username or short password rejected as 400
	respShortPass, _, _ := adminClient.request(http.MethodPost, "/api/admin/users", map[string]any{
		"username": "valid_user_short_pass",
		"password": "12",
		"role":     "user",
		"balance":  100.0,
	})
	if respShortPass.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for password length < 4, got %d", respShortPass.StatusCode)
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
		"domain.CreateUserRequest",
		"domain.CreateProductRequest",
		"domain.CreateOrderRequest",
		"domain.LoginRequest",
		"domain.UpdateStockRequest",
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
	_, _, _ = adminClient.request(http.MethodPut, fmt.Sprintf("/api/admin/users/%s/balance", profA.Data.ID), domain.SetBalanceRequest{
		NewBalance: 10000.00,
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
		Status: "SHIPPED",
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
		AccessLevel:          "FILTERED",
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

	// 4. Configure user for ZERO catalog access (AccessLevel: NONE)
	accessFalse := false
	respZeroAccess, bodyZeroAccess, _ := adminClient.request(
		http.MethodPut,
		fmt.Sprintf("/api/admin/users/%s/filters", userID),
		domain.UpdateFiltersRequest{
			AllowedCategories:    []string{},
			AllowedManufacturers:  []string{},
			AccessLevel:          "NONE",
			CatalogAccessEnabled: &accessFalse,
		},
	)
	if respZeroAccess.StatusCode != http.StatusOK {
		t.Fatalf("Failed to update filters to zero access: %s", string(bodyZeroAccess))
	}
	var zeroSummary struct {
		Data domain.UserSummary `json:"data"`
	}
	_ = json.Unmarshal(bodyZeroAccess, &zeroSummary)
	if zeroSummary.Data.AccessLevel != "NONE" || zeroSummary.Data.CatalogAccessEnabled != false {
		t.Errorf("Expected access_level=NONE and catalog_access_enabled=false, got %s / %v", zeroSummary.Data.AccessLevel, zeroSummary.Data.CatalogAccessEnabled)
	}

	// Verify catalog exploration returns 0 products
	_, bodyEmptyList, _ := caseClient.request(http.MethodGet, "/api/user/products", nil)
	var emptyProds struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyEmptyList, &emptyProds)
	if len(emptyProds.Data) != 0 {
		t.Errorf("Expected 0 products for zero-access user, got %d", len(emptyProds.Data))
	}

	// Verify order attempt is rejected with 422 FILTER_RESTRICTION
	respDeniedOrder, bodyDeniedOrder, _ := caseClient.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
		ProductID: appleLaptop.ID,
		Quantity:  1,
	})
	if respDeniedOrder.StatusCode != 422 {
		t.Errorf("Expected 422 FILTER_RESTRICTION for zero-access user order, got %d: %s", respDeniedOrder.StatusCode, string(bodyDeniedOrder))
	}
	var errDenied domain.ErrorEnvelope
	_ = json.Unmarshal(bodyDeniedOrder, &errDenied)
	if errDenied.Error.Code != "FILTER_RESTRICTION" {
		t.Errorf("Expected code FILTER_RESTRICTION, got %s", errDenied.Error.Code)
	}

	// 5. Restore user to ALL access
	accessTrue := true
	_, _, _ = adminClient.request(
		http.MethodPut,
		fmt.Sprintf("/api/admin/users/%s/filters", userID),
		domain.UpdateFiltersRequest{
			AllowedCategories:    []string{},
			AllowedManufacturers:  []string{},
			AccessLevel:          "ALL",
			CatalogAccessEnabled: &accessTrue,
		},
	)
	_, bodyFullList, _ := caseClient.request(http.MethodGet, "/api/user/products", nil)
	var fullProds struct {
		Data []domain.Product `json:"data"`
	}
	_ = json.Unmarshal(bodyFullList, &fullProds)
	if len(fullProds.Data) < 3 {
		t.Errorf("Expected full catalog for restored user, got %d products", len(fullProds.Data))
	}
}

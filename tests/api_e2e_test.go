package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
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

	// 2. Regular user login on /api/admin/login -> 403 Forbidden
	_, status = login(t, "/api/admin/login", "userA", "user123")
	if status != http.StatusForbidden {
		t.Errorf("Expected 403 when regular user logs into admin endpoint, got %d", status)
	}

	// 3. User login on /api/user/login -> Success (200)
	userAToken, status := login(t, "/api/user/login", "userA", "user123")
	if status != http.StatusOK || userAToken == "" {
		t.Fatalf("UserA login failed, got status %d", status)
	}

	// 4. Admin login on /api/user/login -> 403 Forbidden
	_, status = login(t, "/api/user/login", "admin", "admin123")
	if status != http.StatusForbidden {
		t.Errorf("Expected 403 when admin logs into user endpoint, got %d", status)
	}

	// 5. Invalid password -> 401 Unauthorized
	_, status = login(t, "/api/user/login", "userA", "wrongpass")
	if status != http.StatusUnauthorized {
		t.Errorf("Expected 401 for wrong password, got %d", status)
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

	// Fetch profile to verify initial balance
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

	// 2. Disallowed purchase: User B (only laptops) attempts to purchase a smartphone
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
		respDisallowed, _, _ := clientB.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
			ProductID: smartphone.ID,
			Quantity:  1,
		})
		if respDisallowed.StatusCode != http.StatusForbidden {
			t.Errorf("Expected 403 when User B orders smartphone, got %d", respDisallowed.StatusCode)
		}
	}

	// 3. Insufficient balance: attempt to purchase quantity that exceeds user balance
	respInsufficientFunds, _, _ := clientA.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
		ProductID: testProduct.ID,
		Quantity:  100, // exceeds balance
	})
	if respInsufficientFunds.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for insufficient balance, got %d", respInsufficientFunds.StatusCode)
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

	// 4. Update user balance (+500)
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

package tests

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
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
	firstProduct := prodsResult.Data[0]

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

	respOrder, bodyOrder, _ := userClient.request(http.MethodPost, "/api/user/orders", domain.CreateOrderRequest{
		ProductID: prods.Data[0].ID,
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

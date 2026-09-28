# Warehouse REST API Testbench Specification

## 1. Executive Summary & Objective

The Warehouse REST API Testbench is a lightweight, idiomatic Go backend service designed for API testing, interactive Swagger UI exploration, and seamless future frontend integration. The service provides role-based access control (RBAC), user catalog filtering permissions, inventory management, and transactional ordering with PostgreSQL persistence, containerized via Docker Compose.

---

## 2. Tech Stack & Architectural Design

### 2.1 Technology Stack
- **Language & Runtime:** Go 1.22+ (idiomatic, standard library conventions).
- **HTTP Routing:** Minimal, idiomatic router (`chi` / `gin`).
- **Database:** PostgreSQL 16+ running in Docker with automated schema migration/initialization scripts and container healthchecks.
- **Documentation:** Interactive OpenAPI 3.0 / Swagger 2.0 served directly by the Go backend at `http://localhost:<PORT>/swagger/index.html` (via `swaggo/http-swagger` or embedded spec) with full Bearer Token authorization support.
- **Containerization:** `docker-compose.yml` for unified local orchestration of the backend service and PostgreSQL database.

### 2.2 Layered Architecture
The project follows a standard modular layered architecture ensuring clean separation of concerns:
```
├── cmd/
│   └── server/          # Application entrypoint (main.go)
├── internal/
│   ├── config/          # Environment and application configuration
│   ├── domain/          # Core domain models, request/response DTOs, errors
│   ├── handler/         # HTTP handlers, request validation, response serialization
│   ├── middleware/      # Auth (JWT), RBAC, CORS, logging, recovery
│   ├── repository/      # Database queries, SQL migrations, transactional storage
│   └── service/         # Core business logic, validation rules, transactions
├── docs/                # Swagger annotations, swagger.json, swagger.yaml
├── scripts/             # Database initialization and seed scripts
├── docker-compose.yml   # Docker composition with healthchecks
├── Dockerfile           # Multi-stage Go production container build
├── SPEC.md              # System specification (this document)
└── README.md            # Setup, execution instructions, test credentials
```

### 2.3 Response Envelope & Error Format
All API responses follow consistent JSON envelopes:

**Success Envelope:**
```json
{
  "success": true,
  "data": { ... }
}
```
*(Or array of items in `data` for list endpoints)*

**Error Envelope:**
```json
{
  "success": false,
  "error": {
    "code": "INSUFFICIENT_FUNDS",
    "message": "User balance is insufficient for this purchase",
    "details": null
  }
}
```

---

## 3. Database Schema & Data Models

### 3.1 DDL Schema Definition (PostgreSQL)

```sql
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Users Table
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    username VARCHAR(100) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(20) NOT NULL CHECK (role IN ('admin', 'user')),
    balance NUMERIC(12, 2) NOT NULL DEFAULT 0.00,
    allowed_categories TEXT[] DEFAULT '{}',
    allowed_manufacturers TEXT[] DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE NULL
);

-- Products Table
CREATE TABLE IF NOT EXISTS products (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    category VARCHAR(100) NOT NULL,
    manufacturer VARCHAR(100) NOT NULL,
    model VARCHAR(150) NOT NULL,
    price NUMERIC(12, 2) NOT NULL CHECK (price >= 0),
    stock_quantity INTEGER NOT NULL CHECK (stock_quantity >= 0),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Orders Table
CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    total_price NUMERIC(12, 2) NOT NULL CHECK (total_price >= 0),
    status VARCHAR(50) NOT NULL DEFAULT 'CREATED',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_products_category ON products(category);
CREATE INDEX IF NOT EXISTS idx_products_manufacturer ON products(manufacturer);
CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders(user_id);
```

### 3.2 Domain Model Invariants
1. **User Catalog Filters:**
   - `allowed_categories`: Text array. **If empty (`{}`) or null, all categories are permitted.**
   - `allowed_manufacturers`: Text array. **If empty (`{}`) or null, all manufacturers are permitted.**
2. **Product Extensibility:**
   - Category is an arbitrary, non-restricted string (e.g., `laptop`, `smartphone`, `monitor`, `tablet`, `accessory`). No hardcoded enums.
3. **Atomic Balance & Inventory Constraints:**
   - Balances and stock quantities must never be negative.

---

## 4. Authentication, Authorization & RBAC

### 4.1 JWT Authentication
- Standard JWT (JSON Web Token) with claims:
  - `sub`: User ID (UUID)
  - `username`: Username
  - `role`: Role (`admin` | `user`)
  - `exp`: Expiration timestamp
- Header format: `Authorization: Bearer <token>`

### 4.2 Segregated Login Endpoints & Authentication Oracle Elimination
1. `POST /api/admin/login`
   - Accepts: `{ "username": "...", "password": "..." }`
   - Verifies credentials and strictly enforces `role == 'admin'`.
   - **Unified Failure Response:** Returns uniform `401 Unauthorized` (`{"success": false, "error": {"code": "INVALID_CREDENTIALS", "message": "Invalid username or password"}}`) on non-existent users, bad passwords, and role mismatches. Employs constant-time dummy bcrypt hashing to prevent timing side-channels and user enumeration.
2. `POST /api/user/login`
   - Accepts: `{ "username": "...", "password": "..." }`
   - Verifies credentials and strictly enforces `role == 'user'`.
   - **Unified Failure Response:** Returns uniform `401 Unauthorized` (`{"success": false, "error": {"code": "INVALID_CREDENTIALS", "message": "Invalid username or password"}}`) on non-existent users, bad passwords, and role mismatches.

### 4.3 Route RBAC Middleware
- `/api/admin/*`: Restricted to valid JWTs with `role == 'admin'`.
- `/api/user/*`: Restricted to valid JWTs with `role == 'user'`.

---

## 5. API Specification & Endpoints

### 5.1 Documentation
- `GET /swagger/*`
  - Interactive Swagger UI served at `/swagger/index.html`.
  - Supports Bearer JWT authorization modal.

### 5.2 Auth Routes
- `POST /api/admin/login`
  - Body: `{ "username": "admin", "password": "admin123" }`
  - Response (200 OK): `{ "success": true, "data": { "token": "<jwt>", "user": { "id": "...", "username": "admin", "role": "admin" } } }`
  - Failure (401 Unauthorized): `{ "success": false, "error": {"code": "INVALID_CREDENTIALS", "message": "Invalid username or password"} }`
- `POST /api/user/login`
  - Body: `{ "username": "userA", "password": "user123" }`
  - Response (200 OK): `{ "success": true, "data": { "token": "<jwt>", "user": { "id": "...", "username": "userA", "role": "user" } } }`
  - Failure (401 Unauthorized): `{ "success": false, "error": {"code": "INVALID_CREDENTIALS", "message": "Invalid username or password"} }`

### 5.3 Admin Routes (`/api/admin/*`, Bearer Admin Token Required)

1. `POST /api/admin/users`
   - Creates a new user or admin account.
   - Body:
     ```json
     {
       "username": "new_user",
       "password": "secure_password",
       "role": "user",
       "balance": 1000.00,
       "allowed_categories": ["laptop"],
       "allowed_manufacturers": ["Dell"]
     }
     ```
   - Response (201 Created): User details (excluding `password_hash`).

2. Balance Management Operations (Supports `Idempotency-Key` Header with 24-Hour TTL)
   - **Header Support:** Clients may provide `Idempotency-Key: <uuid-or-string>` on all balance mutation endpoints. Identical replayed requests return the original cached response with `Idempotent-Replayed: true` header. Requests with conflicting payloads or concurrent in-flight executions under the same key return `409 Conflict`.
   - **Top-Up Balance (Relative Increment):** `POST /api/admin/users/{id}/balance/top-up`
     - Increases customer balance by a specified positive increment (`increment_amount >= 0.01`).
     - Request: `{ "increment_amount": 500.00 }`
     - Response (200 OK): `{ "success": true, "data": { "id": "...", "username": "...", "balance": 5500.00 } }`
     - Validation (422 Unprocessable Entity): `{ "code": "INVALID_INPUT", "message": "increment_amount must be at least 0.01" }`
   - **Set Absolute Balance:** `PUT /api/admin/users/{id}/balance`
     - Sets customer balance to an absolute new amount (`new_balance >= 0.00`).
     - Request: `{ "new_balance": 5000.00 }`
     - Response (200 OK): `{ "success": true, "data": { "id": "...", "username": "...", "balance": 5000.00 } }`
     - Validation (422 Unprocessable Entity): `{ "code": "INVALID_INPUT", "message": "balance cannot be negative" }`
   - **Legacy Balance Adjustment:** `PATCH /api/admin/users/{id}/balance`
     - Maintained for backward compatibility. Accepts `{ "amount": 500.00 }`.

3. `PUT /api/admin/users/{id}/filters`
   - Configures catalog visibility rules for a user.
   - Body:
     ```json
     {
       "allowed_categories": ["laptop"],
       "allowed_manufacturers": ["Apple", "Dell"]
     }
     ```
   - Note: An empty array `[]` removes filter restrictions and grants full catalog visibility.
   - Response (200 OK): Updated user filter profile.

4. `POST /api/admin/products`
   - Adds a new product to inventory.
   - Body:
     ```json
     {
       "category": "monitor",
       "manufacturer": "Dell",
       "model": "UltraSharp 27",
       "price": 450.00,
       "stock_quantity": 25
     }
     ```
   - Response (201 Created): Created product object with UUID.

5. `PATCH /api/admin/products/{id}/stock`
   - Updates stock level for a product.
   - Body: `{ "stock_quantity": 50 }`
   - Response (200 OK): Updated product inventory details.

6. `GET /api/admin/users`
   - Returns paginated list of users with optional role filtering (`?role=admin|user&page=1&page_size=20`).
   - Response (200 OK): Array of `UserSummary` objects.

7. `GET /api/admin/users/{id}`
   - Returns details of a specific user account.
   - Response (200 OK): `UserSummary` object.

8. `GET /api/admin/products`
   - Returns full, unrestricted product catalog for administrative inspection.
   - Response (200 OK): Array of `Product` objects.

9. `GET /api/admin/orders`
   - Returns all orders across the system for administrative auditing and fulfillment tracking.
   - Response (200 OK): Array of `OrderResponse` objects (including status and timestamps).

10. `PATCH /api/admin/orders/{id}/status`
    - Updates order fulfillment lifecycle status.
    - Body: `{ "status": "SHIPPED" }` (Valid: `CREATED`, `PROCESSING`, `SHIPPED`, `DELIVERED`, `CANCELLED`).
    - Response (200 OK): Updated `OrderResponse` object.

11. `DELETE /api/admin/users/{id}`
    - Soft-deactivates user account (`deleted_at = CURRENT_TIMESTAMP`) while permanently preserving immutable order records and financial audit trails (`ON DELETE RESTRICT`).
    - Response (200 OK): Success message.

### 5.4 User Routes (`/api/user/*`, Bearer User Token Required)

1. `GET /api/user/profile`
   - Returns authenticated user details, balance, and catalog filters.
   - Response (200 OK):
     ```json
     {
       "success": true,
       "data": {
         "id": "uuid",
         "username": "userA",
         "role": "user",
         "balance": 5000.00,
         "allowed_categories": [],
         "allowed_manufacturers": []
       }
     }
     ```

2. `GET /api/user/products`
   - Query Parameters: `category` (optional, string).
   - **Catalog Filter Evaluation Rules:**
     1. If user's `allowed_categories` is non-empty, query must only return items matching `allowed_categories`. If user requests `?category=smartphone` but is only allowed `["laptop"]`, return empty list `[]`.
     2. If user's `allowed_manufacturers` is non-empty, query must only return items matching `allowed_manufacturers`.
     3. If `category` query param is provided and allowed, filter by that category.
     4. If `category` query param is omitted, return all allowed products.
   - Response (200 OK): Array of matching product items.

3. `POST /api/user/orders` (Supports `Idempotency-Key` Header with 24-Hour TTL)
   - Places an order for a product.
   - **Headers:** `Idempotency-Key` (optional, string / UUID). Clients submitting identical order requests with the same key receive the cached `201 Created` response with `Idempotent-Replayed: true` header without double-charging balance or decrementing stock twice. Concurrent in-flight requests or conflicting payloads under the same key return `409 Conflict`.
   - Body:
     ```json
     {
       "product_id": "uuid",
       "quantity": 2
     }
     ```
   - **Atomic Transaction Workflow (ACID compliant):**
     1. `BEGIN` transaction with strict deterministic global row-level lock hierarchy: ALWAYS lock user account record first (`SELECT ... FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`), then product catalog record second (`SELECT ... FROM products WHERE id = $2 FOR UPDATE`) to eliminate cyclic wait deadlocks (`SQLSTATE 40P01`).
     2. Fetch product and verify existence.
     3. Verify product matches user's permission filters (`allowed_categories` & `allowed_manufacturers`). Return `403 Forbidden` if disallowed.
     4. Check product `stock_quantity >= quantity`. Return `400 Bad Request` if insufficient stock.
     5. Calculate `total_cost = price * quantity`.
     6. Verify user `balance >= total_cost`. Return `400 Bad Request` if insufficient balance.
     7. Deduct stock: `stock_quantity = stock_quantity - quantity`.
     8. Deduct balance: `balance = balance - total_cost`.
     9. Record entry in `orders` table.
     10. `COMMIT` transaction.
   - Response (201 Created):
     ```json
     {
       "success": true,
       "data": {
         "order_id": "uuid",
         "product_id": "uuid",
         "product_model": "MacBook Pro 16",
         "quantity": 2,
         "unit_price": 2499.00,
         "total_price": 4998.00,
         "remaining_balance": 2.00,
         "created_at": "2026-09-23T20:40:00Z"
       }
     }
     ```

4. `GET /api/user/orders`
   - Returns all historical orders placed by the authenticated customer.
   - Response (200 OK): Array of `OrderResponse` objects.

5. `GET /api/user/orders/{id}`
   - Returns details for a specific order belonging to the authenticated customer.
   - Response (200 OK): `OrderResponse` object.
   - Failure: `404 Not Found` if order does not exist or belongs to another user.

---

## 6. Seed Data & Initial State

The database initialization script seeds predefined entities for immediate testing:

### 6.1 Users
- **Admin:** `username: admin`, `password: admin123`, `role: admin`, `balance: 0.00`
- **User A (Unrestricted):** `username: userA`, `password: user123`, `role: user`, `balance: 5000.00`, `allowed_categories: []`, `allowed_manufacturers: []`
- **User B (Category Restricted):** `username: userB`, `password: user123`, `role: user`, `balance: 3000.00`, `allowed_categories: ["laptop"]`, `allowed_manufacturers: []`
- **User C (Manufacturer Restricted):** `username: userC`, `password: user123`, `role: user`, `balance: 4000.00`, `allowed_categories: []`, `allowed_manufacturers: ["Apple"]`

### 6.2 Products Catalog
- **Laptops:**
  - Apple MacBook Pro 16 (`category: laptop`, `manufacturer: Apple`, `model: MacBook Pro 16 M3`, `price: 2499.00`, `stock: 15`)
  - Dell XPS 15 (`category: laptop`, `manufacturer: Dell`, `model: XPS 15 9530`, `price: 1899.00`, `stock: 20`)
- **Smartphones:**
  - Apple iPhone 15 Pro (`category: smartphone`, `manufacturer: Apple`, `model: iPhone 15 Pro Max`, `price: 1199.00`, `stock: 30`)
  - Samsung Galaxy S24 Ultra (`category: smartphone`, `manufacturer: Samsung`, `model: Galaxy S24 Ultra`, `price: 1299.00`, `stock: 25`)
  - Xiaomi 14 Ultra (`category: smartphone`, `manufacturer: Xiaomi`, `model: Xiaomi 14 Ultra`, `price: 999.00`, `stock: 18`)

---

## 7. Deliverables & Acceptance Checklist

- [ ] Go application structured into `cmd/`, `internal/domain/`, `internal/handler/`, `internal/service/`, `internal/repository/`, `internal/middleware/`.
- [ ] Fully functional interactive Swagger UI served at `/swagger/index.html`.
- [ ] Docker Compose setup with PostgreSQL healthcheck and automatic schema migration/seeding.
- [ ] Segregated login endpoints (`/api/admin/login` and `/api/user/login`) with strict role validation.
- [ ] Admin management of users, balance top-ups, filter configuration, products, and stock updates.
- [ ] User profile retrieval and catalog browsing with strict category & manufacturer filter enforcement.
- [ ] Atomic, transaction-safe order execution verifying stock, permissions, balance, and state updates.
- [ ] Comprehensive `README.md` with startup instructions (`docker compose up`), test credentials, and Swagger endpoints.

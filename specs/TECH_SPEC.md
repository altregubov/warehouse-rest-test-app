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

### 2.3 Response Envelope & Distributed Request Tracing
All API responses follow consistent JSON envelopes and carry an end-to-end distributed tracing correlation ID (`requestId` and HTTP response header `X-Request-ID`):

**Success Envelope:**
```json
{
  "success": true,
  "data": { ... },
  "requestId": "c56a4180-65aa-42ec-a945-5fd21dec0538"
}
```
*(Or array of items in `data` for list endpoints)*

**Paginated Success Envelope (e.g. Catalog Browsing):**
```json
{
  "success": true,
  "data": [ ... ],
  "pagination": {
    "total_count": 45,
    "page": 1,
    "page_size": 20,
    "total_pages": 3
  },
  "requestId": "c56a4180-65aa-42ec-a945-5fd21dec0538"
}
```

**Error Envelope with Structured Field Violations:**
```json
{
  "success": false,
  "error": {
    "code": "INVALID_INPUT",
    "message": "Validation failed",
    "details": [
      {
        "field": "increment_amount",
        "issue": "must be at least 0.01"
      }
    ]
  },
  "requestId": "c56a4180-65aa-42ec-a945-5fd21dec0538"
}
```

### 2.4 Operational Observability & Logging
- **Distributed Request Tracing**: The `Tracing` middleware inspects the incoming `X-Request-ID` header. If absent, a cryptographically secure UUID v4 is automatically generated. The ID is stored in the request context, propagated to outbound headers via `X-Request-ID`, and automatically injected into both `SuccessEnvelope` and `ErrorEnvelope` payloads.
- **Structured JSON Logging**: Every HTTP request emits an operational JSON log record with fields `timestamp`, `level` (`INFO`, `WARN`, `ERROR`), `requestId`, `method`, `path`, `status`, `latency_ms`, and `client_ip`.

### 2.5 Standardized Error Taxonomy & Status Code Mapping
The platform adheres to strict HTTP semantic status code conventions across all endpoints:
- **`200 OK`**: Synchronous operation completed successfully, returning the requested resource or mutation confirmation.
- **`201 Created`**: Resource created successfully, returning the created entity representation and generated UUID.
- **`400 Bad Request`**: Malformed JSON syntax, invalid path parameter UUID format, query parameter syntax errors, or schema validation failures (`INVALID_REQUEST`, `INVALID_INPUT`, `INVALID_ID`).
- **`401 Unauthorized`**: Authentication failure, missing or invalid Bearer token, invalid credentials, or role-endpoint mismatch (`INVALID_CREDENTIALS`, `UNAUTHORIZED`).
- **`403 Forbidden`**: Authenticated principal does not possess sufficient role privileges (`FORBIDDEN`).
- **`404 Not Found`**: Target user, product, or order entity does not exist or has been soft-deleted (`NOT_FOUND`).
- **`409 Conflict`**: State or concurrency conflict:
  - `USERNAME_TAKEN`: Attempting to register an account with a username that already exists.
  - `IDEMPOTENCY_CONFLICT`: Concurrent in-flight request executing under the same idempotency key or conflicting request payload for an existing key.
- **`422 Unprocessable Entity`**: Domain business rule and semantic validation failures:
  - `INSUFFICIENT_FUNDS`: Account balance is lower than total purchase price.
  - `INSUFFICIENT_STOCK`: Warehouse stock is less than requested quantity.
  - `FILTER_RESTRICTION`: Product is outside user's whitelist/filter access, or user has zero-access governance.
  - `INVALID_STATUS`: Disallowed order status lifecycle transition.
  - `INVALID_INPUT`: Domain boundary validation breach (e.g. `increment_amount < 0.01` or `new_balance < 0.00`).
- **`500 Internal Server Error`**: Unexpected database errors, unhandled panic recovery, or persistence failures (`INTERNAL_ERROR`).
- **`503 Service Unavailable`**: Infrastructure outages, database connectivity loss, maintenance mode, or temporary upstream dependency degradation (`SERVICE_UNAVAILABLE`).

### 2.6 OpenAPI Schema Validation & Model Constraints
To ensure client SDK predictability and prevent unhandled database violations:
- **Required Fields**: All request and response DTOs define explicit `required` property arrays in OpenAPI definitions via Go struct validation binding (`binding:"required"`).
- **UUID Formatting**: All entity identifiers, foreign keys, and `{id}` path parameters strictly enforce `"format": "uuid"` (validated using standard RFC 4122 UUID parser).
- **Date-Time Formatting**: Timestamp fields (`created_at`, `updated_at`, `deleted_at`) enforce `"format": "date-time"` (RFC 3339).
- **Role Enums**: Role fields and parameters enforce enumerated values: `["admin", "user"]`.
- **Clean Array Examples**: Array properties (`allowed_categories`, `allowed_manufacturers`) define native JSON array examples (`["laptop"]`, `["Apple", "Dell"]`) rather than escaped string literals, ensuring Swagger UI "Try It Out" and automated contract generators populate valid request bodies out-of-the-box.
- **Numeric Boundaries**:
  - `quantity`: `minimum: 1`
  - `stock_quantity`, `price`, `balance`, `new_balance`: `minimum: 0`
  - `increment_amount`: `minimum: 0.01`
- **String Length Constraints**:
  - `username`: `minLength: 1`
  - `password`: `minLength: 4`
All boundary or type violations are caught at the HTTP handler layer and rejected with `400 Bad Request` (`INVALID_INPUT` / `INVALID_REQUEST`) before invoking backend services or touching the database.

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
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    product_model VARCHAR(150) NOT NULL,
    unit_price NUMERIC(12, 2) NOT NULL CHECK (unit_price >= 0),
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
1. **User Catalog Filters & Access Level Semantics:**
   - **Case-Insensitive Normalization:** Category and manufacturer filtering operates strictly case-insensitively across ingestion (`CreateUser`, `UpdateFilters`) and query evaluation (`LOWER(TRIM(...))`), preventing silent mismatches between title-cased catalog entries and lowercase whitelist queries.
   - **Access Level Enum & Denial Semantics:**
     - `access_level`: Explicit enumerated values (`ALL`, `FILTERED`, `NONE`).
     - `catalog_access_enabled`: Boolean flag indicating whether the user has catalog and order placement privileges.
     - **`ALL`**: User has full catalog visibility across all categories and manufacturers.
     - **`FILTERED`**: Visibility and order placement are strictly constrained to whitelisted `allowed_categories` and `allowed_manufacturers`.
     - **`NONE`**: Explicit zero-access configuration. The user sees 0 catalog items (`[]`), and order attempts are immediately rejected with `422 Unprocessable Entity` (`FILTER_RESTRICTION`), enabling suspension or onboarding holds without deleting user accounts.
2. **Product Extensibility:**
   - Category is an arbitrary, non-restricted string (e.g., `laptop`, `smartphone`, `monitor`, `tablet`, `accessory`). No hardcoded enums.
3. **Atomic Balance & Inventory Constraints:**
   - Balances and stock quantities must never be negative.
4. **Monetary Representation & Precision Guardrails:**
   - **Internal Calculation:** All financial computations (balance adjustments, checkout debiting, line-item totals) are executed strictly in integer cents (`int64`, minor currency units) using `DollarsToCents` and `CentsToDollars` conversions, eliminating binary floating-point rounding errors and off-by-one-cent ledger drift.
   - **API Transport:** The REST API accepts and serializes monetary fields in dollars (`format: "double"`, 2 decimal digits) for client convenience and backward compatibility.
   - **Database Persistence:** Persisted in PostgreSQL as `NUMERIC(12, 2)` to ensure strict exact-decimal ledger integrity.
5. **Historical Snapshot Immutability:**
   - When an order is placed, the product's current model and unit price are permanently snapshotted into `orders.product_model` and `orders.unit_price`.
   - Subsequent modifications to product prices or catalog descriptions do not alter historical orders, ensuring immutable receipts for financial audits.

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
  - Failure (400 Bad Request): `{ "success": false, "error": {"code": "INVALID_REQUEST", "message": "Failed to parse JSON body"} }`
  - Failure (401 Unauthorized): `{ "success": false, "error": {"code": "INVALID_CREDENTIALS", "message": "Invalid username or password"} }`
  - Failure (500 Internal Server Error): `{ "success": false, "error": {"code": "INTERNAL_ERROR", "message": "Internal server error"} }`
- `POST /api/user/login`
  - Body: `{ "username": "userA", "password": "user123" }`
  - Response (200 OK): `{ "success": true, "data": { "token": "<jwt>", "user": { "id": "...", "username": "userA", "role": "user" } } }`
  - Failure (400 Bad Request): `{ "success": false, "error": {"code": "INVALID_REQUEST", "message": "Failed to parse JSON body"} }`
  - Failure (401 Unauthorized): `{ "success": false, "error": {"code": "INVALID_CREDENTIALS", "message": "Invalid username or password"} }`
  - Failure (500 Internal Server Error): `{ "success": false, "error": {"code": "INTERNAL_ERROR", "message": "Internal server error"} }`

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
       "allowed_manufacturers": ["Dell"],
       "access_level": "FILTERED",
       "catalog_access_enabled": true
     }
     ```
   - Response (201 Created): User details (excluding `password_hash`).
   - Failure (400 Bad Request): Malformed JSON (`INVALID_REQUEST`) or validation constraint violation (`INVALID_INPUT`).
   - Failure (401 Unauthorized): Missing or invalid Bearer token.
   - Failure (403 Forbidden): Caller does not have administrative privileges.
   - Failure (409 Conflict): `{ "code": "USERNAME_TAKEN", "message": "Username already exists" }`.
   - Failure (500 Internal Server Error): Database persistence failure.

2. Balance Management Operations (Supports `Idempotency-Key` Header with 24-Hour TTL)
   - **Header Support:** Clients may provide `Idempotency-Key: <uuid-or-string>` on all balance mutation endpoints. Identical replayed requests return the original cached response with `Idempotent-Replayed: true` header. Requests with conflicting payloads or concurrent in-flight executions under the same key return `409 Conflict`.
   - **Top-Up Balance (Relative Increment):** `POST /api/admin/users/{id}/balance/top-up`
     - Increases customer balance by a specified positive increment (`increment_amount >= 0.01`).
     - Request: `{ "increment_amount": 500.00 }`
     - Response (200 OK): `{ "success": true, "data": { "id": "...", "username": "...", "balance": 5500.00 } }`
     - Failure (400 Bad Request): Invalid user ID format (`INVALID_ID`) or malformed JSON (`INVALID_REQUEST`).
     - Failure (401 Unauthorized): Missing or invalid token.
     - Failure (403 Forbidden): Insufficient admin privileges.
     - Failure (404 Not Found): Target user account does not exist or has been deactivated (`NOT_FOUND`).
     - Failure (409 Conflict): Concurrent request in flight or conflicting payload for key (`IDEMPOTENCY_CONFLICT`).
     - Failure (422 Unprocessable Entity): `{ "code": "INVALID_INPUT", "message": "increment_amount must be at least 0.01" }`.
     - Failure (500 Internal Server Error): Server or persistence failure.
   - **Set Absolute Balance:** `PUT /api/admin/users/{id}/balance`
     - Sets customer balance to an absolute new amount (`new_balance >= 0.00`).
     - Request: `{ "new_balance": 5000.00 }`
     - Response (200 OK): `{ "success": true, "data": { "id": "...", "username": "...", "balance": 5000.00 } }`
     - Failure (400 Bad Request): Invalid user ID format (`INVALID_ID`) or malformed JSON (`INVALID_REQUEST`).
     - Failure (401 Unauthorized): Missing or invalid token.
     - Failure (403 Forbidden): Insufficient admin privileges.
     - Failure (404 Not Found): Target user account does not exist or has been deactivated (`NOT_FOUND`).
     - Failure (409 Conflict): Concurrent request in flight or conflicting payload for key (`IDEMPOTENCY_CONFLICT`).
     - Failure (422 Unprocessable Entity): `{ "code": "INVALID_INPUT", "message": "balance cannot be negative" }`.
     - Failure (500 Internal Server Error): Server or persistence failure.
   - **Legacy Balance Adjustment:** `PATCH /api/admin/users/{id}/balance`
     - Maintained for backward compatibility. Accepts `{ "amount": 500.00 }`.
     - Response (200 OK): Updated user details.
     - Failure (400 Bad Request): Invalid user ID format or malformed request.
     - Failure (401 Unauthorized): Missing or invalid token.
     - Failure (403 Forbidden): Insufficient admin privileges.
     - Failure (404 Not Found): Target user does not exist (`NOT_FOUND`).
     - Failure (409 Conflict): Idempotency key conflict (`IDEMPOTENCY_CONFLICT`).
     - Failure (422 Unprocessable Entity): Input validation failure.
     - Failure (500 Internal Server Error): Persistence failure.

3. `PUT /api/admin/users/{id}/filters`
   - Configures catalog visibility rules and access tier for a user.
   - Body:
     ```json
     {
       "allowed_categories": ["laptop"],
       "allowed_manufacturers": ["Apple", "Dell"],
       "access_level": "FILTERED",
       "catalog_access_enabled": true
     }
     ```
   - Response (200 OK): Updated user filter profile.
   - Failure (400 Bad Request): Invalid UUID format (`INVALID_ID`) or malformed JSON (`INVALID_REQUEST`).
   - Failure (401 Unauthorized): Missing or invalid token.
   - Failure (403 Forbidden): Insufficient admin privileges.
   - Failure (404 Not Found): User not found (`NOT_FOUND`).
   - Failure (500 Internal Server Error): Persistence failure.

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
   - Failure (400 Bad Request): Malformed JSON (`INVALID_REQUEST`) or validation constraint violation (`INVALID_INPUT`).
   - Failure (401 Unauthorized): Missing or invalid token.
   - Failure (403 Forbidden): Insufficient admin privileges.
   - Failure (500 Internal Server Error): Persistence failure.

5. `PATCH /api/admin/products/{id}/stock`
   - Updates stock level for a product.
   - Body: `{ "stock_quantity": 50 }`
   - Response (200 OK): Updated product inventory details.
   - Failure (400 Bad Request): Invalid product UUID (`INVALID_ID`) or malformed JSON (`INVALID_REQUEST`).
   - Failure (401 Unauthorized): Missing or invalid token.
   - Failure (403 Forbidden): Insufficient admin privileges.
   - Failure (404 Not Found): Product not found (`NOT_FOUND`).
   - Failure (500 Internal Server Error): Persistence failure.

6. `GET /api/admin/users`
   - Returns paginated list of users with optional role filtering (`?role=admin|user&page=1&page_size=20`).
   - Response (200 OK): Array of `UserSummary` objects.
   - Failure (400 Bad Request): Invalid query parameters (`INVALID_INPUT`).
   - Failure (401 Unauthorized): Missing or invalid token.
   - Failure (403 Forbidden): Insufficient admin privileges.
   - Failure (500 Internal Server Error): Persistence failure.

7. `GET /api/admin/users/{id}`
   - Returns details of a specific user account.
   - Response (200 OK): `UserSummary` object.
   - Failure (400 Bad Request): Invalid user UUID (`INVALID_ID`).
   - Failure (401 Unauthorized): Missing or invalid token.
   - Failure (403 Forbidden): Insufficient admin privileges.
   - Failure (404 Not Found): Target user does not exist (`NOT_FOUND`).
   - Failure (500 Internal Server Error): Persistence failure.

8. `GET /api/admin/products`
   - Returns full, unrestricted product catalog for administrative inspection.
   - Response (200 OK): Array of `Product` objects.
   - Failure (401 Unauthorized): Missing or invalid token.
   - Failure (403 Forbidden): Insufficient admin privileges.
   - Failure (500 Internal Server Error): Persistence failure.

9. `GET /api/admin/orders`
   - Returns all orders across the system for administrative auditing and fulfillment tracking.
   - Response (200 OK): Array of `OrderResponse` objects (including status and timestamps).
   - Failure (401 Unauthorized): Missing or invalid token.
   - Failure (403 Forbidden): Insufficient admin privileges.
   - Failure (500 Internal Server Error): Persistence failure.

10. `PATCH /api/admin/orders/{id}/status`
    - Updates order fulfillment lifecycle status.
    - Body: `{ "status": "SHIPPED" }` (Valid: `CREATED`, `PROCESSING`, `SHIPPED`, `DELIVERED`, `CANCELLED`).
    - Response (200 OK): Updated `OrderResponse` object.
    - Failure (400 Bad Request): Invalid order UUID (`INVALID_ID`) or malformed JSON (`INVALID_REQUEST`).
    - Failure (401 Unauthorized): Missing or invalid token.
    - Failure (403 Forbidden): Insufficient admin privileges.
    - Failure (404 Not Found): Target order not found (`NOT_FOUND`).
    - Failure (422 Unprocessable Entity): Invalid status transition (`INVALID_STATUS`).
    - Failure (500 Internal Server Error): Persistence failure.

11. `DELETE /api/admin/users/{id}`
    - Soft-deactivates user account (`deleted_at = CURRENT_TIMESTAMP`) while permanently preserving immutable order records and financial audit trails (`ON DELETE RESTRICT`).
    - Response (200 OK): Success message.
    - Failure (400 Bad Request): Invalid user UUID (`INVALID_ID`).
    - Failure (401 Unauthorized): Missing or invalid token.
    - Failure (403 Forbidden): Insufficient admin privileges.
    - Failure (404 Not Found): Target user not found (`NOT_FOUND`).
    - Failure (500 Internal Server Error): Persistence failure.

### 5.4 User Routes (`/api/user/*`, Bearer User Token Required)

1. `GET /api/user/profile`
   - Returns authenticated user details, balance, access level, and catalog filters.
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
         "allowed_manufacturers": [],
         "access_level": "ALL",
         "catalog_access_enabled": true
       }
     }
     ```
   - Failure (401 Unauthorized): Missing or invalid token.
   - Failure (403 Forbidden): Forbidden.
   - Failure (404 Not Found): User account not found or deactivated (`NOT_FOUND`).
   - Failure (500 Internal Server Error): Server error.

2. `GET /api/user/products`
   - **Query Parameters:**
     - `page` (optional, integer, default: `1`, minimum: `1`): Target page number.
     - `page_size` / `limit` (optional, integer, default: `20`, minimum: `1`, maximum: `100`): Items returned per page.
     - `sort_by` (optional, string, enum: `price`, `created_at`, `model`, default: `created_at`): Sort field.
     - `order` (optional, string, enum: `asc`, `desc`, default: `asc`): Sort direction.
     - `category` (optional, string): Filter by product classification.
     - `manufacturer` (optional, string): Filter by brand / manufacturer.
   - **Catalog Filter Evaluation Rules:**
     1. If user's `access_level` is `NONE` or `catalog_access_enabled` is `false`, return empty list `[]` (`total_count: 0`, `total_pages: 0`).
     2. If user's `access_level` is `FILTERED` and `allowed_categories` is non-empty, query matches case-insensitively using `LOWER(TRIM(...))`.
     3. If user's `access_level` is `FILTERED` and `allowed_manufacturers` is non-empty, query matches case-insensitively using `LOWER(TRIM(...))`.
     4. If `category` query param is provided, filter by that category case-insensitively within permitted bounds.
     5. If `manufacturer` query param is provided, filter by that brand case-insensitively within permitted bounds.
     6. If user has full access (`access_level: ALL`), return all products matching optional category and manufacturer.
     7. Results are sorted deterministically by the requested field and order (with `id ASC` as tie-breaker).
     8. Total matching count is calculated, and results are sliced by `LIMIT page_size OFFSET (page - 1) * page_size`.
   - **Response (200 OK):**
     ```json
     {
       "success": true,
       "data": [
         {
           "id": "c0000000-0000-0000-0000-000000000001",
           "category": "laptop",
           "manufacturer": "Apple",
           "model": "MacBook Pro 16 M3",
           "price": 2499.00,
           "stock_quantity": 15,
           "created_at": "2026-09-23T20:00:00Z",
           "updated_at": "2026-09-23T20:00:00Z"
         }
       ],
       "pagination": {
         "total_count": 1,
         "page": 1,
         "page_size": 20,
         "total_pages": 1
       }
     }
     ```
   - Failure (400 Bad Request): Invalid pagination boundaries (`page < 1`, `page_size < 1` or `> 100`) or unsupported `sort_by`/`order` values (`INVALID_INPUT`).
   - Failure (401 Unauthorized): Missing or invalid token.
   - Failure (403 Forbidden): Forbidden.
   - Failure (404 Not Found): User account not found (`NOT_FOUND`).
   - Failure (500 Internal Server Error): Persistence error.

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
   - Failure (400 Bad Request): Malformed JSON (`INVALID_REQUEST`) or quantity <= 0 (`INVALID_INPUT`).
   - Failure (401 Unauthorized): Missing or invalid token.
   - Failure (403 Forbidden): Forbidden.
   - Failure (404 Not Found): Product SKU or purchasing user not found (`NOT_FOUND`).
   - Failure (409 Conflict): Idempotency collision or concurrent request in flight (`IDEMPOTENCY_CONFLICT`).
   - Failure (422 Unprocessable Entity): Domain rule violation:
     - `FILTER_RESTRICTION`: Product is outside user's whitelist/access level.
     - `INSUFFICIENT_STOCK`: Product stock is less than requested quantity.
     - `INSUFFICIENT_FUNDS`: Account balance is insufficient for purchase.
   - Failure (500 Internal Server Error): Persistence failure.

4. `GET /api/user/orders`
   - Returns all historical orders placed by the authenticated customer.
   - Response (200 OK): Array of `OrderResponse` objects.
   - Failure (401 Unauthorized): Missing or invalid token.
   - Failure (403 Forbidden): Forbidden.
   - Failure (500 Internal Server Error): Persistence error.

5. `GET /api/user/orders/{id}`
   - Returns details for a specific order belonging to the authenticated customer.
   - Response (200 OK): `OrderResponse` object.
   - Failure (400 Bad Request): Invalid order UUID format (`INVALID_ID`).
   - Failure (401 Unauthorized): Missing or invalid token.
   - Failure (403 Forbidden): Forbidden.
   - Failure (404 Not Found): Order does not exist or belongs to another user (`NOT_FOUND`).
   - Failure (500 Internal Server Error): Persistence error.

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

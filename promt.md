Act as a Senior Go Backend Engineer. Build a clean, lightweight, and highly readable Warehouse REST API testbench designed for API testing, interactive Swagger UI exploration, and future frontend integration.

### 1. Tech Stack & Architecture
- **Language:** Go (idiomatic, clean architecture, minimal routing like `chi` or `gin`).
- **Database:** PostgreSQL running in Docker (`docker-compose.yml` included with healthcheck and auto-migration/init script).
- **API Documentation & Swagger UI:** 
  - Fully functional, interactive **Swagger UI** served directly by the Go backend (accessible at `http://localhost:<PORT>/swagger/index.html` via `swaggo/http-swagger` or embedded OpenAPI spec).
  - All endpoints, authentication schemes, schemas, query parameters, and error models must be fully annotated and executable directly from Swagger UI.
- **Architecture:** Simple, modular, and readable layered structure: `handlers/`, `service/` (business logic), `repository/` (database queries/models), `middleware/`.
- **Frontend Readiness:** Enable CORS middleware, use standard JSON response envelopes, and return explicit HTTP status codes and error payloads.

---

### 2. Domain Models & Database Schema

1. **User:**
   - `id` (UUID or Serial)
   - `username`, `password_hash`
   - `role` (`admin` | `user`)
   - `balance` (numeric/decimal, default: 0.00)
   - **Admin-assigned Permissions / Filters (per user):**
     - `allowed_categories`: array of strings (`text[]` in Postgres). **If empty or null, all categories are allowed by default.**
     - `allowed_manufacturers`: array of strings (`text[]` in Postgres). **If empty or null, all manufacturers are allowed by default.**

2. **Product (Warehouse Item):**
   - Extensible, open category model (do not hardcode enums or restrict categories to two types; new categories can be added dynamically at any time).
   - Initial catalog focuses on `laptop` and `smartphone`, but allows any category (e.g. `tablet`, `monitor`, `accessory`).
   - Fields:
     - `id` (UUID or Serial)
     - `category` (string, e.g., "laptop", "smartphone")
     - `manufacturer` (string)
     - `model` (string)
     - `price` (numeric/decimal)
     - `stock_quantity` (integer)

3. **Order / Transaction Log:**
   - `id`, `user_id`, `product_id`, `quantity`, `total_price`, `created_at`

---

### 3. Authentication & RBAC

- JWT or Bearer token authentication.
- **Separate Login Endpoints with Role Validation:**
  - `POST /api/admin/login`: Accepts `{ "username": "...", "password": "..." }`. Validates credentials and strictly verifies that `role == 'admin'`. Returns HTTP 403 Forbidden if the user is not an admin. Returns JWT token with admin claims on success.
  - `POST /api/user/login`: Accepts `{ "username": "...", "password": "..." }`. Validates credentials and strictly verifies that `role == 'user'`. Returns HTTP 403 Forbidden if an admin attempts to log in here. Returns JWT token with user claims on success.
- Middleware to enforce role-based access control (`admin` vs `user`) on protected routes.
- Authorize button configured in Swagger UI (Bearer token) for interactive testing.

---

### 4. API Endpoints

#### Documentation Endpoint
- `GET /swagger/*` — Interactive Swagger UI to inspect and execute all requests.

#### Auth Endpoints
- `POST /api/admin/login` — Authenticate an admin user and return a JWT token.
- `POST /api/user/login` — Authenticate a regular user and return a JWT token.

#### Admin Endpoints (`/api/admin/...`, requires Admin JWT)
- `POST /api/admin/users` — Create a user (can create both `admin` and regular `user` accounts).
- `PATCH /api/admin/users/{id}/balance` — Top up or modify a user's balance (`{ "amount": 500.00 }`).
- `PUT /api/admin/users/{id}/filters` — Configure catalog access filters for a user:
  - `allowed_categories`: list of allowed category strings (`["laptop"]`, `["smartphone"]`, or `[]` for unrestricted).
  - `allowed_manufacturers`: list of allowed brands (`["Apple", "Dell"]`, or `[]` for unrestricted).
- `POST /api/admin/products` — Add a new product to the warehouse (`category`: arbitrary string, `manufacturer`, `model`, `price`, `stock_quantity`).
- `PATCH /api/admin/products/{id}/stock` — Update stock quantity for an item.

#### User Endpoints (`/api/user/...`, requires User JWT)
- `GET /api/user/profile` — Get authenticated user details, current balance, and assigned filter rules.
- `GET /api/user/products?category={category}` — Browse available warehouse products:
  - **Query parameter:** `category` is optional.
    - If `category` is provided (e.g., `?category=laptop`), filter results by that category.
    - If `category` is omitted or empty, return products across all categories.
  - **Strict Filter Enforcement:**
    - If `allowed_categories` is empty/null, the user can view products in all categories.
    - If `allowed_categories` has specific values, products outside these allowed categories must never be returned. If the requested `?category=` is not in the user's whitelist, return an empty list `[]`.
    - If `allowed_manufacturers` has specific values, products from other brands must be excluded. If empty/null, all brands are visible.
- `POST /api/user/orders` — Purchase products from the warehouse:
  - Request body: `{ "product_id": ..., "quantity": ... }`
  - **Business logic (Must execute inside an atomic DB transaction):**
    1. Verify that the product exists and has sufficient `stock_quantity`.
    2. Check that the product matches the user's assigned visibility/purchase filters (`allowed_categories` and `allowed_manufacturers`).
    3. Calculate total cost (`price * quantity`) and verify that user's `balance >= total_cost`.
    4. Decrement `stock_quantity` in `products`.
    5. Deduct `total_cost` from user's `balance`.
    6. Insert record into `orders`.
    7. Commit transaction and return order summary.

---

### 5. Deliverables Required
1. Complete Go project source code with clean, readable structure.
2. Complete Swagger setup with interactive **Swagger UI** served at `/swagger/index.html` with Bearer token authentication support.
3. `docker-compose.yml` configuring PostgreSQL and the Go backend service.
4. Database initialization/migration script with seed data:
   - 1 Admin user (`admin` / `admin123`).
   - Regular users demonstrating different filter states:
     - User A: Default/unrestricted (`allowed_categories: []`, `allowed_manufacturers: []`, balance: 5000.00).
     - User B: Restricted to `["laptop"]` only.
     - User C: Restricted to specific brand (e.g., `allowed_manufacturers: ["Apple"]`).
   - Diverse warehouse catalog (laptops and smartphones from brands like Apple, Dell, Samsung, Xiaomi).
5. `README.md` with instructions to launch with `docker compose up`, the exact URL to access Swagger UI, and test credentials.

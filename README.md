# Warehouse REST API Testbench

<p align="center">
  <img src="https://img.shields.io/badge/Purpose-Educational%20Testbench-orange?style=for-the-badge&logo=mortarboard" alt="Educational Purpose" />
  <img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go" />
  <img src="https://img.shields.io/badge/PostgreSQL-16-336791?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL" />
  <img src="https://img.shields.io/badge/Docker-Compose-2496ED?style=for-the-badge&logo=docker&logoColor=white" alt="Docker Compose" />
  <img src="https://img.shields.io/badge/Swagger-OpenAPI-85EA2D?style=for-the-badge&logo=swagger&logoColor=black" alt="Swagger" />
</p>

> [!NOTE]
> This codebase was fully produced by AI (**Gemini 3.8 flash medium + Antigravity 2.0**).

---

### 🎓 Educational Purpose & Interactive Testbench

> **This application is crafted specifically for educational purposes as a realistic, feature-complete sandbox for REST API testing, test automation training, and QA exploration.**

Whether you are learning modern API testing techniques, teaching automated testing, or building test suites, this testbench provides real-world scenarios ready out of the box:
- 🔍 **Interactive Exploration:** Live Swagger UI for manual requests, schema discovery, and interactive Bearer authorization.
- 🔐 **Authentication & RBAC:** Multi-role access control (`admin` vs. `user`) with segregated login routes and strict HTTP 403 enforcement.
- 🎯 **Filter & Permission Rules:** Per-user whitelist filters (`allowed_categories`, `allowed_manufacturers`) to test access control edge cases.
- ⚡ **Transactional Workflows:** Atomic purchases with balance deduction and inventory decrement under concurrency.
- 🧪 **Ideal Automation Target:** Perfect target for practicing with **Postman**, **Newman**, **Playwright**, **REST Assured**, **pytest**, or **Cypress**.

---

## Features

- **Idiomatic Go Architecture:** Layered structure (`cmd/`, `internal/domain`, `internal/handler`, `internal/service`, `internal/repository`, `internal/middleware`).
- **Interactive Swagger UI:** Embedded Swagger UI served directly at `http://localhost:8080/swagger/index.html` with Bearer token authentication support.
- **Segregated Authentication & RBAC:**
  - Dedicated `/api/admin/login` and `/api/user/login` endpoints enforcing strict role separation (mismatched roles return HTTP 403 Forbidden).
  - Role-based route middleware protecting `/api/admin/*` and `/api/user/*`.
- **Granular Catalog Access Control:** Per-user whitelist filters for `allowed_categories` and `allowed_manufacturers` (empty/null grants unrestricted access).
- **Atomic Order Placement:** ACID-compliant PostgreSQL transaction with row-level locking (`SELECT ... FOR UPDATE`), ensuring stock and balance integrity under concurrent requests.
- **Docker Compose Setup:** One-command startup with PostgreSQL healthcheck and automatic schema migration/seeding.

---

## Quick Start

### 1. Prerequisites
- Docker and Docker Compose

### 2. Launch with Docker Compose
```bash
docker compose up -d --build
```

The database will initialize automatically with migrations and seed data, and the backend service will start listening on port `8080`.

### 3. Access Swagger UI
Open your browser and navigate to:
```
http://localhost:8080/swagger/index.html
```

Use the **Authorize** button in Swagger UI to paste your Bearer token (`Bearer <token>`) for interactive endpoint testing.

### 4. Recreation
```bash
# 1. Stop containers and destroy the persistent volume
docker compose down -v

# 2. Start services fresh (PostgreSQL will automatically execute scripts/init.sql)
docker compose up -d
```
---

## Pre-seeded Test Credentials

The database is initialized with the following test accounts:

| Username | Password | Role | Balance | Allowed Categories | Allowed Manufacturers | Description |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| `admin` | `admin123` | `admin` | 0.00 | All | All | System administrator |
| `userA` | `user123` | `user` | 5000.00 | All (`[]`) | All (`[]`) | Unrestricted regular user |
| `userB` | `user123` | `user` | 3000.00 | `["laptop"]` | All (`[]`) | Category restricted |
| `userC` | `user123` | `user` | 4000.00 | All (`[]`) | `["Apple"]` | Brand restricted |

### Initial Catalog Items
- **Apple MacBook Pro 16 M3** (`laptop`, Apple, $2499.00, Stock: 15)
- **Dell XPS 15 9530** (`laptop`, Dell, $1899.00, Stock: 20)
- **Apple iPhone 15 Pro Max** (`smartphone`, Apple, $1199.00, Stock: 30)
- **Samsung Galaxy S24 Ultra** (`smartphone`, Samsung, $1299.00, Stock: 25)
- **Xiaomi 14 Ultra** (`smartphone`, Xiaomi, $999.00, Stock: 18)

---

## API Endpoints Summary

### Documentation
- `GET /swagger/*` — Interactive Swagger UI and OpenAPI documentation

### Authentication
- `POST /api/admin/login` — Authenticate an admin user (strictly verifies admin role)
- `POST /api/user/login` — Authenticate a regular user (strictly verifies user role)

### Admin Endpoints (Requires Admin Bearer Token)
- `POST /api/admin/users` — Create admin or regular user accounts
- `PATCH /api/admin/users/{id}/balance` — Adjust or top up user balance
- `PUT /api/admin/users/{id}/filters` — Update user catalog permissions (`allowed_categories`, `allowed_manufacturers`)
- `POST /api/admin/products` — Add a new product to inventory
- `PATCH /api/admin/products/{id}/stock` — Update stock quantity for a product

### User Endpoints (Requires User Bearer Token)
- `GET /api/user/profile` — View authenticated user profile, balance, and filter rules
- `GET /api/user/products?category={category}` — Browse catalog with strict filter enforcement
- `POST /api/user/orders` — Atomically purchase products

---

## Running Automated Tests

To run the end-to-end integration test suite against the running service:

```bash
go test -v ./tests/...
```

All tests verify Swagger endpoints, authentication separation, RBAC guards, category/manufacturer filtering, and atomic transactional ordering.

---

## Security & Configuration Guidelines

### JWT Algorithm Pinning & Claims Verification
- **Algorithm:** Exclusively pinned to `HS256` (HMAC with SHA-256) to prevent algorithm-confusion and `alg: none` exploits.
- **Claims:** Verifies `iss` (`warehouse-api`), `aud` (`warehouse-clients`), and expiration bounded to 24 hours.

### Password Security & Complexity Policy
- **Hashing:** User passwords are encrypted using **bcrypt** with a computational work factor $\ge 12$.
- **Anti-Enumeration:** Constant-time dummy hash evaluations prevent user enumeration and timing attacks.
- **Complexity:** All passwords require a minimum of 8 characters (`minLength: 8`) and must contain both alphabetic letters and numbers.

### Secret Key Provisioning & Zero-Downtime Rotation
- **Provisioning:** Production deployments must generate a cryptographically strong 256-bit secret via:
  ```bash
  openssl rand -hex 32
  ```
  Pass this secret via the `JWT_SECRET` environment variable.
- **Rotation Procedure:** Use a phased rollover protocol with an overlapping 24-hour expiration window:
  1. Set the new key as primary for issuing new tokens.
  2. Accept both old and new keys for validation during the 24-hour window.
  3. Retire the old key after 24 hours without disrupting active customer sessions.

---

## Project Structure

```
├── cmd/
│   └── server/          # Application entrypoint (main.go)
├── docs/                # Generated Swagger OpenAPI specifications
├── internal/
│   ├── config/          # Environment configuration loader
│   ├── domain/          # Domain models, request/response DTOs, errors
│   ├── handler/         # HTTP handlers and JSON envelope helpers
│   ├── middleware/      # JWT auth, RBAC, and CORS middleware
│   ├── repository/      # Database queries and atomic transaction logic
│   └── service/         # Business logic and validation services
├── scripts/
│   └── init.sql         # PostgreSQL schema and seed dataset
├── tests/
│   └── api_e2e_test.go  # End-to-end integration tests
├── docker-compose.yml   # Multi-container orchestration
├── Dockerfile           # Multi-stage production container build
├── specs/               # Technical and client specifications
└── README.md            # Setup and user guide
```

## License

This project is licensed under the **PolyForm Noncommercial License 1.0.0**.

* **Free for Noncommercial Use:** You are free to use, modify, and share this project for personal, educational, or other noncommercial purposes.
* **Commercial Use:** Commercial use (including using this software within a commercial product, service, or company) requires a separate commercial license.

For commercial licensing inquiries, please contact: `al.tregybov@gmail.com`

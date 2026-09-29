package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/altregubov/warehouse-rest-test-app/docs"
	"github.com/altregubov/warehouse-rest-test-app/internal/config"
	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
	"github.com/altregubov/warehouse-rest-test-app/internal/handler"
	"github.com/altregubov/warehouse-rest-test-app/internal/middleware"
	"github.com/altregubov/warehouse-rest-test-app/internal/repository"
	"github.com/altregubov/warehouse-rest-test-app/internal/service"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

// @title Warehouse REST API Testbench
// @version 1.0
// @description Clean, lightweight, and idiomatic Go backend for warehouse inventory and transactional ordering.
// @host localhost:8080
// @BasePath /
// @schemes http https

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token. Example: "Bearer ey..."
func main() {
	cfg := config.Load()

	log.Printf("Connecting to PostgreSQL at %s:%s (db: %s)...", cfg.DBHost, cfg.DBPort, cfg.DBName)
	db, err := repository.NewDB(cfg.DSN())
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	log.Println("Ensuring schema migrations and seed data are applied...")
	if err := repository.EnsureSchemaAndSeed(db); err != nil {
		log.Fatalf("Failed to run migrations/seed: %v", err)
	}

	// Repositories
	userRepo := repository.NewUserRepository(db)
	prodRepo := repository.NewProductRepository(db)
	orderRepo := repository.NewOrderRepository(db)
	idempotencyRepo := repository.NewIdempotencyRepository(db)

	// Services
	authService := service.NewAuthService(userRepo, cfg.JWTSecret)
	userService := service.NewUserService(userRepo)
	prodService := service.NewProductService(prodRepo, userRepo)
	orderService := service.NewOrderService(orderRepo)

	// Handlers
	authHandler := handler.NewAuthHandler(authService)
	adminHandler := handler.NewAdminHandler(userService, prodService, orderService)
	userHandler := handler.NewUserHandler(userService, prodService, orderService)

	// Middlewares
	idempotencyMiddleware := middleware.Idempotency(idempotencyRepo)

	// Router
	r := chi.NewRouter()

	// Global Middlewares
	r.Use(middleware.Tracing())
	r.Use(chimiddleware.RealIP)
	r.Use(middleware.StructuredLogger())
	r.Use(middleware.Recoverer())
	r.Use(middleware.CORS())

	// Swagger UI
	r.Get("/swagger/*", httpSwagger.Handler(
		httpSwagger.URL("/swagger/doc.json"),
	))

	// Auth Endpoints
	r.Post("/api/admin/login", authHandler.AdminLogin)
	r.Post("/api/user/login", authHandler.UserLogin)

	// Admin Endpoints
	r.Group(func(adminRouter chi.Router) {
		adminRouter.Use(middleware.AuthMiddleware(authService))
		adminRouter.Use(middleware.RequireRole(domain.RoleAdmin))

		adminRouter.Get("/api/admin/users", adminHandler.ListUsers)
		adminRouter.Get("/api/admin/users/{id}", adminHandler.GetUser)
		adminRouter.Delete("/api/admin/users/{id}", adminHandler.DeleteUser)
		adminRouter.Post("/api/admin/users", adminHandler.CreateUser)
		adminRouter.With(idempotencyMiddleware).Post("/api/admin/users/{id}/balance/top-up", adminHandler.TopUpBalance)
		adminRouter.With(idempotencyMiddleware).Put("/api/admin/users/{id}/balance", adminHandler.SetBalance)
		adminRouter.With(idempotencyMiddleware).Patch("/api/admin/users/{id}/balance", adminHandler.UpdateBalance)
		adminRouter.Put("/api/admin/users/{id}/filters", adminHandler.UpdateFilters)
		adminRouter.Get("/api/admin/products", adminHandler.ListProducts)
		adminRouter.Post("/api/admin/products", adminHandler.CreateProduct)
		adminRouter.Patch("/api/admin/products/{id}/stock", adminHandler.UpdateStock)
		adminRouter.Get("/api/admin/orders", adminHandler.ListOrders)
		adminRouter.Patch("/api/admin/orders/{id}/status", adminHandler.UpdateOrderStatus)
	})

	// User Endpoints
	r.Group(func(userRouter chi.Router) {
		userRouter.Use(middleware.AuthMiddleware(authService))
		userRouter.Use(middleware.RequireRole(domain.RoleUser))

		userRouter.Get("/api/user/profile", userHandler.GetProfile)
		userRouter.Get("/api/user/products", userHandler.ListProducts)
		userRouter.Get("/api/user/orders", userHandler.ListOrders)
		userRouter.Get("/api/user/orders/{id}", userHandler.GetOrder)
		userRouter.With(idempotencyMiddleware).Post("/api/user/orders", userHandler.CreateOrder)
	})

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("Server listening on port %s", cfg.Port)
		log.Printf("Interactive Swagger UI ready at: http://localhost:%s/swagger/index.html", cfg.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server gracefully...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited properly.")
}

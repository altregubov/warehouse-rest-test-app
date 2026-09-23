package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
	"github.com/altregubov/warehouse-rest-test-app/internal/service"
	"github.com/google/uuid"
)

type contextKey string

const (
	UserClaimsContextKey contextKey = "userClaims"
)

func AuthMiddleware(authService service.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing authorization header")
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid authorization header format. Expected 'Bearer <token>'")
				return
			}

			tokenString := parts[1]
			claims, err := authService.ValidateToken(tokenString)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Invalid or expired token")
				return
			}

			ctx := context.WithValue(r.Context(), UserClaimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireRole(expectedRole string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(UserClaimsContextKey).(*service.JWTClaims)
			if !ok || claims == nil {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
				return
			}

			if claims.Role != expectedRole {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "Access forbidden: insufficient role permissions")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func GetClaims(ctx context.Context) (*service.JWTClaims, bool) {
	claims, ok := ctx.Value(UserClaimsContextKey).(*service.JWTClaims)
	return claims, ok && claims != nil
}

func GetUserID(ctx context.Context) (uuid.UUID, bool) {
	claims, ok := GetClaims(ctx)
	if !ok {
		return uuid.Nil, false
	}
	return claims.UserID, true
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(domain.ErrorEnvelope{
		Success: false,
		Error: domain.ErrorDetails{
			Code:    code,
			Message: message,
		},
	})
}

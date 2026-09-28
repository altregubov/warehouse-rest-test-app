package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
	"github.com/altregubov/warehouse-rest-test-app/internal/repository"
)

type responseCapture struct {
	http.ResponseWriter
	statusCode int
	body       bytes.Buffer
}

func (rc *responseCapture) WriteHeader(code int) {
	rc.statusCode = code
	rc.ResponseWriter.WriteHeader(code)
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	if rc.statusCode == 0 {
		rc.statusCode = http.StatusOK
	}
	rc.body.Write(b)
	return rc.ResponseWriter.Write(b)
}

func Idempotency(repo *repository.IdempotencyRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
			if idempotencyKey == "" {
				next.ServeHTTP(w, r)
				return
			}

			var bodyBytes []byte
			if r.Body != nil {
				var err error
				bodyBytes, err = io.ReadAll(r.Body)
				if err != nil {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					_ = json.NewEncoder(w).Encode(domain.ErrorEnvelope{
						Success: false,
						Error: domain.ErrorDetails{
							Code:    "INVALID_REQUEST",
							Message: "Failed to read request body for idempotency validation",
						},
					})
					return
				}
				_ = r.Body.Close()
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}

			cached, err := repo.LockOrGet(r.Context(), idempotencyKey, r.Method, r.URL.Path, bodyBytes)
			if err != nil {
				if errors.Is(err, repository.ErrIdempotencyConflict) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusConflict)
					_ = json.NewEncoder(w).Encode(domain.ErrorEnvelope{
						Success: false,
						Error: domain.ErrorDetails{
							Code:    "IDEMPOTENCY_CONFLICT",
							Message: "Idempotency key has already been used with a different request payload",
						},
					})
					return
				}
				if errors.Is(err, repository.ErrRequestInFlight) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusConflict)
					_ = json.NewEncoder(w).Encode(domain.ErrorEnvelope{
						Success: false,
						Error: domain.ErrorDetails{
							Code:    "REQUEST_IN_FLIGHT",
							Message: "A concurrent request with the same idempotency key is already in progress",
						},
					})
					return
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(domain.ErrorEnvelope{
					Success: false,
					Error: domain.ErrorDetails{
						Code:    "INTERNAL_ERROR",
						Message: "Failed to process idempotency lock",
					},
				})
				return
			}

			if cached != nil {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Idempotent-Replayed", "true")
				w.WriteHeader(cached.StatusCode)
				_, _ = w.Write([]byte(cached.ResponseBody))
				return
			}

			rec := &responseCapture{
				ResponseWriter: w,
			}

			next.ServeHTTP(rec, r)

			if rec.statusCode == 0 {
				rec.statusCode = http.StatusOK
			}

			if rec.statusCode >= 500 {
				_ = repo.Delete(r.Context(), idempotencyKey)
			} else {
				_ = repo.Complete(r.Context(), idempotencyKey, rec.statusCode, rec.body.String())
			}
		})
	}
}

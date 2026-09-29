package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
)

// Recoverer recovers from unhandled panics, logs the stack trace to stderr,
// and serializes a standard JSON ErrorEnvelope with HTTP 500 Internal Server Error.
func Recoverer() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					if rvr == http.ErrAbortHandler {
						panic(rvr)
					}

					reqID := w.Header().Get("X-Request-ID")
					if reqID == "" {
						reqID = GetRequestID(r.Context())
					}

					fmt.Fprintf(os.Stderr, "[PANIC RECOVER] [%s] %v\n%s\n", reqID, rvr, debug.Stack())

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(domain.ErrorEnvelope{
						Success: false,
						Error: domain.ErrorDetails{
							Code:    "INTERNAL_ERROR",
							Message: "Internal server error occurred",
						},
						RequestID: reqID,
					})
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

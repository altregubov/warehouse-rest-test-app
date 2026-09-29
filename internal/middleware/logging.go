package middleware

import (
	"encoding/json"
	"net/http"
	"os"
	"time"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *loggingResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

type LogEntry struct {
	Timestamp string  `json:"timestamp"`
	Level     string  `json:"level"`
	RequestID string  `json:"requestId"`
	Method    string  `json:"method"`
	Path      string  `json:"path"`
	Status    int     `json:"status"`
	LatencyMs float64 `json:"latency_ms"`
	ClientIP  string  `json:"client_ip"`
}

// StructuredLogger returns a middleware that logs request execution in structured JSON format.
func StructuredLogger() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			writer := &loggingResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(writer, r)

			reqID := w.Header().Get("X-Request-ID")
			if reqID == "" {
				reqID = GetRequestID(r.Context())
			}

			latency := float64(time.Since(start).Microseconds()) / 1000.0

			entry := LogEntry{
				Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
				Level:     "INFO",
				RequestID: reqID,
				Method:    r.Method,
				Path:      r.URL.Path,
				Status:    writer.statusCode,
				LatencyMs: latency,
				ClientIP:  r.RemoteAddr,
			}
			if writer.statusCode >= 500 {
				entry.Level = "ERROR"
			} else if writer.statusCode >= 400 {
				entry.Level = "WARN"
			}

			_ = json.NewEncoder(os.Stdout).Encode(entry)
		})
	}
}

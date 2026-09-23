package handler

import (
	"encoding/json"
	"net/http"

	"github.com/altregubov/warehouse-rest-test-app/internal/domain"
)

func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(domain.SuccessEnvelope{
		Success: true,
		Data:    data,
	})
}

func Error(w http.ResponseWriter, status int, code, message string) {
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

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

func JSONPaginated(w http.ResponseWriter, status int, data any, page, pageSize, totalCount int) {
	totalPages := 0
	if pageSize > 0 && totalCount > 0 {
		totalPages = (totalCount + pageSize - 1) / pageSize
	}
	p := domain.PaginationMetadata{
		TotalCount: totalCount,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(domain.SuccessEnvelope{
		Success:    true,
		Data:       data,
		Pagination: &p,
		TotalCount: &totalCount,
		Page:       &page,
		PageSize:   &pageSize,
		TotalPages: &totalPages,
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

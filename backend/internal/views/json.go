package views

import (
	"encoding/json"
	"net/http"

	"github.com/iag/finance-backend/internal/models"
)

type ErrorBody struct {
	Error string `json:"error"`
}

func JSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func Error(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Cache-Control", "no-store")
	JSON(w, status, ErrorBody{Error: message})
}

func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func WriteError(w http.ResponseWriter, err error) {
	switch err {
	case models.ErrNotFound:
		Error(w, http.StatusNotFound, "not found")
	case models.ErrValidation:
		Error(w, http.StatusBadRequest, err.Error())
	case models.ErrConflict:
		Error(w, http.StatusConflict, "conflict")
	case models.ErrUnauthorized:
		Error(w, http.StatusUnauthorized, "unauthorized")
	case models.ErrForbidden:
		Error(w, http.StatusForbidden, "forbidden")
	default:
		Error(w, http.StatusInternalServerError, "internal server error")
	}
}

func Health(w http.ResponseWriter) {
	JSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "iag-finance-api"})
}

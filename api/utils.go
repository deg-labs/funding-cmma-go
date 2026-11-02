package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
)

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{
		Error: errorDetail{Code: code, Message: message},
	})
}

func writeValidationError(w http.ResponseWriter, field, message string) {
	writeError(w, http.StatusUnprocessableEntity, "INVALID_INPUT", "["+field+"]: "+message)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	// Python の json 出力に合わせ、<, >, & をエスケープしません。
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		fmt.Fprintf(os.Stderr, "failed to encode JSON response: %v\n", err)
	}
}

func parsePositiveFloat(raw string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0, errors.New("invalid positive float")
	}
	return value, nil
}

func parseNonNegativeFloat(raw string) (float64, error) {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, errors.New("invalid non-negative float")
	}
	return value, nil
}

func parseIntInRange(raw string, minimum, maximum int) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, errors.New("invalid integer")
	}
	if value < minimum {
		return 0, errors.New("too small")
	}
	if value > maximum {
		return 0, errors.New("too large")
	}
	return value, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func rateDirection(rate float64) string {
	switch {
	case rate > 0:
		return "positive"
	case rate < 0:
		return "negative"
	default:
		return "neutral"
	}
}

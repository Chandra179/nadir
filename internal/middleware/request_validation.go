package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-playground/validator/v10"
)

// ValidationError is returned when request decoding or validation fails.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

var validate = validator.New()

// DecodeAndValidate decodes JSON from r.Body into T and validates struct tags.
// Uses go-playground/validator tags (e.g. validate:"required,min=3,max=100").
// Call this inside handlers, not as middleware.
func DecodeAndValidate[T any](r *http.Request) (T, error) {
	var v T
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		return v, &ValidationError{Message: "invalid JSON: " + err.Error()}
	}
	if err := validate.Struct(v); err != nil {
		return v, &ValidationError{Message: formatValidationErrors(err)}
	}
	return v, nil
}

func formatValidationErrors(err error) string {
	var errs validator.ValidationErrors
	if !isValidationErrors(err, &errs) {
		return err.Error()
	}
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, e.Field()+": "+e.Tag())
	}
	return strings.Join(msgs, "; ")
}

func isValidationErrors(err error, out *validator.ValidationErrors) bool {
	if ve, ok := err.(validator.ValidationErrors); ok {
		*out = ve
		return true
	}
	return false
}

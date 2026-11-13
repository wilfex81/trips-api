package httpapi

import (
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// apiError is the one error shape every endpoint returns.
type apiError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func writeError(c *gin.Context, status int, code, message string, fields map[string]string) {
	c.AbortWithStatusJSON(status, gin.H{"error": apiError{Code: code, Message: message, Fields: fields}})
}

func init() {
	// reject JSON with fields we do not know, so a typo like "phon" does not pass silently
	binding.EnableDecoderDisallowUnknownFields = true

	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		// report errors under the JSON name ("phone"), not the Go name ("Phone")
		v.RegisterTagNameFunc(func(f reflect.StructField) string {
			name := strings.SplitN(f.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		})
		// a string made only of spaces is not a name
		_ = v.RegisterValidation("maxbytes", func(fl validator.FieldLevel) bool {
			n, err := strconv.Atoi(fl.Param())
			return err == nil && len(fl.Field().String()) <= n
		})
	}
}

// bindJSON reads the request body into dst. On failure it writes the error
// response itself and returns false.
func bindJSON(c *gin.Context, dst any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
	err := c.ShouldBindJSON(dst)
	if err == nil {
		return true
	}

	var tooBig *http.MaxBytesError
	var invalid validator.ValidationErrors
	switch {
	case errors.As(err, &tooBig):
		writeError(c, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large", nil)
	case errors.As(err, &invalid):
		fields := make(map[string]string, len(invalid))
		for _, fe := range invalid {
			fields[fe.Field()] = fieldMessage(fe)
		}
		writeError(c, http.StatusUnprocessableEntity, "validation_failed", "request body failed validation", fields)
	default:
		writeError(c, http.StatusBadRequest, "invalid_json", "request body is not valid JSON or has unknown fields", nil)
	}
	return false
}

func fieldMessage(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required", "notblank":
		return "is required"
	case "max":
		return "must be at most " + fe.Param() + " characters"
	case "min":
		return "must be at least " + fe.Param() + " characters"
	case "maxbytes":
		return "must be at most " + fe.Param() + " bytes"
	case "email":
		return "must be a valid email address"
	case "e164":
		return "must be in international format, like +254700000001"
	case "oneof":
		return "must be one of: " + strings.ReplaceAll(fe.Param(), " ", ", ")
	default:
		return "is invalid"
	}
}

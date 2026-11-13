package httpapi

import (
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"

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

var registerOnce sync.Once

// registerValidators adds our custom rules to Gin's validator. NewRouter calls
// it once, and it panics if anything is wrong, so a broken setup shows up when
// the server boots and not on the first request.
func registerValidators() {
	registerOnce.Do(func() {
		binding.EnableDecoderDisallowUnknownFields = true

		v, ok := binding.Validator.Engine().(*validator.Validate)
		if !ok {
			panic("httpapi: gin's validator is not a *validator.Validate")
		}

		// report errors under the JSON name ("phone"), not the Go name ("Phone")
		v.RegisterTagNameFunc(func(f reflect.StructField) string {
			name := strings.SplitN(f.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return ""
			}
			return name
		})

		// a string made only of spaces is not a name
		mustRegister(v, "notblank", func(fl validator.FieldLevel) bool {
			return strings.TrimSpace(fl.Field().String()) != ""
		})

		// bcrypt only uses the first 72 bytes, so longer passwords are refused
		mustRegister(v, "maxbytes", func(fl validator.FieldLevel) bool {
			n, err := strconv.Atoi(fl.Param())
			return err == nil && len(fl.Field().String()) <= n
		})
	})
}

func mustRegister(v *validator.Validate, tag string, fn validator.Func) {
	if err := v.RegisterValidation(tag, fn); err != nil {
		panic("httpapi: cannot register validator " + tag + ": " + err.Error())
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

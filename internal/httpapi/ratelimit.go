package httpapi

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// Allower is whatever can answer "is this key allowed right now".
// *limiter.Store from the rate-limiter module satisfies it.
type Allower interface {
	Allow(key string) (bool, time.Duration)
}

var noLimit gin.HandlerFunc = func(c *gin.Context) { c.Next() }

// rateLimit rejects requests over the limit with 429 and a Retry-After header.
// keyFn decides who counts as one client.
func rateLimit(a Allower, keyFn func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ok, retry := a.Allow(keyFn(c))
		if ok {
			c.Next()
			return
		}
		secs := int(math.Ceil(retry.Seconds()))
		if secs < 1 {
			secs = 1
		}
		c.Header("Retry-After", strconv.Itoa(secs))
		writeError(c, http.StatusTooManyRequests, "rate_limited", "too many requests, try again later", nil)
	}
}

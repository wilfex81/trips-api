package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wilfex81/trips-api/internal/auth"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type Deps struct {
	DB             Pinger
	Drivers        DriverRepo
	Trips          TripRepo
	Users          UserRepo
	Auth           *auth.Service
	IPLimiter      Allower // limits /auth by client address; nil turns it off
	UserLimiter    Allower // limits the API by logged in user; nil turns it off
	TrustedProxies []string
}

func NewRouter(d Deps) *gin.Engine {
	registerValidators()
	r := gin.New()
	// Gin trusts X-Forwarded-For from anyone by default, which lets a client
	// fake its address and dodge a per-address limit. Trust only proxies we list.
	if err := r.SetTrustedProxies(d.TrustedProxies); err != nil {
		panic("httpapi: bad trusted proxies: " + err.Error())
	}

	ipLimit, userLimit := noLimit, noLimit
	if d.IPLimiter != nil {
		ipLimit = rateLimit(d.IPLimiter, func(c *gin.Context) string { return "ip:" + c.ClientIP() })
	}
	if d.UserLimiter != nil {
		userLimit = rateLimit(d.UserLimiter, func(c *gin.Context) string {
			return "user:" + strconv.FormatInt(currentUserID(c), 10)
		})
	}
	r.Use(gin.CustomRecovery(func(c *gin.Context, _ any) {
		writeError(c, http.StatusInternalServerError, "internal_error", "internal error", nil)
	}), gin.Logger())

	r.HandleMethodNotAllowed = true
	r.NoRoute(func(c *gin.Context) {
		writeError(c, http.StatusNotFound, "route_not_found", "no such route", nil)
	})
	r.NoMethod(func(c *gin.Context) {
		writeError(c, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", nil)
	})

	r.GET("/healthz", func(ctx *gin.Context) {
		ctx.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// We can reach the DB
	r.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := d.DB.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "database unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})

	})
	(&authHandler{users: d.Users, tokens: d.Auth, ipLimit: ipLimit, userLimit: userLimit}).mount(r)
	(&driverHandler{repo: d.Drivers, tokens: d.Auth, limit: userLimit}).register(r)
	(&tripHandler{repo: d.Trips, tokens: d.Auth, limit: userLimit}).register(r)
	return r

}

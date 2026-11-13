package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/wilfex81/trips-api/internal/auth"
	"github.com/wilfex81/trips-api/internal/store"
)

type UserRepo interface {
	Create(ctx context.Context, email, passwordHash string) (store.User, error)
	GetByEmail(ctx context.Context, email string) (store.User, error)
	Get(ctx context.Context, id int64) (store.User, error)
}

type registerRequest struct {
	Email    string `json:"email" binding:"required,email,max=254"`
	Password string `json:"password" binding:"required,min=8,maxbytes=72"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required,maxbytes=72"`
}

type authHandler struct {
	users  UserRepo
	tokens *auth.Service
}

func (h *authHandler) mount(r gin.IRouter) {
	g := r.Group("/auth")
	g.POST("register", h.registerUser)
	g.POST("/login", h.login)

	r.GET("/me", requireAuth(h.tokens), h.me)
}

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (h *authHandler) registerUser(c *gin.Context) {
	var in registerRequest
	if !bindJSON(c, &in) {
		return
	}
	hash, err := h.tokens.HashPassword(in.Password)
	if err != nil {
		fail(c, err)
		return
	}

	u, err := h.users.Create(c.Request.Context(), normalizeEmail(in.Email), hash)
	if errors.Is(err, store.ErrConflict) {
		writeError(c, http.StatusConflict, "email_taken", "an account with this email already exists", nil)
		return
	}
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, u)
}

func (h *authHandler) login(c *gin.Context) {
	var in loginRequest
	if !bindJSON(c, &in) {
		return
	}
	u, err := h.users.GetByEmail(c.Request.Context(), normalizeEmail(in.Email))
	switch {
	case errors.Is(err, store.ErrNotFound):
		h.tokens.BurnTime(in.Password) // same cost as a real check
		invalidCredentials(c)
		return
	case err != nil:
		fail(c, err)
		return
	}
	if !h.tokens.CheckPassword(u.PasswordHash, in.Password) {
		invalidCredentials(c)
		return
	}

	token, _, err := h.tokens.Issue(u.ID)
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   int(h.tokens.TTL().Seconds()),
	})
}

func (h *authHandler) me(c *gin.Context) {
	u, err := h.users.Get(c.Request.Context(), currentUserID(c))
	if errors.Is(err, store.ErrNotFound) {
		unauthorized(c)
		return
	}
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, u)
}

func invalidCredentials(c *gin.Context) {
	writeError(c, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect", nil)

}

const userIDKey = "userID"

// requireAuth rejects requests without a valid Bearer token and stores the user id.
func requireAuth(tokens *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		scheme, raw, ok := strings.Cut(c.GetHeader("Authorization"), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(raw) == "" {
			unauthorized(c)
			return
		}
		id, err := tokens.Parse(strings.TrimSpace(raw))
		if err != nil {
			unauthorized(c)
			return
		}
		c.Set(userIDKey, id)
		c.Next()
	}
}

func currentUserID(c *gin.Context) int64 { return c.GetInt64(userIDKey) }

func unauthorized(c *gin.Context) {
	c.Header("WWW-Authenticate", `Bearer realm="trips-api"`)
	writeError(c, http.StatusUnauthorized, "unauthorized", "missing or invalid access token", nil)
}

package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/wilfex81/trips-api/internal/store"
)

// DriverRepo is what the handlers need from the database layer.
type DriverRepo interface {
	Create(ctx context.Context, name, phone string) (store.Driver, error)
	Get(ctx context.Context, id int64) (store.Driver, error)
	List(ctx context.Context) ([]store.Driver, error)
	Update(ctx context.Context, id int64, name, phone, status string) (store.Driver, error)
	Delete(ctx context.Context, id int64) error
}

type createDriverRequest struct {
	Name  string `json:"name" binding:"required"`
	Phone string `json:"phone" binding:"required"`
}

type updateDriverRequest struct {
	Name   string `json:"name" binding:"required"`
	Phone  string `json:"phone" binding:"required"`
	Status string `json:"status" binding:"required,oneof=offline available on_trip"`
}

type driverHandler struct {
	repo DriverRepo
}

func (h *driverHandler) register(r gin.IRouter) {
	g := r.Group("/drivers")
	g.POST("", h.create)
	g.GET("", h.list)
	g.GET("/:id", h.get)
	g.PUT("/:id", h.update)
	g.DELETE("/:id", h.delete)
}

func (h *driverHandler) create(c *gin.Context) {
	var in createDriverRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	d, err := h.repo.Create(c.Request.Context(), in.Name, in.Phone)
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Location", "/drivers/"+strconv.FormatInt(d.ID, 10))
	c.JSON(http.StatusCreated, d)
}

func (h *driverHandler) list(c *gin.Context) {
	ds, err := h.repo.List(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, ds)
}

func (h *driverHandler) get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	d, err := h.repo.Get(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

func (h *driverHandler) update(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in updateDriverRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	d, err := h.repo.Update(c.Request.Context(), id, in.Name, in.Phone, in.Status)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

func (h *driverHandler) delete(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.repo.Delete(c.Request.Context(), id); err != nil {
		fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// pathID reads :id from the URL. It writes the 400 itself when the id is bad.
func pathID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id must be a positive integer"})
		return 0, false
	}
	return id, true
}

// fail maps store errors to status codes. Anything unexpected is a 500
// and its details stay in the log, never in the response.
func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "driver not found"})
	case errors.Is(err, store.ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "phone number already in use"})
	default:
		log.Printf("internal error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
	}
}

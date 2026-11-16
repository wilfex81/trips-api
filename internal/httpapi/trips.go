package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/wilfex81/trips-api/internal/auth"
	"github.com/wilfex81/trips-api/internal/store"
	"github.com/wilfex81/trips-api/internal/trips"
)

type TripRepo interface {
	Create(ctx context.Context, ownerID int64, pickup, dropoff string) (store.Trip, error)
	Get(ctx context.Context, ownerID, id int64) (store.Trip, error)
	List(ctx context.Context, p store.TripListParams) ([]store.Trip, error)
	Apply(ctx context.Context, ownerID, id int64, a trips.Action, driverID int64) (store.Trip, error)
}

type createTripRequest struct {
	Pickup  string `json:"pickup" binding:"required,notblank,max=200"`
	Dropoff string `json:"dropoff" binding:"required,notblank,max=200"`
}

type acceptTripRequest struct {
	DriverID int64 `json:"driver_id" binding:"required,gte=1"`
}

type tripHandler struct {
	repo   TripRepo
	tokens *auth.Service
}

func (h *tripHandler) register(r gin.IRouter) {
	g := r.Group("/trips", requireAuth(h.tokens))
	g.POST("", h.create)
	g.GET("", h.list)
	g.GET("/:id", h.get)
	g.POST("/:id/accept", h.accept)
	g.POST("/:id/start", h.act(trips.Start))
	g.POST("/:id/complete", h.act(trips.Complete))
	g.POST("/:id/cancel", h.act(trips.Cancel))
}

func (h *tripHandler) create(c *gin.Context) {
	var in createTripRequest
	if !bindJSON(c, &in) {
		return
	}
	t, err := h.repo.Create(c.Request.Context(), currentUserID(c), in.Pickup, in.Dropoff)
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Location", "/trips/"+strconv.FormatInt(t.ID, 10))
	c.JSON(http.StatusCreated, t)
}

type tripPage struct {
	Data       []store.Trip `json:"data"`
	NextCursor *string      `json:"next_cursor"`
}

func (h *tripHandler) list(c *gin.Context) {
	p, ok := parseListParams(c, trips.Statuses)
	if !ok {
		return
	}
	ts, err := h.repo.List(c.Request.Context(), store.TripListParams{
		OwnerID: currentUserID(c),
		AfterID: p.afterID,
		Status:  p.status,
		Limit:   p.limit + 1,
	})
	if err != nil {
		fail(c, err)
		return
	}

	var next *string
	if len(ts) > p.limit {
		ts = ts[:p.limit]
		cur := encodeCursor(ts[len(ts)-1].ID)
		next = &cur

	}
	c.JSON(http.StatusOK, tripPage{Data: ts, NextCursor: next})
}

func (h *tripHandler) get(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	t, err := h.repo.Get(c.Request.Context(), currentUserID(c), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, t)

}

func (h *tripHandler) accept(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var in acceptTripRequest
	if !bindJSON(c, &in) {
		return
	}
	t, err := h.repo.Apply(c.Request.Context(), currentUserID(c), id, trips.Accept, in.DriverID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, t)
}

func (h *tripHandler) act(a trips.Action) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathID(c)
		if !ok {
			return
		}
		t, err := h.repo.Apply(c.Request.Context(), currentUserID(c), id, a, 0)
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(http.StatusOK, t)
	}
}

package httpapi

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

type listParams struct {
	limit   int
	afterID int64
	status  string
}

func parseListParams(c *gin.Context) (listParams, bool) {
	p := listParams{limit: defaultLimit}
	fields := map[string]string{}

	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxLimit {
			fields["limit"] = "must be an interger between 1 and 100"
		} else {
			p.limit = n
		}
	}

	if s := c.Query("status"); s != "" {
		switch s {
		case "offline", "available", "on_trip":
			p.status = s
		default:
			fields["status"] = "must be one of: offline, available, on_trip"
		}
	}

	if len(fields) > 0 {
		writeError(c, http.StatusUnprocessableEntity, "validation_failed", "invalid query parameters", fields)
		return p, false
	}

	if s := c.Query("cursor"); s != "" {
		id, err := decodeCursor(s)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid cursor", "cursor is not valid", nil)
			return p, false
		}
		p.afterID = id
	}
	return p, true
}

func encodeCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}

func decodeCursor(s string) (int64, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil || id < 1 {
		return 0, errors.New("bad cursor")
	}
	return id, nil
}

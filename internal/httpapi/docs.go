package httpapi

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

// openAPISpec is the API description, compiled into the binary.
// The tests in openapi_test.go fail when it drifts from the real routes.
//
//go:embed openapi.yaml
var openAPISpec []byte

const docsPage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>trips-api docs</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui.css">
</head>
<body>
  <div id="ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
  <script>window.ui = SwaggerUIBundle({ url: "/openapi.yaml", dom_id: "#ui" });</script>
</body>
</html>`

func mountDocs(r gin.IRouter) {
	r.GET("/openapi.yaml", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/yaml; charset=utf-8", openAPISpec)
	})
	r.GET("/docs", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(docsPage))
	})
}

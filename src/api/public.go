/**
 * public.go - / rest api implementation
 *
 * @author Mike Schroeder <m.schroeder223@gmail.com>
 */
package api

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

//实现HTTP-ping接口，/ping返回200 OK

/**
 * Attaches / handlers
 */
func attachPublic(app *gin.RouterGroup) {

	/**
	 * Simple 200 and OK response
	 */
	app.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})
}

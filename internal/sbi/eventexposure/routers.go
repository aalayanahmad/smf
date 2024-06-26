/*
 * Nsmf_EventExposure
 *
 * Session Management Event Exposure Service API
 *
 * API version: 1.0.0
 * Generated by: OpenAPI Generator (https://openapi-generator.tech)
 */

package eventexposure

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	smf_context "github.com/aalayanahmad/smf/internal/context"
	"github.com/aalayanahmad/smf/internal/logger"
	util_oauth "github.com/aalayanahmad/smf/internal/util/oauth"
	"github.com/aalayanahmad/smf/pkg/factory"
	"github.com/free5gc/openapi/models"
	logger_util "github.com/free5gc/util/logger"
)

// Route is the information for every URI.
type Route struct {
	// Name is the name of this Route.
	Name string
	// Method is the string for the HTTP method. ex) GET, POST etc..
	Method string
	// Pattern is the pattern of the URI.
	Pattern string
	// HandlerFunc is the handler function of this route.
	HandlerFunc gin.HandlerFunc
}

// Routes is the list of the generated Route.
type Routes []Route

// NewRouter returns a new router.
func NewRouter() *gin.Engine {
	router := logger_util.NewGinWithLogrus(logger.GinLog)
	AddService(router)
	return router
}

func AddService(engine *gin.Engine) *gin.RouterGroup {
	group := engine.Group(factory.SmfEventExposureResUriPrefix)

	routerAuthorizationCheck := util_oauth.NewRouterAuthorizationCheck(models.ServiceName_NSMF_EVENT_EXPOSURE)
	group.Use(func(c *gin.Context) {
		routerAuthorizationCheck.Check(c, smf_context.GetSelf())
	})

	for _, route := range routes {
		switch route.Method {
		case "GET":
			group.GET(route.Pattern, route.HandlerFunc)
		case "POST":
			group.POST(route.Pattern, route.HandlerFunc)
		case "PUT":
			group.PUT(route.Pattern, route.HandlerFunc)
		case "DELETE":
			group.DELETE(route.Pattern, route.HandlerFunc)
		}
	}

	return group
}

// Index is the index handler.
func Index(c *gin.Context) {
	c.String(http.StatusOK, "Hello World!")
}

var routes = Routes{
	{
		"Index",
		"GET",
		"",
		Index,
	},

	{
		"SubscriptionsPost",
		strings.ToUpper("Post"),
		"subscriptions",
		SubscriptionsPost,
	},

	{
		"SubscriptionsSubIdDelete",
		strings.ToUpper("Delete"),
		"/subscriptions/:subId",
		SubscriptionsSubIdDelete,
	},

	{
		"SubscriptionsSubIdGet",
		strings.ToUpper("Get"),
		"/subscriptions/:subId",
		SubscriptionsSubIdGet,
	},

	{
		"SubscriptionsSubIdPut",
		strings.ToUpper("Put"),
		"/subscriptions/:subId",
		SubscriptionsSubIdPut,
	},
}

package router

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelStatusRoutesUseOperatePermission(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodPost, "/:id/status", authz.ChannelOperate, controller.UpdateChannelStatus)
	assertChannelRoutePermission(t, http.MethodPost, "/status/batch", authz.ChannelOperate, controller.BatchUpdateChannelStatus)
	assertChannelRoutePermission(t, http.MethodPut, "/", authz.ChannelWrite, controller.UpdateChannel)
}

func TestChannelDeleteRoutesUseSensitiveWritePermission(t *testing.T) {
	assertChannelRoutePermission(t, http.MethodDelete, "/:id", authz.ChannelSensitiveWrite, controller.DeleteChannel)
	assertChannelRoutePermission(t, http.MethodPost, "/batch", authz.ChannelSensitiveWrite, controller.DeleteChannelBatch)
	assertChannelRoutePermission(t, http.MethodDelete, "/disabled", authz.ChannelSensitiveWrite, controller.DeleteDisabledChannel)
	assertChannelRoutePermission(t, http.MethodPut, "/", authz.ChannelWrite, controller.UpdateChannel)
	assertChannelRoutePermission(t, http.MethodPut, "/tag", authz.ChannelWrite, controller.EditTagChannels)
	assertChannelRoutePermission(t, http.MethodPost, "/batch/tag", authz.ChannelWrite, controller.BatchSetChannelTag)
}

func TestChannelMonitorRoutesUseExpectedPermissions(t *testing.T) {
	assertChannelMonitorRoutePermission(t, http.MethodGet, "/config/:id", authz.ChannelRead, controller.GetChannelMonitorConfig)
	assertChannelMonitorRoutePermission(t, http.MethodPut, "/config", authz.ChannelWrite, controller.SaveChannelDetectionConfig)
	assertChannelMonitorRoutePermission(t, http.MethodPut, "/templates/:id", authz.ChannelWrite, controller.UpdateMonitorTemplate)
}

func TestChannelStatusRoutesRegisterWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	api := engine.Group("/api")

	require.NotPanics(t, func() {
		registerChannelRoutes(api)
	})
}

// TestChannelCollectionRootResolvesWithoutTrailingSlash guards against a
// regression where GET /api/channel (no trailing slash) returned 404. Gin
// normally 301-redirects the no-slash form to the "/" route, but that
// trailing-slash redirect is silently dropped once the full route tree
// contains a root-level param wildcard (/:mode/mj, from the relay router)
// alongside the /api/channel vs /api/channel_monitor prefix split. The
// channel router works around it by also registering the exact no-slash
// alias, so the collection root must resolve to a handler (401 without a
// session), never 404.
func TestChannelCollectionRootResolvesWithoutTrailingSlash(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)
	SetDashboardRouter(engine)
	SetRelayRouter(engine)
	SetVideoRouter(engine)

	for _, target := range []string{
		"/api/channel",
		"/api/channel/",
		"/api/channel?tag_mode=false&id_sort=false&p=1&page_size=20",
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		engine.ServeHTTP(w, req)
		assert.Equalf(t, http.StatusUnauthorized, w.Code,
			"GET %s should resolve to the channel list handler (auth-gated), not 404", target)
	}
}

func TestChannelMonitorManualProbeRouteResolves(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/channel_monitor/probe", nil)
	engine.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func assertChannelRoutePermission(t *testing.T, method string, path string, permission authz.Permission, handler any) {
	t.Helper()
	for _, route := range channelPermissionRoutes {
		if route.method == method && route.path == path {
			assert.Equal(t, permission, route.permission)
			assert.Equal(t, reflect.ValueOf(handler).Pointer(), reflect.ValueOf(route.handler).Pointer())
			return
		}
	}
	t.Fatalf("route %s %s not found", method, path)
}

func assertChannelMonitorRoutePermission(t *testing.T, method string, path string, permission authz.Permission, handler any) {
	t.Helper()
	for _, route := range channelMonitorPermissionRoutes {
		if route.method == method && route.path == path {
			assert.Equal(t, permission, route.permission)
			assert.Equal(t, reflect.ValueOf(handler).Pointer(), reflect.ValueOf(route.handler).Pointer())
			return
		}
	}
	t.Fatalf("channel monitor route %s %s not found", method, path)
}

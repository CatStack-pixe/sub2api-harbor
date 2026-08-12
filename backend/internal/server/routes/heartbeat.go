package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"

	"github.com/gin-gonic/gin"
)

// RegisterHeartbeatRoutes registers the constrained external provisioning
// callback outside the public/admin API namespaces.
func RegisterHeartbeatRoutes(r *gin.Engine, heartbeat *handler.HeartbeatHandler) {
	if heartbeat == nil {
		return
	}
	r.POST("/api/heartbeat", heartbeat.Handle)
}

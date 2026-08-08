package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

const remoteIngestMaxBodyBytes = 16 * 1024

func RegisterRemoteIngestRoutes(v1 *gin.RouterGroup, h *handler.RemoteIngestHandler) {
	if h == nil {
		return
	}
	remote := v1.Group("/remote-ingest")
	remote.Use(middleware.RequestBodyLimit(remoteIngestMaxBodyBytes))
	{
		remote.POST("/enroll", h.Enroll)
		remote.POST("/handshakes", h.Handshake)
		remote.POST("/accounts", h.SubmitAccount)
		remote.GET("/deliveries/:id", h.GetDelivery)
	}
}

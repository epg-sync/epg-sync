package router

import (
	"github.com/gin-gonic/gin"

	"github.com/epg-sync/epgsync/internal/api/http/handler"
	"github.com/epg-sync/epgsync/internal/api/http/middleware"
	"github.com/epg-sync/epgsync/internal/config"
)

func SetupRouter(
	cfg *config.AppConfig,
	channelHandler *handler.ChannelHandler,
	epgHandler *handler.EPGHandler,
	schedulerHandler *handler.SchedulerHandler,
	authHandler *handler.AuthHandler,
	logHandler *handler.LogHandler,
) *gin.Engine {

	router := gin.New()
	router.Use(middleware.RequestLogger())
	router.Use(middleware.Recovery())
	router.Use(corsMiddleware())

	auth := router.Group("/auth")
	{
		auth.POST("/login", authHandler.Login)
		auth.GET("/me", middleware.JWTAuthMiddleware(cfg.Server.JWTSecret), authHandler.GetCurrentUser)
		auth.PUT("/password", middleware.JWTAuthMiddleware(cfg.Server.JWTSecret), authHandler.ChangePassword)
	}

	admin := router.Group("/admin")
	admin.Use(middleware.JWTAuthMiddleware(cfg.Server.JWTSecret))
	{
		admin.GET("/channels", channelHandler.ListChannels)
		admin.POST("/channels", channelHandler.CreateChannel)
		admin.POST("/channels/batch", channelHandler.BatchCreateChannel)
		admin.GET("/channels/:id", channelHandler.GetChannel)
		admin.PUT("/channels/:id", channelHandler.UpdateChannel)
		admin.DELETE("/channels/:id", channelHandler.DeleteChannel)

		admin.GET("/channel-mappings", channelHandler.ListChannelMappings)
		admin.GET("/channels/:id/mappings", channelHandler.GetChannelMappings)

		admin.GET("/programs/search", epgHandler.GetEPGByChannelAndDate)

		admin.POST("/epg/sync", epgHandler.SyncEPGByChannelAndDateRange)

		admin.POST("/job/sync", schedulerHandler.SyncAllEPG)

		admin.GET("/logs", logHandler.ListLogs)
	}

	api := router.Group("/api")
	{
		api.GET("/diyp", epgHandler.GenerateDIYPProgram)
		api.GET("/xmltv", epgHandler.GenerateXMLTVProgram)
	}

	return router
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

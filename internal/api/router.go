package api

import (
	"github.com/AungKyawPhyo1142/sentria-go/internal/api/handlers"
	"github.com/AungKyawPhyo1142/sentria-go/internal/config"
	"github.com/gin-gonic/gin"
)

func SetupRouter(cfg *config.Config) *gin.Engine {
	gin.SetMode(cfg.GinMode)
	router := gin.Default()

	router.GET("/health", handlers.HealthCheck)

	//API versioning
	// v1Router := router.Group("/v1")
	// {
	//add v1 routes here
	//v1Router.GET("/fact-check", handlers.FactCheck)
	// }

	return router
}

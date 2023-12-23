package api

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func Register(router *gin.Engine) {
	// Configure CORS to allow all methods and all origins
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	config.AllowHeaders = []string{
		"Accept",
		"Authorization",
		"Content-Type",
		"Accept-Language",
		"Content-Language",
		"DPR",
		"Downlink",
		"Save-Data",
		"Viewport-Width",
		"Width",
		"X-Requested-With",
	}
	config.OptionsResponseStatusCode = http.StatusOK
	router.Use(cors.New(config))

	// Define a route and its handler
	router.GET("/", IndexHandler)
	// openai model
	router.GET("/v1/models", ModelListHandler)
	router.GET("/v1/models/gpt-3.5-turbo", ModelRetrieveHandler)

	// openai chat
	router.POST("/v1/chat/completions", ChatProxyHandler)
	router.POST("/v1/chat/completions/vision", VisionProxyHandler)
}

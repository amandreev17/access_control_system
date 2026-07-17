package main

import (
	"log"
	"os"

	"qr-code-backend/database"
	"qr-code-backend/handlers"
	"qr-code-backend/keys"
	"qr-code-backend/middleware"

	"github.com/gin-gonic/gin"
)

func main() {
	// Initialize RSA keys
	if err := keys.Init(); err != nil {
		log.Fatalf("Failed to initialize RSA keys: %v", err)
	}

	// Initialize database
	database.Init()

	// Initialize handlers
	authHandler := handlers.NewAuthHandler()
	qrHandler := handlers.NewQRHandler()
	userHandler := handlers.NewUserHandler()

	// Setup Gin router
	r := gin.Default()
	r.Use(middleware.CORSMiddleware())

	// Public routes
	r.POST("/api/login", authHandler.Login)
	r.POST("/api/validate-qr", qrHandler.ValidateQRCode)
	r.GET("/api/public-key", qrHandler.GetPublicKey)

	// Protected routes
	api := r.Group("/api")
	api.Use(middleware.AuthMiddleware())
	{
		api.GET("/profile", authHandler.GetProfile)
		api.POST("/generate-qr", qrHandler.GenerateQRCode)
		api.GET("/access-logs", qrHandler.GetAccessLogs)

		// Admin-only routes for user management
		admin := api.Group("/users")
		admin.Use(handlers.AdminOnly())
		{
			admin.GET("", userHandler.ListUsers)
			admin.GET("/:id", userHandler.GetUser)
			admin.POST("", userHandler.CreateUser)
			admin.PUT("/:id", userHandler.UpdateUser)
			admin.DELETE("/:id", userHandler.DeleteUser)
		}
	}

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s...", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

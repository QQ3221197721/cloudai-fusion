// Initializer creates default admin user if not exists
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/auth"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
)

func main() {
	// Create database store using SQLite for easy local development
	dbStore, err := store.New(store.Config{
		DSN:             "./data/cloudai-fusion.db",
		MaxOpenConns:    25,
		MaxIdleConns:    10,
		ConnMaxLifetime: 5 * time.Minute,
		LogLevel:        "error",
		Driver:          "sqlite",
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbStore.Close()

	// Check if admin user already exists
	_, err = dbStore.GetUserByUsername("admin")
	if err == nil {
		fmt.Println("✅ Admin user already exists")
		return
	}

	fmt.Println("🔧 Creating default admin user...")

	// Create auth service
	authSvc, err := auth.NewService(auth.Config{
		JWTSecret: []byte("cloudai-fusion-jwt-secret-key-please-change-in-prod"),
		JWTExpiry: 24 * time.Hour,
	})
	if err != nil {
		log.Fatalf("Failed to create auth service: %v", err)
	}

	// Create admin user
	req := &auth.RegisterRequest{
		Username:    "admin",
		Email:       "admin@cloudai.io",
		Password:    "Admin123!",
		DisplayName: "System Administrator",
	}

	user, err := authSvc.RegisterUser(req)
	if err != nil {
		log.Fatalf("Failed to create admin user: %v", err)
	}

	fmt.Printf("✅ Default admin user created:\n")
	fmt.Printf("   Username: %s\n", user.Username)
	fmt.Printf("   Email: %s\n", user.Email)
	fmt.Printf("   Password: Admin123!\n")
	fmt.Printf("   Role: %s\n", user.Role)
	fmt.Printf("\n🎉 You can now login with these credentials!\n")
}

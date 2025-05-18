package config

import (
	"log"
	"os"
	"strconv"
)

type Config struct {
	ServerPort int
	GinMode    string

	// add other configs here
	// JobQueuesAddress string
	// NodeAPIBaseURL   string
	// NodeAPIKey       string
}

func LoadConfig() (*Config, error) {
	portStr := os.Getenv("PORT")
	if portStr == "" {
		portStr = "8081"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Printf("Invalid port number: %s", portStr)
		port = 8081
	}
	ginMode := os.Getenv("GIN_MODE")
	if ginMode == "" {
		ginMode = "debug"
	}
	return &Config{
		ServerPort: port,
		GinMode:    ginMode,
	}, nil

}

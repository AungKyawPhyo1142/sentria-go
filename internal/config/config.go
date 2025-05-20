package config

import (
	"log"
	"os"
	"strconv"
)

type Config struct {
	ServerPort           int
	GinMode              string
	RabbitMQ_URL         string
	Factcheck_Queue_Name string

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

	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		rabbitURL = "amqp://guest:guest@localhost:5672/"
		log.Printf("Warning: RABBITMQ_URL not set, using default: %s", rabbitURL)
	}

	queueName := os.Getenv("FACTCHECK_QUEUE_NAME")
	if queueName == "" {
		queueName = "sentria_factcheck_jobs"
		log.Printf("Warning: FACTCHECK_QUEUE_NAME not set, using default: %s", queueName)
	}

	return &Config{
		ServerPort:           port,
		GinMode:              ginMode,
		RabbitMQ_URL:         rabbitURL,
		Factcheck_Queue_Name: queueName,
	}, nil

}

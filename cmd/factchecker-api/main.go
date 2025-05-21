package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AungKyawPhyo1142/sentria-go/internal/api"
	"github.com/AungKyawPhyo1142/sentria-go/internal/config"
	"github.com/AungKyawPhyo1142/sentria-go/internal/core/service"
	"github.com/AungKyawPhyo1142/sentria-go/internal/models"
	"github.com/AungKyawPhyo1142/sentria-go/internal/platform/external_apis"
	"github.com/AungKyawPhyo1142/sentria-go/internal/worker"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("FATAL: Failed to load configuration: %v", err)
	}
	log.Printf("Starting Sentria Fact-Checking Service on port %d in %s mode...", cfg.ServerPort, cfg.GinMode)

	// --- Initialize Dependencies for FactCheckService ---
	gdacsAPIClient := external_apis.NewGDACSClient()            // Create GDACS client instance
	factCheckSvc := service.NewFactCheckService(gdacsAPIClient) // Create FactCheckService instance
	// ----------------------------------------------------

	var jobConsumer *worker.JobConsumer

	if cfg.RabbitMQ_URL != "" && cfg.Factcheck_Queue_Name != "" {
		numberOfWorkers := 10
		jobConsumer, err = worker.NewJobConsumer(cfg, factCheckSvc, numberOfWorkers) // <-- PASS factCheckSvc here
		if err != nil {
			log.Printf("WARNING: Failed to initialize RabbitMQ Job Consumer: %v. Will not consume jobs.", err)
			// Decide if this is fatal. If MQ is essential, log.Fatalf here.
			// For now, allow server to start but log a clear warning.
			jobConsumer = nil // Ensure it's nil so we don't try to use it
		} else {
			consumerCtx, cancelConsumer := context.WithCancel(context.Background())

			go jobConsumer.StartConsuming(consumerCtx)
			log.Println("[Main] RabbitMQ Job Consumer listening for jobs.")

			// consumerWg.Add(1)
			// go func() {
			// 	defer consumerWg.Done()
			// 	defer log.Println("[Main] Consumer goroutine has exited.")
			// 	jobConsumer.StartConsuming(consumerCtx) // This will block until ctx is cancelled or fatal error
			// }()

			// Graceful shutdown setup for when consumer IS active
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

			go func() {
				sig := <-quit
				log.Printf("Received signal: %s. Initiating graceful shutdown...", sig)

				log.Println("[Main] Signaling job consumer to stop via Stop()...")
				if jobConsumer != nil {
					jobConsumer.Stop()
				}

				log.Println("[Main] Cancelling consumer context...")
				cancelConsumer() // Cancel the context for the consumer loop

				if jobConsumer != nil {
					log.Println("[Main] calling jobConsumer Close()...")
					jobConsumer.Close()
				}

				log.Println("Application shut down attempt complete. Exiting.")
				os.Exit(0)
			}()
		}
	} else {
		log.Println("[Main] RabbitMQ URI or Queue Name not configured. Job consumer will NOT start.")
		// Simpler shutdown if no consumer
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		sig := <-quit
		log.Printf("Received signal: %s. Shutting down (no consumer active)...", sig)
		os.Exit(0)
	}

	// Setup and run Gin server (this will block the main goroutine here if not handled above)
	// If the consumer logic above results in os.Exit(0), this part might not be reached
	// in the graceful shutdown path when a consumer is active.
	// Consider running Gin server in its own goroutine and managing its shutdown too.
	router := api.SetupRouter(cfg)
	serverAddr := fmt.Sprintf(":%d", cfg.ServerPort)
	log.Printf("Attempting to start Gin HTTP server on address: %s", serverAddr)
	// For graceful HTTP server shutdown, you'd use http.Server and its Shutdown method.
	// Example:
	// srv := &http.Server{ Addr: serverAddr, Handler: router }
	// go func() { if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed { log.Fatalf(...) } }()
	// Then in shutdown: ctxShutdown, _ := context.WithTimeout(...); srv.Shutdown(ctxShutdown)

	if err := router.Run(serverAddr); err != nil && err != http.ErrServerClosed {
		log.Fatalf("FATAL: Failed to run Gin server: %v", err)
	}
	log.Println("Gin HTTP server finished.") // Should only be reached if Gin server is explicitly stopped.
}

// func main() {
// 	cfg, err := config.LoadConfig()
// 	if err != nil {
// 		log.Fatalf("Failed to load config: %v", err)
// 	}
// 	log.Printf("Starting server on port %d", cfg.ServerPort)

// 	router := api.SetupRouter(cfg)

// 	// run test as go-routine
// 	go testGDACSFactCheckFromFile()

// 	// TODO: init jobQueue consumer

// 	serverAddr := fmt.Sprintf(":%d", cfg.ServerPort)
// 	go func() {
// 		if err := router.Run(serverAddr); err != nil {
// 			log.Fatalf("Failed to start server: %v", err)
// 		}
// 	}()
// 	log.Printf("Gin server started on port %v", serverAddr)

// 	// graceful shutdown
// 	quit := make(chan os.Signal, 1)
// 	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
// 	<-quit // block until signal is received
// 	log.Println("Shutting down server...")

// 	// TODO: close jobQueue consumer
// 	log.Println("Server stopped")

// }

// Updated test function to load an array of data from test_disaster_report.json
func testGDACSFactCheckFromFile() {
	log.Println("---- Starting GDACS Fact Check Test from test_disaster_report.json (Processing Array) ----")

	filePath := "test.json" // Assuming it's in the root of sentria-factchecker
	jsonData, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("FATAL: Error reading %s: %v", filePath, err)
		log.Println("---- GDACS Fact Check Test Ended: FAILED TO READ TEST FILE ----")
		return
	}

	var reportsToTest []models.DisasterReportData // <--- Changed to a slice
	err = json.Unmarshal(jsonData, &reportsToTest)
	if err != nil {
		log.Printf("FATAL: Error unmarshaling JSON array from %s: %v", filePath, err)
		log.Println("---- GDACS Fact Check Test Ended: FAILED TO PARSE TEST FILE ----")
		return
	}

	if len(reportsToTest) == 0 {
		log.Println("No test reports found in the array within test_disaster_report.json.")
		log.Println("---- GDACS Fact Check Test Completed (No Data) ----")
		return
	}

	log.Printf("Found %d test report(s) in %s. Starting processing...", len(reportsToTest), filePath)

	gdacsClient := external_apis.NewGDACSClient()
	factCheckSvc := service.NewFactCheckService(gdacsClient)

	// Create a parent context with a timeout that covers all test cases
	// Adjust timeout per report if individual API calls are long: time.Duration(len(reportsToTest)) * 30 * time.Second
	parentCtx, parentCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer parentCancel()

	for i, reportData := range reportsToTest {
		log.Printf("\n===== PROCESSING TEST CASE %d/%d: '%s' (Type: %s) =====",
			i+1, len(reportsToTest), reportData.Title, reportData.IncidentType)

		// Create a per-test-case context if needed, or use parentCtx
		// For simplicity, using parentCtx. If one API call hangs long, it might affect others.
		// Alternatively:
		// individualCtx, individualCancel := context.WithTimeout(parentCtx, 45*time.Second)

		result, serviceErr := factCheckSvc.VerifyReportWithGDACS(parentCtx, reportData) // Pass reportData (not reportToTest, which is the slice)

		// individualCancel() // Call if using individual contexts

		if serviceErr != nil {
			log.Printf("  ERROR during VerifyReportWithGDACS for report '%s': %v", reportData.Title, serviceErr)
			// Check if context error (timeout or cancellation)
			if parentCtx.Err() != nil {
				log.Printf("  Parent context error: %v. Aborting further tests.", parentCtx.Err())
				break // Stop processing further reports if parent context is done
			}
			continue // Continue to the next test case on other service errors
		}

		if result != nil {
			log.Printf("  FactCheck Result Status: %s", result.Status)
			log.Printf("  Overall Confidence: %.2f (Raw Calculated Score: %.2f)", result.OverallConfidence, result.CalculatedScore)
			log.Printf("  Narrative: %s", result.Narrative)
			if result.ProcessingError != "" {
				log.Printf("  Processing Error in result: %s", result.ProcessingError)
			}
			if len(result.Evidence) > 0 {
				log.Println("  Evidence Details:")
				for j, ev := range result.Evidence {
					log.Printf("    Evidence %d: [%s] - '%s' (Confidence: %.2f, URL: %s, Time: %s)",
						j+1, ev.Source, ev.Summary, ev.Confidence, ev.URL, ev.Timestamp.Format(time.RFC3339))
				}
			} else {
				log.Println("  No specific corroborating evidence found by GDACS based on criteria for this report.")
			}
		} else {
			log.Printf("  VerifyReportWithGDACS returned a nil result without a service error for report '%s'. This should be investigated.", reportData.Title)
		}
		log.Printf("===== COMPLETED TEST CASE %d/%d: '%s' =====", i+1, len(reportsToTest), reportData.Title)

		// Optional: Add a small delay between processing test cases if you're hitting APIs rapidly
		// time.Sleep(1 * time.Second)
	}
	log.Println("\n---- All GDACS Fact Check Test Cases from File Processed ----")
}

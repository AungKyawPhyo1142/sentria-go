package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AungKyawPhyo1142/sentria-go/internal/api"
	"github.com/AungKyawPhyo1142/sentria-go/internal/config"
	"github.com/AungKyawPhyo1142/sentria-go/internal/core/service"
	"github.com/AungKyawPhyo1142/sentria-go/internal/models"
	"github.com/AungKyawPhyo1142/sentria-go/internal/platform/external_apis"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("Starting server on port %d", cfg.ServerPort)

	router := api.SetupRouter(cfg)

	// run test as go-routine
	go testGDACSFactCheckFromFile()

	// TODO: init jobQueue consumer

	serverAddr := fmt.Sprintf(":%d", cfg.ServerPort)
	go func() {
		if err := router.Run(serverAddr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()
	log.Printf("Gin server started on port %v", serverAddr)

	// graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit // block until signal is received
	log.Println("Shutting down server...")

	// TODO: close jobQueue consumer
	log.Println("Server stopped")

}

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

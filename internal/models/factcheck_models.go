// sentria-factchecker/internal/models/factcheck_models.go
package models

import "time"

// DisasterReportData contains the essential information from a Sentria disaster report
// that will be passed to the fact-checking service (e.g., via a job queue).
type DisasterReportData struct {
	// IDs to identify the report throughout the system
	PostgresReportID string `json:"postgresReportId"` // The CUID from your Node.js app's PostgreSQL 'Report' table
	MongoDocID       string `json:"mongoDocId"`       // The ObjectId string from your Node.js app's MongoDB 'disaster_incidents' collection

	Title             string    `json:"title"`             // Specific title of the disaster event
	Description       string    `json:"description"`       // User-provided description
	IncidentType      string    `json:"incidentType"`      // e.g., "FLOOD", "EARTHQUAKE", "FIRE", "STORM", "LANDSLIDE", "OTHER"
	Severity          string    `json:"severity"`          // e.g., "UNKNOWN", "LOW", "MEDIUM", "HIGH", "CRITICAL"
	IncidentTimestamp time.Time `json:"incidentTimestamp"` // Actual time of the incident

	// Location data
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	City      string  `json:"city"`
	Country   string  `json:"country"`
	Address   string  `json:"address,omitempty"` // Optional: more specific address string

	// Media URLs (assuming URLs are strings)
	Media []struct {
		Type    string `json:"type"` // "IMAGE" or "VIDEO"
		URL     string `json:"url"`
		Caption string `json:"caption,omitempty"`
	} `json:"media,omitempty"`

	ReporterUserID int `json:"reporterUserId"` // User ID (from PG) of the person who created the report
}

// FactCheckEvidence holds a piece of evidence found by a fact-checking source (like GDACS).
type FactCheckEvidence struct {
	Source      string    `json:"source"`              // Name of the API or data source (e.g., "GDACS API", "USGS")
	URL         string    `json:"url,omitempty"`       // URL to the source data if available
	Summary     string    `json:"summary"`             // Brief summary of the finding
	Confidence  float64   `json:"confidence"`          // Confidence score from this specific piece of evidence (0.0 to 1.0)
	Timestamp   time.Time `json:"timestamp,omitempty"` // Timestamp of the evidence/event from the source
	RawResponse string    `json:"-"`                   // Store raw API response for debugging, exclude from JSON by default
}

// FactCheckResult encapsulates the outcome of the entire fact-checking process for a report.
// This is what the Go service will eventually send back to the Node.js backend.
type FactCheckResult struct {
	PostgresReportID  string              `json:"postgresReportId"`          // To identify which report this result is for
	MongoDocID        string              `json:"mongoDocId"`                // To identify which report this result is for
	OverallConfidence float64             `json:"overallConfidence"`         // Combined confidence score (0.0 to 1.0)
	CalculatedScore   float64             `json:"calculatedScore"`           // The raw score before any capping or normalization
	Status            string              `json:"status"`                    // e.g., "CONFIRMED", "PARTIALLY_CONFIRMED", "UNVERIFIABLE", "NEEDS_REVIEW"
	Narrative         string              `json:"narrative"`                 // A summary explaining the overall findings
	Evidence          []FactCheckEvidence `json:"evidence,omitempty"`        // Array of supporting evidence from various sources
	ServiceProvider   string              `json:"serviceProvider"`           // Identifier for this Go fact-checking service
	ProcessingError   string              `json:"processingError,omitempty"` // Any error message if the fact-checking process itself failed for this report
	CheckedAt         time.Time           `json:"checkedAt"`                 // When this fact-check was performed
}

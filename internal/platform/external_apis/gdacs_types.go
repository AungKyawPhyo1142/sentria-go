// sentria-factchecker/internal/platform/external_apis/gdacs_types.go
package external_apis

import (
	"encoding/json" // For json.Number if eventid is still mixed type
	"fmt"
	"time" // For time.Time if we parse date strings
)

// GDACSResponse is the top-level structure.
type GDACSResponse struct {
	Type     string         `json:"type"`
	Features []GDACSFeature `json:"features"` // Changed from Feature to GDACSFeature for clarity
}

// GDACSFeature represents a single disaster event feature.
type GDACSFeature struct {
	Type       string          `json:"type"`
	Properties GDACSProperties `json:"properties"` // Changed from Properties to GDACSProperties
	Geometry   GDACSGeometry   `json:"geometry"`   // Assuming Geometry structure is still needed
	ID         string          `json:"id"`         // Standard GeoJSON feature ID, often same as eventid or combination
}

// GDACSSeverityData holds specific severity information.
type GDACSSeverityData struct {
	Severity     float32 `json:"severity"`     // e.g., magnitude for earthquakes
	SeverityText string  `json:"severitytext"` // e.g., "M 5.8"
	SeverityUnit string  `json:"severityunit"` // e.g., "Richter" or other units
}

// GDACSProperties contains the detailed attributes of a GDACS event.
// This version merges fields from your working example and our previous one.
type GDACSProperties struct {
	// Fields from your working example
	Name            string            `json:"name"`            // Primary name/title of the event
	Description     string            `json:"description"`     // Detailed description
	HtmlDescription string            `json:"htmldescription"` // HTML version of description
	Glide           string            `json:"glide"`           // GLIDE number
	IsCurrent       string            `json:"iscurrent"`       // "true" or "false"
	Source          string            `json:"source"`          // Data source (e.g., USGS, JRC)
	AlertLevel      string            `json:"alertlevel"`      // "Green", "Orange", "Red"
	FromDateStr     string            `json:"fromdate"`        // Date string from API
	ToDateStr       string            `json:"todate"`          // Date string from API
	EventType       string            `json:"eventtype"`       // e.g., "EQ", "FL", "TC"
	SeverityData    GDACSSeverityData `json:"severitydata"`    // Nested severity information

	// Additional potentially useful fields from our earlier struct / common GDACS structure
	EventID   json.Number `json:"eventid"`   // GDACS internal event ID (using json.Number for flexibility)
	EpisodeID json.Number `json:"episodeid"` // GDACS internal episode ID
	Country   string      `json:"country"`   // Country name
	Link      string      `json:"link"`      // Link to GDACS report page

	// Parsed date fields (not directly from JSON, but can be populated after parsing FromDateStr/ToDateStr)
	FromDate time.Time `json:"-"` // Ignored by JSON unmarshal, populated manually
	ToDate   time.Time `json:"-"` // Ignored by JSON unmarshal, populated manually
}

// GDACSGeometry contains the location of the event.
type GDACSGeometry struct {
	Type        string    `json:"type"`        // e.g., "Point"
	Coordinates []float64 `json:"coordinates"` // [longitude, latitude, Optional: depth/altitude]
}

// GDACSQueryParameters holds parameters for the GDACS event search API.
type GDACSQueryParameters struct {
	EventListType string // Mapped GDACS event type (e.g., "EQ", "FL") for the 'eventlist' param
	FromDate      string // Start of the date range (YYYY-MM-DD format)
	ToDate        string // End of the date range (YYYY-MM-DD format)
	Country       string // Country name or potentially ISO code (API behavior dependent)

	// These are kept for potential client-side filtering if the API search is too broad
	ReportLatitude  float64
	ReportLongitude float64
}

// --- Helper function to parse date strings after unmarshaling ---
// You might call this in your GDACS client after successfully decoding the JSON.
// GDACS date format from your example seems to be "2006-01-02T15:04:05" (without Z)
const gdacsDateLayout = "2006-01-02T15:04:05" // Define the expected layout

func (p *GDACSProperties) ParseDates() error {
	var err error
	if p.FromDateStr != "" {
		p.FromDate, err = time.Parse(gdacsDateLayout, p.FromDateStr)
		if err != nil {
			return fmt.Errorf("error parsing FromDateStr '%s': %w", p.FromDateStr, err)
		}
	}
	if p.ToDateStr != "" {
		p.ToDate, err = time.Parse(gdacsDateLayout, p.ToDateStr)
		if err != nil {
			return fmt.Errorf("error parsing ToDateStr '%s': %w", p.ToDateStr, err)
		}
	}
	return nil
}

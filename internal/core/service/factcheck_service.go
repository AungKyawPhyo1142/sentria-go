package service

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/AungKyawPhyo1142/sentria-go/internal/models"
	"github.com/AungKyawPhyo1142/sentria-go/internal/platform/external_apis"
)

type FactCheckService struct {
	gdacsClient *external_apis.GDACSClient
}

func NewFactCheckService(gdacsClient *external_apis.GDACSClient) *FactCheckService {
	return &FactCheckService{
		gdacsClient: gdacsClient,
	}
}

func mapSentriaToGDACSEventType(sentriaEventType string) string {
	switch strings.ToUpper(sentriaEventType) {
	case "EARTHQUAKE":
		return "EQ"
	case "FLOOD":
		return "FL"
	case "STORM":
		return "TC"
	case "FIRE":
		return "WF"
	default:
		return ""
	}
}

// Haversine distance formula to calculate the distance between two points on the Earth's surface
func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371.0 // Earth's radius in kilometers
	dLat := (lat2 - lat1) * (math.Pi / 180.0)
	dLon := (lon2 - lon1) * (math.Pi / 180.0)
	lat1R := lat1 * (math.Pi / 180.0)
	lat2R := lat2 * (math.Pi / 180.0)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Sin(dLon/2)*math.Sin(dLon/2)*math.Cos(lat1R)*math.Cos(lat2R)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

// Scoring weights (can be tuned)
const (
	scoreBaseMatchIfFound   = 0.1 // Base score if GDACS returns any event for the query
	scoreExactTypeMatch     = 0.25
	scoreTimeWindowMatch    = 0.2 // If GDACS event time is very close to reported time
	scoreCountryMatch       = 0.1
	scoreCloseProximity     = 0.15 // Bonus for very close events
	scoreModerateProximity  = 0.05 // Bonus for moderate proximity
	scoreHighAlertGDACS     = 0.15 // For Red or Orange GDACS alerts
	scoreSeverityDataMatch  = 0.1  // If numeric severity data aligns (e.g. magnitude for EQ)
	maxPossibleScore        = 1.0
	relevantTimeWindowHours = 24.0
	closeProximityKm        = 50.0
	moderateProximityKm     = 200.0
)

// VerifyReportWithGDACS with updated scoring logic using new structs
func (s *FactCheckService) VerifyReportWithGDACS(ctx context.Context, reportData models.DisasterReportData) (*models.FactCheckResult, error) {
	log.Printf("[FactCheckService] Verifying report '%s' (Type: %s, Country: %s) with GDACS Scoring.",
		reportData.ReportName, reportData.IncidentType, reportData.Country)

	gdacsEventTypeMapped := mapSentriaToGDACSEventType(reportData.IncidentType)

	reportDay := reportData.IncidentTimestamp
	dayBefore := reportDay.AddDate(0, 0, -1)
	dayAfter := reportDay.AddDate(0, 0, 1)
	gdacsDateFormat := "2006-01-02"

	queryParams := external_apis.GDACSQueryParameters{
		EventListType:   gdacsEventTypeMapped,
		FromDate:        dayBefore.Format(gdacsDateFormat),
		ToDate:          dayAfter.Format(gdacsDateFormat),
		Country:         reportData.Country,
		ReportLatitude:  reportData.Latitude,
		ReportLongitude: reportData.Longitude,
	}
	// ... (log queryParams) ...

	gdacsEvents, err := s.gdacsClient.FetchEventsBySearch(ctx, queryParams)
	// ... (error handling for API call itself remains the same) ...
	if err != nil {
		// ... existing error handling ...
		log.Printf("[FactCheckService] Error fetching events from GDACS search: %v", err)
		return &models.FactCheckResult{
			PostgresReportID: reportData.PostgresReportID, MongoDocID: reportData.MongoDocID,
			OverallConfidence: 0.0, CalculatedScore: 0.0, Status: "ERROR_FETCHING_SOURCE_GDACS",
			Narrative: "Could not query GDACS for events using search criteria.", ProcessingError: err.Error(),
			ServiceProvider: "SentriaGoFactChecker/GDACS", CheckedAt: time.Now(),
		}, nil
	}

	if len(gdacsEvents) == 0 {
		// ... existing no data handling ...
		log.Println("[FactCheckService] No events returned by GDACS search for the given criteria.")
		return &models.FactCheckResult{
			PostgresReportID: reportData.PostgresReportID, MongoDocID: reportData.MongoDocID,
			OverallConfidence: 0.05, CalculatedScore: 0.05, Status: "UNVERIFIABLE_NO_GDACS_DATA",
			Narrative:       "No events found in GDACS matching the specified disaster type, date window, and country criteria from API.",
			ServiceProvider: "SentriaGoFactChecker/GDACS", CheckedAt: time.Now(),
		}, nil
	}

	bestScore := 0.0
	var bestMatchEvidence []models.FactCheckEvidence
	var bestMatchNarrative strings.Builder

	log.Printf("[FactCheckService] GDACS search returned %d events. Performing scoring and client-side filtering...", len(gdacsEvents))

	for _, event := range gdacsEvents {
		currentFactScore := 0.0
		var currentEventEvidence []models.FactCheckEvidence // Evidence specific to this GDACS event
		var currentNarrativeParts []string

		props := event.Properties
		eventIDStr := props.EventID.String() // Use .String() for json.Number

		currentFactScore += scoreBaseMatchIfFound // Base score for event being returned by query
		currentNarrativeParts = append(currentNarrativeParts, fmt.Sprintf("GDACS event '%s'(%s) considered.", props.Name, eventIDStr))

		// 1. Precise Event Type Match
		if gdacsEventTypeMapped != "" && strings.EqualFold(props.EventType, gdacsEventTypeMapped) {
			currentFactScore += scoreExactTypeMatch
			currentNarrativeParts = append(currentNarrativeParts, "Exact disaster type match.")
			currentEventEvidence = append(currentEventEvidence, models.FactCheckEvidence{Source: "GDACS", Summary: "Disaster type matches.", Confidence: scoreExactTypeMatch, Timestamp: props.FromDate})
		} // ... (else if for mismatch, etc. as before) ...

		// 2. Precise Time Window Match
		// props.FromDate is now time.Time after ParseDates()
		if !props.FromDate.IsZero() { // Check if date parsing was successful
			timeDiffHours := math.Abs(reportData.IncidentTimestamp.Sub(props.FromDate).Hours())
			if timeDiffHours <= relevantTimeWindowHours {
				currentFactScore += scoreTimeWindowMatch
				currentNarrativeParts = append(currentNarrativeParts, fmt.Sprintf("Event time within +/-%.0f hours (%.1f hours diff).", relevantTimeWindowHours, timeDiffHours))
				currentEventEvidence = append(currentEventEvidence, models.FactCheckEvidence{Source: "GDACS", Summary: "Event time consistent.", Confidence: scoreTimeWindowMatch, Timestamp: props.FromDate})
			} else {
				currentNarrativeParts = append(currentNarrativeParts, fmt.Sprintf("Event time (%.1f hours diff) outside +/-%.0f hour window.", timeDiffHours, relevantTimeWindowHours))
				continue // Skip this event if time is too far off
			}
		} else {
			currentNarrativeParts = append(currentNarrativeParts, "GDACS event 'fromdate' could not be parsed or was empty.")
		}

		// 3. Country Match
		if reportData.Country != "" && props.Country != "" && strings.EqualFold(reportData.Country, props.Country) {
			currentFactScore += scoreCountryMatch
			currentNarrativeParts = append(currentNarrativeParts, "Country match.")
			currentEventEvidence = append(currentEventEvidence, models.FactCheckEvidence{Source: "GDACS", Summary: "Country matches.", Confidence: scoreCountryMatch, Timestamp: props.FromDate})
		} // ... (else if for mismatch) ...

		// 4. Location Proximity
		var eventLon, eventLat float64
		if len(event.Geometry.Coordinates) >= 2 {
			eventLon = event.Geometry.Coordinates[0]
			eventLat = event.Geometry.Coordinates[1]
			distance := haversine(reportData.Latitude, reportData.Longitude, eventLat, eventLon)
			currentNarrativeParts = append(currentNarrativeParts, fmt.Sprintf("Distance: %.0fkm.", distance))
			if distance <= closeProximityKm {
				currentFactScore += scoreCloseProximity
				currentNarrativeParts = append(currentNarrativeParts, "Close proximity.")
				currentEventEvidence = append(currentEventEvidence, models.FactCheckEvidence{Source: "GDACS", Summary: fmt.Sprintf("Located within %.0fkm.", closeProximityKm), Confidence: scoreCloseProximity, Timestamp: props.FromDate})
			} else if distance <= moderateProximityKm {
				currentFactScore += scoreModerateProximity
				currentNarrativeParts = append(currentNarrativeParts, "Moderate proximity.")
				currentEventEvidence = append(currentEventEvidence, models.FactCheckEvidence{Source: "GDACS", Summary: fmt.Sprintf("Located within %.0fkm.", moderateProximityKm), Confidence: scoreModerateProximity, Timestamp: props.FromDate})
			} else {
				currentNarrativeParts = append(currentNarrativeParts, "Distant event.")
				continue // If too distant, this event is likely not the primary corroborating one
			}
		} else {
			currentNarrativeParts = append(currentNarrativeParts, "GDACS event missing coordinates for proximity check.")
		}

		// 5. Alert Level
		if strings.EqualFold(props.AlertLevel, "Red") || strings.EqualFold(props.AlertLevel, "Orange") {
			currentFactScore += scoreHighAlertGDACS
			currentNarrativeParts = append(currentNarrativeParts, fmt.Sprintf("High alert level (%s).", props.AlertLevel))
			currentEventEvidence = append(currentEventEvidence, models.FactCheckEvidence{Source: "GDACS", Summary: fmt.Sprintf("Alert level is %s.", props.AlertLevel), Confidence: scoreHighAlertGDACS, Timestamp: props.FromDate})
		} else {
			currentNarrativeParts = append(currentNarrativeParts, fmt.Sprintf("Alert level: %s.", props.AlertLevel))
		}

		// 6. SeverityData (NEW - using your struct)
		if props.EventType == "EQ" && props.SeverityData.Severity > 0 { // Example for Earthquake magnitude
			currentNarrativeParts = append(currentNarrativeParts, fmt.Sprintf("GDACS Severity: %.1f %s.", props.SeverityData.Severity, props.SeverityData.SeverityUnit))
			if props.SeverityData.Severity >= 4.5 { // Example threshold for significant EQ
				currentFactScore += scoreSeverityDataMatch
				currentEventEvidence = append(currentEventEvidence, models.FactCheckEvidence{Source: "GDACS", Summary: fmt.Sprintf("Severity data indicates magnitude %.1f %s.", props.SeverityData.Severity, props.SeverityData.SeverityUnit), Confidence: scoreSeverityDataMatch, Timestamp: props.FromDate})
			}
		}
		// Add similar checks for other disaster types if SeverityData is relevant and structured

		// Cap score
		if currentFactScore > maxPossibleScore {
			currentFactScore = maxPossibleScore
		}
		if currentFactScore < 0 {
			currentFactScore = 0
		}

		log.Printf("[FactCheckService] GDACS Event %s (%s) processed. Score for this event: %.2f. Narrative points: %s",
			eventIDStr, props.Name, currentFactScore, strings.Join(currentNarrativeParts, " | "))

		if currentFactScore > bestScore {
			bestScore = currentFactScore
			mainEventSummary := fmt.Sprintf("Best match in GDACS: %s (ID: %s, Type: %s, Alert: %s, Country: %s, Severity: %.1f %s). ",
				props.Name, eventIDStr, props.EventType, props.AlertLevel, props.Country, props.SeverityData.Severity, props.SeverityData.SeverityUnit)

			bestMatchNarrative.Reset()
			bestMatchNarrative.WriteString(mainEventSummary)
			bestMatchNarrative.WriteString(strings.Join(currentNarrativeParts, " "))

			// Add a main evidence piece for the best matching event
			bestMatchEvidence = []models.FactCheckEvidence{{
				Source:     "GDACS Event (Best Match)",
				URL:        "https://www.gdacs.org" + props.Link, // Make sure props.Link is populated
				Summary:    mainEventSummary,
				Confidence: currentFactScore, // Use the score of this best match as its confidence contribution
				Timestamp:  props.FromDate,
			}}
			bestMatchEvidence = append(bestMatchEvidence, currentEventEvidence...) // Append detailed points
		}
	}

	finalCalculatedScore := bestScore // Raw score before final capping for OverallConfidence
	finalOverallConfidence := bestScore
	if finalOverallConfidence > maxPossibleScore {
		finalOverallConfidence = maxPossibleScore
	}
	if finalOverallConfidence < 0 {
		finalOverallConfidence = 0
	}

	finalStatus := "UNVERIFIED_BY_GDACS_ANALYSIS"
	if finalOverallConfidence >= 0.7 {
		finalStatus = "STRONGLY_CORROBORATED_BY_GDACS"
	} else if finalOverallConfidence >= 0.4 {
		finalStatus = "PARTIALLY_CORROBORATED_BY_GDACS"
	} else if finalOverallConfidence > 0.15 { // Slightly above just base match
		finalStatus = "WEAKLY_CORROBORATED_BY_GDACS"
	}

	finalNarrative := "No significant corroborating event found in GDACS after detailed analysis."
	if bestScore > 0.0 || len(gdacsEvents) > 0 {
		finalNarrative = fmt.Sprintf("GDACS Analysis: %s", bestMatchNarrative.String())
	}

	return &models.FactCheckResult{
		PostgresReportID:  reportData.PostgresReportID,
		MongoDocID:        reportData.MongoDocID,
		OverallConfidence: finalOverallConfidence,
		CalculatedScore:   finalCalculatedScore,
		Status:            finalStatus,
		Narrative:         finalNarrative,
		Evidence:          bestMatchEvidence,
		ServiceProvider:   "SentriaGoFactChecker/GDACS",
		CheckedAt:         time.Now(),
	}, nil
}

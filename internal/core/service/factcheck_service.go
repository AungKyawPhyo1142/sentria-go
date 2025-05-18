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

// const (
// 	// Proximity thresholds for scoring
// 	closeProximityKm    = 50.0  // For a higher score bonus
// 	moderateProximityKm = 200.0 // General threshold for considering an event
// 	// Date format for GDACS API query
// 	gdacsDateFormat = "2006-01-02"
// 	// Time window for considering an event relevant (e.g. +/- 24 hours)
// 	relevantTimeWindowHours = 24.0
// )

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
		reportData.Title, reportData.IncidentType, reportData.Country)

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

// func (s *FactCheckService) VerifyReportWithGDACS(ctx context.Context, reportData models.DisasterReportData) (*models.FactCheckResult, error) {
// 	log.Printf("[FactCheckService] Verifying report '%s' (Type: %s, Country: %s) with GDACS Scoring.",
// 		reportData.Title, reportData.IncidentType, reportData.Country)

// 	gdacsEventTypeMapped := mapSentriaToGDACSEventType(reportData.IncidentType)

// 	reportDay := reportData.IncidentTimestamp
// 	dayBefore := reportDay.AddDate(0, 0, -1)
// 	dayAfter := reportDay.AddDate(0, 0, 1)

// 	queryParams := external_apis.GDACSQueryParameters{
// 		EventListType:   gdacsEventTypeMapped,
// 		FromDate:        dayBefore.Format(gdacsDateFormat),
// 		ToDate:          dayAfter.Format(gdacsDateFormat),
// 		Country:         reportData.Country,
// 		ReportLatitude:  reportData.Latitude,
// 		ReportLongitude: reportData.Longitude,
// 	}

// 	log.Printf("[FactCheckService] Querying GDACS with: Type=%s, From=%s, To=%s, Country=%s",
// 		queryParams.EventListType, queryParams.FromDate, queryParams.ToDate, queryParams.Country)

// 	gdacsEvents, err := s.gdacsClient.FetchEventsBySearch(ctx, queryParams)
// 	if err != nil {
// 		log.Printf("[FactCheckService] Error fetching events from GDACS: %v", err)
// 		return &models.FactCheckResult{
// 			OverallConfidence: 0.0,
// 			Status:            "ERROR_FETCHING_EVENTS_FROM_GDACS",
// 			Narrative:         "Could not query GDACS for events",
// 			ProcessingError:   err.Error(),
// 		}, nil
// 	}

// 	if len(gdacsEvents) == 0 {
// 		log.Printf("[FactCheckService] No matching events found in GDACS for report '%s'.", reportData.Title)
// 		return &models.FactCheckResult{
// 			OverallConfidence: 0.05,
// 			Status:            "UNVERIFIABLE_NO_GDACS_DATA",
// 			Narrative:         "No events found in GDACS matching the specified disaster type, date window, and country criteria from API.",
// 		}, nil
// 	}

// 	log.Printf("[FactCheckService] Found %d matching events in GDACS for report '%s'.", len(gdacsEvents), reportData.Title)

// 	bestScore := 0.0
// 	var bestMatchEvidence []models.FactCheckEvidence
// 	var bestMatchNarrative strings.Builder
// 	bestMatchNarrative.WriteString("Initial analysis: ")

// 	for _, event := range gdacsEvents {
// 		currentScore := 0.0
// 		var currentEvidence []models.FactCheckEvidence
// 		var currentNarrativeParts []string

// 		props := event.Properties

// 		// 0. Base assumption: if GDACS returns it based on type/date/country, it's a potential match
// 		currentScore += scoreBaseMatch
// 		currentNarrativeParts = append(currentNarrativeParts, fmt.Sprintf("GDACS event '%s', EventID(%s) matches basic criteria.", props.Title, props.EventID))

// 		// 1. Precise Event Type Match (if our type was mappable)
// 		if gdacsEventTypeMapped != "" && strings.EqualFold(props.EventType, gdacsEventTypeMapped) {
// 			currentScore += scoreExactTypeMatch
// 			currentNarrativeParts = append(currentNarrativeParts, "Exact disaster type match.")
// 			currentEvidence = append(currentEvidence, models.FactCheckEvidence{
// 				Source: "GDACS", Summary: "Disaster type matches.", Confidence: scoreExactTypeMatch, Timestamp: props.FromDate,
// 			})
// 		} else if gdacsEventTypeMapped == "" && reportData.IncidentType == "OTHER" {
// 			// If Sentria type is "OTHER", we don't penalize for type mismatch but don't add score either.
// 			currentNarrativeParts = append(currentNarrativeParts, "Sentria report type is 'OTHER', GDACS type is '"+props.EventType+"'.")
// 		} else if gdacsEventTypeMapped != "" && !strings.EqualFold(props.EventType, gdacsEventTypeMapped) {
// 			currentNarrativeParts = append(currentNarrativeParts, "GDACS event type '"+props.EventType+"' differs from expected '"+gdacsEventTypeMapped+"'.")
// 			currentScore -= 0.1 // Penalize slightly for type mismatch if a specific type was expected
// 		}

// 		// 2. Precise Time Window Match (+/- relevantTimeWindowHours)
// 		timeDiffHours := math.Abs(reportData.IncidentTimestamp.Sub(props.FromDate).Hours())
// 		if timeDiffHours <= relevantTimeWindowHours {
// 			currentScore += scoreTimeWindowMatch
// 			currentNarrativeParts = append(currentNarrativeParts, fmt.Sprintf("Event time within +/-%.0f hours (%.1f hours diff).", relevantTimeWindowHours, timeDiffHours))
// 			currentEvidence = append(currentEvidence, models.FactCheckEvidence{
// 				Source: "GDACS", Summary: "Event time is consistent.", Confidence: scoreTimeWindowMatch, Timestamp: props.FromDate,
// 			})
// 		} else {
// 			currentNarrativeParts = append(currentNarrativeParts, fmt.Sprintf("Event time (%.1f hours diff) outside +/-%.0f hour window.", timeDiffHours, relevantTimeWindowHours))
// 			continue // If time is too far off, this event is likely not relevant.
// 		}

// 		// 3. Country Match (case-insensitive, direct property)
// 		if reportData.Country != "" && props.Country != "" && strings.EqualFold(reportData.Country, props.Country) {
// 			currentScore += scoreCountryMatch
// 			currentNarrativeParts = append(currentNarrativeParts, "Country match.")
// 			currentEvidence = append(currentEvidence, models.FactCheckEvidence{
// 				Source: "GDACS", Summary: "Country matches.", Confidence: scoreCountryMatch, Timestamp: props.FromDate,
// 			})
// 		} else if reportData.Country != "" && props.Country != "" && !strings.EqualFold(reportData.Country, props.Country) {
// 			currentNarrativeParts = append(currentNarrativeParts, fmt.Sprintf("Country mismatch (Reported: %s, GDACS: %s).", reportData.Country, props.Country))
// 			currentScore -= 0.1 // Penalize for country mismatch if both are specified
// 			// Depending on strictness, you might 'continue' here
// 		}

// 		// 4. Location Proximity
// 		var eventLon, eventLat float64
// 		if len(event.Geometry.Coordinates) >= 2 {
// 			eventLon = event.Geometry.Coordinates[0]
// 			eventLat = event.Geometry.Coordinates[1]
// 			distance := haversine(reportData.Latitude, reportData.Longitude, eventLat, eventLon)
// 			currentNarrativeParts = append(currentNarrativeParts, fmt.Sprintf("Distance: %.0fkm.", distance))
// 			if distance <= closeProximityKm {
// 				currentScore += scoreCloseProximity
// 				currentNarrativeParts = append(currentNarrativeParts, "Close proximity.")
// 				currentEvidence = append(currentEvidence, models.FactCheckEvidence{
// 					Source: "GDACS", Summary: fmt.Sprintf("Located within %.0fkm.", closeProximityKm), Confidence: scoreCloseProximity, Timestamp: props.FromDate,
// 				})
// 			} else if distance <= moderateProximityKm {
// 				// currentScore += scoreModerateProximity (already covered by base score somewhat)
// 				currentNarrativeParts = append(currentNarrativeParts, "Moderate proximity.")
// 			} else {
// 				currentNarrativeParts = append(currentNarrativeParts, "Distant event.")
// 				// If too distant, might not be the best match, reduce score or skip
// 				currentScore -= 0.1
// 				continue // If very distant, this event is probably not the one we are looking for.
// 			}
// 		} else {
// 			currentNarrativeParts = append(currentNarrativeParts, "GDACS event missing coordinates.")
// 		}

// 		// 5. Alert Level
// 		if strings.EqualFold(props.AlertLevel, "Red") || strings.EqualFold(props.AlertLevel, "Orange") {
// 			currentScore += scoreHighAlert
// 			currentNarrativeParts = append(currentNarrativeParts, fmt.Sprintf("High alert level (%s).", props.AlertLevel))
// 			currentEvidence = append(currentEvidence, models.FactCheckEvidence{
// 				Source: "GDACS", Summary: fmt.Sprintf("Alert level is %s.", props.AlertLevel), Confidence: scoreHighAlert, Timestamp: props.FromDate,
// 			})
// 		} else {
// 			currentNarrativeParts = append(currentNarrativeParts, fmt.Sprintf("Alert level: %s.", props.AlertLevel))
// 		}

// 		// Cap score and ensure it's not negative
// 		if currentScore > maxPossibleScore {
// 			currentScore = maxPossibleScore
// 		}
// 		if currentScore < 0 {
// 			currentScore = 0
// 		}

// 		log.Printf("[FactCheckService] GDACS Event %s (%s) processed. Current Score for this event: %.2f. Details: %s",
// 			props.EventID, props.Title, currentScore, strings.Join(currentNarrativeParts, " "))

// 		if currentScore > bestScore {
// 			bestScore = currentScore
// 			// Add the main event link as a key piece of evidence for the best match
// 			mainEvidence := models.FactCheckEvidence{
// 				Source:     "GDACS Event Search API (Best Match)",
// 				URL:        "https://www.gdacs.org" + props.Link,
// 				Summary:    fmt.Sprintf("Best match in GDACS: %s (ID: %s, Type: %s, Alert: %s, Country: %s)", props.Title, props.EventID, props.EventType, props.AlertLevel, props.Country),
// 				Confidence: bestScore, // Reflect the overall score for this specific source match
// 				Timestamp:  props.FromDate,
// 			}
// 			bestMatchEvidence = append([]models.FactCheckEvidence{mainEvidence}, currentEvidence...) // Prepend main summary
// 			bestMatchNarrative.Reset()
// 			bestMatchNarrative.WriteString(strings.Join(currentNarrativeParts, " "))
// 		}

// 	}

// 	finalStatus := "UNVERIFIED_BY_GDACS_ANALYSIS"
// 	if bestScore >= 0.7 {
// 		finalStatus = "STRONGLY_CORROBORATED_BY_GDACS"
// 	} else if bestScore >= 0.4 {
// 		finalStatus = "PARTIALLY_CORROBORATED_BY_GDACS"
// 	} else if bestScore > 0.1 { // scoreBaseMatch implies some relevance
// 		finalStatus = "WEAKLY_CORROBORATED_BY_GDACS"
// 	}

// 	if bestScore > 0.0 || len(gdacsEvents) > 0 { // If we processed any events or had a score
// 		return &models.FactCheckResult{
// 			OverallConfidence: bestScore,
// 			Status:            finalStatus,
// 			Narrative:         fmt.Sprintf("GDACS Analysis: %s (Overall Score: %.2f)", bestMatchNarrative.String(), bestScore),
// 			Evidence:          bestMatchEvidence,
// 		}, nil
// 	}

// 	// Fallback if no events were relevant enough to even start scoring significantly
// 	return &models.FactCheckResult{
// 		OverallConfidence: 0.1, // Default low if some events returned but none matched well
// 		Status:            "LOW_CORROBORATION_GDACS",
// 		Narrative:         "GDACS search returned events, but none closely matched all combined criteria after detailed analysis.",
// 	}, nil

// }

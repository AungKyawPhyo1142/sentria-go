package external_apis

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"
)

const (
	gdacsSearchEventListURL = "https://www.gdacs.org/gdacsapi/api/Events/geteventlist/search"
	defaultAPITimeout       = 15 * time.Second
)

type GDACSClient struct {
	httpClient *http.Client
}

func NewGDACSClient() *GDACSClient {
	return &GDACSClient{
		httpClient: &http.Client{
			Timeout: defaultAPITimeout,
		},
	}
}

func (c *GDACSClient) FetchEventsBySearch(ctx context.Context, params GDACSQueryParameters) ([]GDACSFeature, error) {
	apiURL, err := url.Parse(gdacsSearchEventListURL)
	if err != nil {
		log.Printf("[GDACSClient] Error parsing URL: %v", err)
		return nil, fmt.Errorf("error parsing URL: %w", err)
	}

	queryParams := url.Values{}

	if params.EventListType != "" {
		queryParams.Set("eventlist", params.EventListType)
	}
	if params.FromDate != "" {
		queryParams.Set("fromDate", params.FromDate)
	}
	if params.ToDate != "" {
		queryParams.Set("toDate", params.ToDate)
	}
	if params.Country != "" {
		queryParams.Set("country", params.Country)
	}

	apiURL.RawQuery = queryParams.Encode()
	fullURL := apiURL.String()
	log.Printf("[GDACSClient] Fetching events from search URL: %s", fullURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		log.Printf("[GDACSClient] Error creating request: %v", err)
		return nil, fmt.Errorf("error creating GDACS search request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("[GDACSClient] Error making request: %v", err)
		return nil, fmt.Errorf("error making GDACS search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[GDACSClient] Unexpected status code: %d", resp.StatusCode)
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var gdacsResp GDACSResponse
	if err := json.NewDecoder(resp.Body).Decode(&gdacsResp); err != nil {
		log.Printf("[GDACSClient] Error decoding JSON response: %v", err)
		return nil, fmt.Errorf("error decoding GDACS search JSON response: %w", err)
	}

	// Parse date strings within properties
	for i := range gdacsResp.Features {
		if err := gdacsResp.Features[i].Properties.ParseDates(); err != nil {
			// Log the error but potentially continue with other features or the feature with partial data
			log.Printf("[GDACSClient] Error parsing dates for event %s (Name: %s): %v. FromDateStr: %s, ToDateStr: %s",
				gdacsResp.Features[i].Properties.EventID.String(),
				gdacsResp.Features[i].Properties.Name,
				err,
				gdacsResp.Features[i].Properties.FromDateStr,
				gdacsResp.Features[i].Properties.ToDateStr)
			// Depending on strictness, you might choose to exclude this feature or handle it.
			// For now, we'll keep the feature but its FromDate/ToDate time.Time fields might be zero.
		}
	}

	log.Printf("[GDACSClient] Successfully fetched and processed %d features from GDACS search.", len(gdacsResp.Features))
	return gdacsResp.Features, nil

}

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Credentials stores user login information
type Credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginResponse represents the structure of the login API response
type LoginResponse struct {
	AccessToken string `json:"accessToken"`
	// Other fields can be added here if needed
}

// SummaryRequest represents the request body for the box summary endpoint
type SummaryRequest struct {
	Month int `json:"month"`
	Year  int `json:"year"`
}

// BoxData represents the structure of a single box in the response
type BoxData struct {
	BoxId          int     `json:"boxId"`
	Name           string  `json:"name"`
	LastAlive      string  `json:"lastAlive"`
	LastProduction string  `json:"lastProduction"`
	SerialNumber   string  `json:"serialNumber"`
	TotalMonth     int     `json:"totalMonth"`
	WattHour       int     `json:"wattHour"`
	TotalDay       int     `json:"totalDay"`
	Year           int     `json:"year"`
	Month          int     `json:"month"`
	LastDbm        int     `json:"lastDbm"`
	Power          int     `json:"power"`
	Weather        *string `json:"weather"`
}

// SummaryResponse represents the array of box data returned by the API
type SummaryResponse []BoxData

func fetchBeemData(config *Config) {

	if config.Token != "" && !isTokenExpired(config.Token) {
		slog.Info("beem already logged with existing access token")

	} else {

		err := login(config)
		if err != nil {
			slog.Error("beem login failed", "error", err)
			return
		}
		slog.Info("beem successfully logged in and got access token")
	}

	// Step 2: Get the box summary for current month and year
	slog.Debug("fetching box summary for current month and year")
	summary, err := getBoxSummary(config.Token)
	if err != nil {
		slog.Error("beem: failed to get box summary", "error", err)
		return
	}

	// Process and display the summary data
	slog.Debug("processing box summary data")
	processData(summary, config.Debug)
}

func login(config *Config) error {
	// Convert credentials to JSON

	var credentials Credentials
	credentials.Email = config.BeemEmail
	credentials.Password = config.BeemPassword

	jsonData, err := json.Marshal(credentials)
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	// Create the HTTP request
	req, err := http.NewRequest("POST", baseURL+loginEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")

	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("failed to close response body", "error", cerr)
		}
	}()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	// Check if response status code is not successful
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("login failed with status code %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response
	var loginResp LoginResponse
	if err := json.Unmarshal(body, &loginResp); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if loginResp.AccessToken == "" {
		return fmt.Errorf("no access token in response")
	}

	config.Token = loginResp.AccessToken
	slog.Debug("beem login successful", "accessToken", config.Token)

	return nil
}

func isTokenExpired(token string) bool {
	// Split the token into parts
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return true // Invalid token format
	}

	// Decode the payload (second part)
	payload := parts[1]

	// Add padding if needed for base64 decoding
	for len(payload)%4 != 0 {
		payload += "="
	}

	// Decode base64 URL
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return true // Failed to decode
	}

	// Parse JSON
	var claims map[string]interface{}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return true // Failed to parse JSON
	}

	// Check expiration time
	exp, exists := claims["exp"]
	if !exists {
		return true // No expiration claim
	}

	// Convert to int64 (Unix timestamp)
	expTime, ok := exp.(float64)
	if !ok {
		return true // Invalid exp format
	}

	// Compare with current time
	return time.Now().Unix() > int64(expTime)
}

func getBoxSummary(token string) (SummaryResponse, error) {
	// Get current month and year
	now := time.Now()
	month := now.Month()
	year := now.Year()

	// Create request body
	summaryReq := SummaryRequest{
		Month: int(month),
		Year:  year,
	}

	// Convert to JSON
	jsonData, err := json.Marshal(summaryReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal summary request: %w", err)
	}

	// Create the HTTP request
	req, err := http.NewRequest("POST", baseURL+summaryEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	slog.Debug("box summary request")
	// Make the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("failed to close response body", "error", cerr)
		}
	}()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check if response status code is not successful
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("summary request failed with status code %d: %s", resp.StatusCode, string(body))
	}

	// Parse the response
	var summaryResp SummaryResponse
	if err := json.Unmarshal(body, &summaryResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return summaryResp, nil
}

func processData(data SummaryResponse, debug bool) {
	// Print raw data for debugging
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		slog.Error("Failed to marshal summary data", "error", err)
		return
	}

	slog.Debug("box summary", "data", string(jsonData))

	// Process each box and publish to MQTT
	for _, box := range data {
		slog.Info("beem processing box", "name", box.Name, "id", box.BoxId)

		// Create device ID and name based on box details
		deviceId := fmt.Sprintf("beem_energy_%s", strings.ToLower(box.SerialNumber))
		deviceName := fmt.Sprintf("Beem Energy %s", box.Name)

		// Publish device configuration and data to MQTT
		publishBoxToMQTT(box, deviceId, deviceName)

		slog.Info("data summary", "current Power", box.WattHour, "daily production", box.TotalDay, "monthly production", box.TotalMonth, "last Alive", box.LastAlive, "signal strength", box.LastDbm)

		// Calculate and display last alive duration
		lastAliveDuration := calculateLastContactDuration(box.LastAlive)
		if lastAliveDuration < 0 {
			slog.Error("unable to calculate last alive")
		}

		// Calculate and display last production duration
		lastProductionDuration := calculateLastContactDuration(box.LastProduction)
		if lastProductionDuration < 0 {
			slog.Error("unable to calculate lasy productiopn")
		}

	}
}

// Calculate duration since last contact in seconds
func calculateLastContactDuration(lastAliveStr string) int {
	// Parse the ISO 8601 timestamp
	lastAlive, err := time.Parse(time.RFC3339, lastAliveStr)
	if err != nil {
		slog.Error("failed to parse lastalive timestamp", "error", err)
		return -1
	}

	// Calculate duration since last contact
	now := time.Now().UTC()
	duration := now.Sub(lastAlive)

	// Return duration in seconds
	return int(duration.Seconds())
}

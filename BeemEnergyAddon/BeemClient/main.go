package main

import (
	"encoding/json"
	"log/slog"
	"os"
	"time"
)

const (
	baseURL         = "https://api-x.beem.energy/beemapp"
	loginEndpoint   = "/user/login"
	devicesEndpoint = "/devices"
	summaryEndpoint = "/box/summary"

	// MQTT configuration
	mqttBaseTopic       = "homeassistant"
	mqttDiscoveryPrefix = "homeassistant"
	mqttDeviceClass     = "energy"
	mqttStateClass      = "measurement"
)

// Config stores addon config information
type Config struct {
	BeemEmail    string `json:"beem_email"`
	BeemPassword string `json:"beem_password"`

	StartDelay      int  `json:"start_delayseconds"`
	Debug           bool `json:"debug"`
	RefreshInterval int  `json:"refresh_interval"`

	MQTTHost     string `json:"override_mqtt_server"`
	MQTTPort     int    `json:"override_mqtt_port"`
	MQTTUsername string `json:"override_mqtt_username"`
	MQTTPassword string `json:"override_mqtt_password"`

	Token string
}

func main() {

	logLevel := &slog.LevelVar{} // INFO

	opts := &slog.HandlerOptions{
		Level: logLevel,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, opts))
	slog.SetDefault(logger)

	slog.Info("=============   Beem Energy Addon - starting Application   =============")

	// Global config
	var config Config

	err := getMQTTInfo(&config)
	if err != nil {
		slog.Error("error retrieving MQTT info", "error", err)
	}

	// Load config from /data/options.json if it exists
	configFile := "/data/options.json"

	if _, err := os.Stat(configFile); err == nil {
		file, err := os.Open(configFile)
		if err != nil {
			slog.Error("failed to open config file", "error", err)
			os.Exit(1)
		}

		if err := json.NewDecoder(file).Decode(&config); err != nil {
			slog.Error("Failed to decode config file", "error", err)
		}
		if err := file.Close(); err != nil {
			slog.Error("failed to close config file", "error", err)
		}
	}

	if config.Debug {
		logLevel.Set(slog.LevelDebug)
		slog.Debug("debug mode is enabled")

		anonConfig := Config{
			StartDelay:      config.StartDelay,
			Debug:           config.Debug,
			RefreshInterval: config.RefreshInterval,
			MQTTHost:        config.MQTTHost,
			MQTTPort:        config.MQTTPort,
			MQTTUsername:    config.MQTTUsername,
			MQTTPassword:    config.MQTTPassword,
			Token:           config.Token,
			BeemPassword:    "xxxxxx", // Masked for debug output
		}
		// Anonymize sensitive information
		masked := ""
		for _, c := range config.BeemEmail {
			if c == '@' || c == '.' {
				masked += string(c)
			} else {
				masked += "x"
			}
		}
		anonConfig.BeemEmail = masked
		slog.Debug("config file", "content", anonConfig)
	}

	// Wait for StartDelay seconds if specified
	if config.StartDelay > 0 {
		slog.Info("waiting before starting", "delay", config.StartDelay)
		time.Sleep(time.Duration(config.StartDelay) * time.Second)
	}

	// Connect to MQTT broker
	setupMQTTClient(config)

	// Start the ticker to run every minute
	ticker := time.NewTicker(time.Duration(config.RefreshInterval) * time.Minute)
	defer ticker.Stop()

	// Run once immediately before waiting for the ticker
	fetchBeemData(&config)

	for range ticker.C {
		fetchBeemData(&config)
	}
}

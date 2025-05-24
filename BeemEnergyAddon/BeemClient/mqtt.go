package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"log/slog"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MQTT client connection
var mqttClient mqtt.Client

// MQTT Configuration structs
type MQTTDeviceInfo struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
	SwVersion    string   `json:"sw_version"`
}

type MQTTDiscoveryConfig struct {
	Name              string         `json:"name"`
	UniqueId          string         `json:"unique_id"`
	Device            MQTTDeviceInfo `json:"device"`
	StateTopic        string         `json:"state_topic"`
	UnitOfMeasurement string         `json:"unit_of_measurement,omitempty"`
	DeviceClass       string         `json:"device_class,omitempty"`
	StateClass        string         `json:"state_class,omitempty"`
	Icon              string         `json:"icon,omitempty"`
	EntityCategory    string         `json:"entity_category,omitempty"`
}

type MQTTData struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Protocol string `json:"protocol"`
}

type MQTTResponse struct {
	Result string   `json:"result"`
	Data   MQTTData `json:"data"`
}

func getMQTTInfo(config *Config) error {
	supervisorToken := os.Getenv("SUPERVISOR_TOKEN")
	if supervisorToken == "" {
		return fmt.Errorf("SUPERVISOR_TOKEN not set")
	}

	client := &http.Client{}
	req, err := http.NewRequest("GET", "http://supervisor/services/mqtt", nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+supervisorToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("failed to close response body", "error", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected response %d: %s", resp.StatusCode, string(body))
	}

	var mqttResp MQTTResponse
	if err := json.NewDecoder(resp.Body).Decode(&mqttResp); err != nil {
		return err
	}

	if mqttResp.Result != "ok" {
		return fmt.Errorf("supervisor API returned result: %s", mqttResp.Result)
	}

	slog.Debug("MQTT Response", "host", mqttResp.Data.Host, "port", mqttResp.Data.Port, "username", mqttResp.Data.Username)

	config.MQTTHost = mqttResp.Data.Host
	config.MQTTPort = mqttResp.Data.Port
	config.MQTTUsername = mqttResp.Data.Username
	config.MQTTPassword = mqttResp.Data.Password

	return nil
}

// Set up MQTT client
func setupMQTTClient(config Config) {
	// Define MQTT broker options
	opts := mqtt.NewClientOptions()
	brokerURL := fmt.Sprintf("tcp://%s:%d", config.MQTTHost, config.MQTTPort)
	opts.AddBroker(brokerURL)
	opts.SetClientID("beem-energy-client")

	slog.Info("Connecting to MQTT broker", "url", brokerURL)

	// Set credentials if provided
	if config.MQTTUsername != "" {
		opts.SetUsername(config.MQTTUsername)
		opts.SetPassword(config.MQTTPassword)
	}

	// Set callbacks
	opts.SetOnConnectHandler(func(client mqtt.Client) {
		slog.Info("Connected to MQTT broker", "url", brokerURL)
	})

	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		slog.Error("connection to MQTT broker lost", "error", err)
	})

	// Create and connect the client
	mqttClient = mqtt.NewClient(opts)
	if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
		slog.Error("failed to connect to MQTT broker", "error", token.Error())
	}
}

// Publish box data to MQTT with auto-discovery
func publishBoxToMQTT(box BoxData, deviceId, deviceName string) {
	if !mqttClient.IsConnected() {
		slog.Error("MQTT client not connected. Cannot publish data.")
		return
	}

	// Create the base device information
	deviceInfo := MQTTDeviceInfo{
		Identifiers:  []string{deviceId},
		Name:         deviceName,
		Manufacturer: "Beem Energy",
		Model:        "Solar Panel",
		SwVersion:    "1.0",
	}

	// Configure and publish the sensors
	publishSensor(box, deviceInfo, deviceId, "power", "Current Power", "W", box.WattHour, "power", "measurement", "mdi:solar-power", "")
	publishSensor(box, deviceInfo, deviceId, "energy_daily", "Daily Energy", "Wh", box.TotalDay, "energy", "total_increasing", "mdi:solar-power", "")
	publishSensor(box, deviceInfo, deviceId, "energy_month", "Monthly Energy", "Wh", box.TotalMonth, "energy", "total_increasing", "mdi:solar-power", "")
	publishSensor(box, deviceInfo, deviceId, "signal_strength", "Signal Strength", "dBm", box.LastDbm, "signal_strength", "measurement", "mdi:wifi", "diagnostic")

	// Calculate and publish duration since last alive
	lastAlive := calculateLastContactDuration(box.LastAlive)
	publishSensor(box, deviceInfo, deviceId, "last_alive", "Last Alive", "s", lastAlive, "duration", "measurement", "mdi:clock-outline", "diagnostic")

	// Calculate and publish duration since last production
	lastProduction := calculateLastContactDuration(box.LastProduction)
	publishSensor(box, deviceInfo, deviceId, "last_production", "Last Production", "s", lastProduction, "duration", "measurement", "mdi:solar-panel", "diagnostic")
}

// Publish an individual sensor to MQTT
func publishSensor(box BoxData, deviceInfo MQTTDeviceInfo, deviceId, sensorType, name, unit string, value interface{}, deviceClass, stateClass, icon, entityCategory string) {
	// Create unique IDs and topics
	uniqueId := fmt.Sprintf("%s_%s", deviceId, sensorType)
	discoveryTopic := fmt.Sprintf("%s/sensor/%s/%s/config", mqttDiscoveryPrefix, deviceId, sensorType)
	stateTopic := fmt.Sprintf("%s/sensor/%s/%s/state", mqttBaseTopic, deviceId, sensorType)

	// Create the discovery config
	config := MQTTDiscoveryConfig{
		Name:              name,
		UniqueId:          uniqueId,
		Device:            deviceInfo,
		StateTopic:        stateTopic,
		UnitOfMeasurement: unit,
		DeviceClass:       deviceClass,
		StateClass:        stateClass,
		Icon:              icon,
		EntityCategory:    entityCategory,
	}

	// Convert config to JSON
	configJson, err := json.Marshal(config)
	if err != nil {
		slog.Error("failed to marshal discovery config", "error", err)
		return
	}

	// Publish the discovery config
	if token := mqttClient.Publish(discoveryTopic, 0, true, configJson); token.Wait() && token.Error() != nil {
		slog.Error("failed to publish discovery config", "error", token.Error())
		return
	}

	// Convert state value to string
	var stateValue string
	switch v := value.(type) {
	case int:
		stateValue = fmt.Sprintf("%d", v)
	case float64:
		stateValue = fmt.Sprintf("%.2f", v)
	case string:
		stateValue = v
	default:
		stateValue = fmt.Sprintf("%v", v)
	}

	// Publish the state
	if token := mqttClient.Publish(stateTopic, 0, true, stateValue); token.Wait() && token.Error() != nil {
		slog.Error("failed to publish state", "error", token.Error())
		return
	}

	slog.Debug("published", name, stateValue, "unit", unit)
}

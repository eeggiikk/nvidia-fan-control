package main

import (
	"os"
	"path/filepath"
	"testing"
)

func standardRanges() []TemperatureRange {
	return []TemperatureRange{
		{MinTemperature: 0, MaxTemperature: 40, FanSpeed: 30, Hysteresis: 3},
		{MinTemperature: 40, MaxTemperature: 60, FanSpeed: 40, Hysteresis: 3},
		{MinTemperature: 60, MaxTemperature: 80, FanSpeed: 70, Hysteresis: 3},
		{MinTemperature: 80, MaxTemperature: 100, FanSpeed: 100, Hysteresis: 3},
	}
}

func TestGetFanSpeedForTemperature_BasicRangeMatching(t *testing.T) {
	ranges := standardRanges()

	tests := []struct {
		name      string
		temp      int
		prevSpeed int
		expected  int
	}{
		{"Temp 30 in first range", 30, 30, 30},
		{"Temp 50 in second range", 50, 40, 40},
		{"Temp 70 in third range", 70, 70, 70},
		{"Temp 90 in fourth range", 90, 100, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFanSpeedForTemperature(tt.temp, tt.prevSpeed, ranges)
			if result != tt.expected {
				t.Errorf("getFanSpeedForTemperature(%d, %d) = %d, want %d", tt.temp, tt.prevSpeed, result, tt.expected)
			}
		})
	}
}

func TestGetFanSpeedForTemperature_BoundaryConditions(t *testing.T) {
	ranges := standardRanges()

	tests := []struct {
		name      string
		temp      int
		prevSpeed int
		expected  int
	}{
		{"Temp exactly 40 (boundary)", 40, 30, 30},
		{"Temp exactly 60 (boundary)", 60, 40, 40},
		{"Temp exactly 80 (boundary)", 80, 70, 70},
		{"Temp exactly 100 (boundary)", 100, 100, 100},
		{"Temp exactly 0 (lower boundary)", 0, 30, 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFanSpeedForTemperature(tt.temp, tt.prevSpeed, ranges)
			if result != tt.expected {
				t.Errorf("getFanSpeedForTemperature(%d, %d) = %d, want %d", tt.temp, tt.prevSpeed, result, tt.expected)
			}
		})
	}
}

func TestGetFanSpeedForTemperature_HysteresisCooling(t *testing.T) {
	ranges := standardRanges()

	tests := []struct {
		name      string
		temp      int
		prevSpeed int
		expected  int
	}{
		{"Cooling blocked: 39C with speed 40%", 39, 40, 40},
		{"Cooling blocked: 38C with speed 40%", 38, 40, 40},
		{"Cooling allowed: 37C with speed 40%", 37, 40, 30},
		{"Cooling allowed: 36C with speed 40%", 36, 40, 30},
		{"Cooling blocked: 59C with speed 70%", 59, 70, 70},
		{"Cooling allowed: 57C with speed 70%", 57, 70, 40},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFanSpeedForTemperature(tt.temp, tt.prevSpeed, ranges)
			if result != tt.expected {
				t.Errorf("getFanSpeedForTemperature(%d, %d) = %d, want %d", tt.temp, tt.prevSpeed, result, tt.expected)
			}
		})
	}
}

func TestGetFanSpeedForTemperature_HysteresisHeating(t *testing.T) {
	ranges := standardRanges()

	tests := []struct {
		name      string
		temp      int
		prevSpeed int
		expected  int
	}{
		{"Heating blocked: 41C with speed 30%", 41, 30, 30},
		{"Heating blocked: 42C with speed 30%", 42, 30, 30},
		{"Heating blocked: 43C with speed 30%", 43, 30, 30},
		{"Heating allowed: 44C with speed 30%", 44, 30, 40},
		{"Heating allowed: 50C with speed 30%", 50, 30, 40},
		{"Heating blocked: 61C with speed 40%", 61, 40, 40},
		{"Heating allowed: 64C with speed 40%", 64, 40, 70},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFanSpeedForTemperature(tt.temp, tt.prevSpeed, ranges)
			if result != tt.expected {
				t.Errorf("getFanSpeedForTemperature(%d, %d) = %d, want %d", tt.temp, tt.prevSpeed, result, tt.expected)
			}
		})
	}
}

func TestGetFanSpeedForTemperature_OscillationPrevention(t *testing.T) {
	ranges := standardRanges()

	initialSpeed := 40
	temperatures := []int{41, 39, 41, 39, 41, 39}
	expectedSpeeds := []int{40, 40, 40, 40, 40, 40}

	currentSpeed := initialSpeed
	for i, temp := range temperatures {
		newSpeed := getFanSpeedForTemperature(temp, currentSpeed, ranges)
		if newSpeed != expectedSpeeds[i] {
			t.Errorf("Oscillation test step %d: temp=%d, prevSpeed=%d, got=%d, want=%d",
				i, temp, currentSpeed, newSpeed, expectedSpeeds[i])
		}
		currentSpeed = newSpeed
	}
}

func TestGetFanSpeedForTemperature_EmptyRanges(t *testing.T) {
	result := getFanSpeedForTemperature(50, 40, []TemperatureRange{})
	if result != 40 {
		t.Errorf("Empty ranges: got %d, want 40 (prevSpeed)", result)
	}

	result = getFanSpeedForTemperature(50, 40, nil)
	if result != 40 {
		t.Errorf("Nil ranges: got %d, want 40 (prevSpeed)", result)
	}
}

func TestGetFanSpeedForTemperature_UnknownPrevSpeed(t *testing.T) {
	ranges := standardRanges()

	tests := []struct {
		name      string
		temp      int
		prevSpeed int
		expected  int
	}{
		{"Unknown speed 50%, temp in range 2", 50, 50, 40},
		{"Unknown speed 99%, temp in range 1", 30, 99, 30},
		{"Unknown speed 0%, temp in range 3", 70, 0, 70},
		{"Unknown speed 15%, temp in range 4", 90, 15, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFanSpeedForTemperature(tt.temp, tt.prevSpeed, ranges)
			if result != tt.expected {
				t.Errorf("getFanSpeedForTemperature(%d, %d) = %d, want %d", tt.temp, tt.prevSpeed, result, tt.expected)
			}
		})
	}
}

func TestGetFanSpeedForTemperature_TempOutOfRange(t *testing.T) {
	ranges := standardRanges()

	tests := []struct {
		name      string
		temp      int
		prevSpeed int
		expected  int
	}{
		{"Temp below all ranges: -10C", -10, 40, 40},
		{"Temp above all ranges: 150C", 150, 40, 40},
		{"Temp way above: 250C", 250, 70, 70},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFanSpeedForTemperature(tt.temp, tt.prevSpeed, ranges)
			if result != tt.expected {
				t.Errorf("getFanSpeedForTemperature(%d, %d) = %d, want %d", tt.temp, tt.prevSpeed, result, tt.expected)
			}
		})
	}
}

func TestGetFanSpeedForTemperature_MultiRangeJump(t *testing.T) {
	ranges := standardRanges()

	tests := []struct {
		name      string
		temp      int
		prevSpeed int
		expected  int
	}{
		{"Jump up: 30C->90C from 30%", 90, 30, 100},
		{"Jump up: 30C->70C from 30%", 70, 30, 70},
		{"Jump down: 90C->30C from 100%", 30, 100, 30},
		{"Jump down: 90C->50C from 100%", 50, 100, 40},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFanSpeedForTemperature(tt.temp, tt.prevSpeed, ranges)
			if result != tt.expected {
				t.Errorf("getFanSpeedForTemperature(%d, %d) = %d, want %d", tt.temp, tt.prevSpeed, result, tt.expected)
			}
		})
	}
}

func TestGetFanSpeedForTemperature_ZeroHysteresis(t *testing.T) {
	ranges := []TemperatureRange{
		{MinTemperature: 0, MaxTemperature: 50, FanSpeed: 30, Hysteresis: 0},
		{MinTemperature: 50, MaxTemperature: 100, FanSpeed: 70, Hysteresis: 0},
	}

	tests := []struct {
		name      string
		temp      int
		prevSpeed int
		expected  int
	}{
		{"Cross boundary up immediately", 51, 30, 70},
		{"Cross boundary down immediately", 50, 70, 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFanSpeedForTemperature(tt.temp, tt.prevSpeed, ranges)
			if result != tt.expected {
				t.Errorf("getFanSpeedForTemperature(%d, %d) = %d, want %d", tt.temp, tt.prevSpeed, result, tt.expected)
			}
		})
	}
}

func TestGetFanSpeedForTemperature_DuplicateFanSpeed(t *testing.T) {
	ranges := []TemperatureRange{
		{MinTemperature: 0, MaxTemperature: 40, FanSpeed: 30, Hysteresis: 3},
		{MinTemperature: 40, MaxTemperature: 60, FanSpeed: 30, Hysteresis: 5},
		{MinTemperature: 60, MaxTemperature: 100, FanSpeed: 70, Hysteresis: 3},
	}

	tests := []struct {
		name      string
		temp      int
		prevSpeed int
		expected  int
	}{
		{"Correct hysteresis blocks: 63C stays at 30%", 63, 30, 30},
		{"Correct hysteresis allows: 66C transitions to 70%", 66, 30, 70},
		{"Temp in first duplicate range stays", 35, 30, 30},
		{"Temp in second duplicate range stays", 50, 30, 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getFanSpeedForTemperature(tt.temp, tt.prevSpeed, ranges)
			if result != tt.expected {
				t.Errorf("getFanSpeedForTemperature(%d, %d) = %d, want %d", tt.temp, tt.prevSpeed, result, tt.expected)
			}
		})
	}
}

func TestLoadConfig_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	content := `{
		"time_to_update": 10,
		"temperature_ranges": [
			{"min_temperature": 0, "max_temperature": 50, "fan_speed": 30, "hysteresis": 5}
		]
	}`

	err := os.WriteFile(configPath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	if config.TimeToUpdate != 10 {
		t.Errorf("TimeToUpdate = %d, want 10", config.TimeToUpdate)
	}

	if len(config.TemperatureRanges) != 1 {
		t.Fatalf("TemperatureRanges length = %d, want 1", len(config.TemperatureRanges))
	}

	r := config.TemperatureRanges[0]
	if r.MinTemperature != 0 || r.MaxTemperature != 50 || r.FanSpeed != 30 || r.Hysteresis != 5 {
		t.Errorf("Range = %+v, want {0, 50, 30, 5}", r)
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := loadConfig("/nonexistent/path/config.json")
	if err == nil {
		t.Error("Expected error for missing file, got nil")
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	err := os.WriteFile(configPath, []byte("{ invalid json }"), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err = loadConfig(configPath)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestLoadConfig_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	err := os.WriteFile(configPath, []byte(""), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err = loadConfig(configPath)
	if err == nil {
		t.Error("Expected error for empty file, got nil")
	}
}

func TestLoadConfig_EmptyJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	err := os.WriteFile(configPath, []byte("{}"), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	if config.TimeToUpdate != 0 {
		t.Errorf("TimeToUpdate = %d, want 0", config.TimeToUpdate)
	}

	if len(config.TemperatureRanges) != 0 {
		t.Errorf("TemperatureRanges length = %d, want 0", len(config.TemperatureRanges))
	}
}

func TestLoadConfig_MultipleRanges(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	content := `{
		"time_to_update": 5,
		"temperature_ranges": [
			{"min_temperature": 0, "max_temperature": 40, "fan_speed": 30, "hysteresis": 3},
			{"min_temperature": 40, "max_temperature": 60, "fan_speed": 50, "hysteresis": 3},
			{"min_temperature": 60, "max_temperature": 100, "fan_speed": 100, "hysteresis": 3}
		]
	}`

	err := os.WriteFile(configPath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	config, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	if len(config.TemperatureRanges) != 3 {
		t.Errorf("TemperatureRanges length = %d, want 3", len(config.TemperatureRanges))
	}
}

func createTempConfig(t *testing.T, content string) string {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	err := os.WriteFile(configPath, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}
	return configPath
}

func TestLoadConfiguration_ValidConfig(t *testing.T) {
	oldOutput := os.Stdout
	os.Stdout = nil
	defer func() { os.Stdout = oldOutput }()

	content := `{
		"time_to_update": 10,
		"temperature_ranges": [
			{"min_temperature": 0, "max_temperature": 100, "fan_speed": 50, "hysteresis": 3}
		]
	}`
	configPath := createTempConfig(t, content)

	config, err := loadConfiguration(configPath)
	if err != nil {
		t.Fatalf("loadConfiguration failed: %v", err)
	}

	if config.TimeToUpdate != 10 {
		t.Errorf("TimeToUpdate = %d, want 10", config.TimeToUpdate)
	}
}

func TestLoadConfiguration_ZeroTimeToUpdate(t *testing.T) {
	content := `{
		"time_to_update": 0,
		"temperature_ranges": [
			{"min_temperature": 0, "max_temperature": 100, "fan_speed": 50, "hysteresis": 3}
		]
	}`
	configPath := createTempConfig(t, content)

	config, err := loadConfiguration(configPath)
	if err != nil {
		t.Fatalf("loadConfiguration failed: %v", err)
	}

	if config.TimeToUpdate != 5 {
		t.Errorf("TimeToUpdate = %d, want 5 (default)", config.TimeToUpdate)
	}
}

func TestLoadConfiguration_NegativeTimeToUpdate(t *testing.T) {
	content := `{
		"time_to_update": -10,
		"temperature_ranges": [
			{"min_temperature": 0, "max_temperature": 100, "fan_speed": 50, "hysteresis": 3}
		]
	}`
	configPath := createTempConfig(t, content)

	config, err := loadConfiguration(configPath)
	if err != nil {
		t.Fatalf("loadConfiguration failed: %v", err)
	}

	if config.TimeToUpdate != 5 {
		t.Errorf("TimeToUpdate = %d, want 5 (default)", config.TimeToUpdate)
	}
}

func TestLoadConfiguration_MissingFile(t *testing.T) {
	_, err := loadConfiguration("/nonexistent/config.json")
	if err == nil {
		t.Error("Expected error for missing file, got nil")
	}
}

func TestLoadConfiguration_CriticalTempSet(t *testing.T) {
	content := `{
		"time_to_update": 5,
		"temperature_ranges": [
			{"min_temperature": 0, "max_temperature": 100, "fan_speed": 50, "hysteresis": 3}
		],
		"critical_temp": 95
	}`
	configPath := createTempConfig(t, content)

	config, err := loadConfiguration(configPath)
	if err != nil {
		t.Fatalf("loadConfiguration failed: %v", err)
	}

	if config.CriticalTemp != 95 {
		t.Errorf("CriticalTemp = %d, want 95", config.CriticalTemp)
	}
}

func TestLoadConfiguration_CriticalTempDefault(t *testing.T) {
	content := `{
		"time_to_update": 5,
		"temperature_ranges": [
			{"min_temperature": 0, "max_temperature": 100, "fan_speed": 50, "hysteresis": 3}
		]
	}`
	configPath := createTempConfig(t, content)

	config, err := loadConfiguration(configPath)
	if err != nil {
		t.Fatalf("loadConfiguration failed: %v", err)
	}

	if config.CriticalTemp != 105 {
		t.Errorf("CriticalTemp = %d, want 105 (default)", config.CriticalTemp)
	}
}

func TestLoadConfiguration_CriticalTempZero(t *testing.T) {
	content := `{
		"time_to_update": 5,
		"temperature_ranges": [
			{"min_temperature": 0, "max_temperature": 100, "fan_speed": 50, "hysteresis": 3}
		],
		"critical_temp": 0
	}`
	configPath := createTempConfig(t, content)

	config, err := loadConfiguration(configPath)
	if err != nil {
		t.Fatalf("loadConfiguration failed: %v", err)
	}

	if config.CriticalTemp != 105 {
		t.Errorf("CriticalTemp = %d, want 105 (default for zero)", config.CriticalTemp)
	}
}

func TestLoadConfiguration_CriticalTempNegative(t *testing.T) {
	content := `{
		"time_to_update": 5,
		"temperature_ranges": [
			{"min_temperature": 0, "max_temperature": 100, "fan_speed": 50, "hysteresis": 3}
		],
		"critical_temp": -50
	}`
	configPath := createTempConfig(t, content)

	config, err := loadConfiguration(configPath)
	if err != nil {
		t.Fatalf("loadConfiguration failed: %v", err)
	}

	if config.CriticalTemp != 105 {
		t.Errorf("CriticalTemp = %d, want 105 (default for negative)", config.CriticalTemp)
	}
}

func TestLoadConfiguration_EmptyRanges(t *testing.T) {
	content := `{
		"time_to_update": 5,
		"temperature_ranges": []
	}`
	configPath := createTempConfig(t, content)

	_, err := loadConfiguration(configPath)
	if err == nil {
		t.Error("Expected error for empty temperature_ranges, got nil")
	}
}

func TestLoadConfiguration_InvertedRange(t *testing.T) {
	content := `{
		"time_to_update": 5,
		"temperature_ranges": [
			{"min_temperature": 80, "max_temperature": 40, "fan_speed": 50, "hysteresis": 3}
		]
	}`
	configPath := createTempConfig(t, content)

	_, err := loadConfiguration(configPath)
	if err == nil {
		t.Error("Expected error for inverted range, got nil")
	}
}

func TestLoadConfiguration_FanSpeedAbove100(t *testing.T) {
	content := `{
		"time_to_update": 5,
		"temperature_ranges": [
			{"min_temperature": 0, "max_temperature": 100, "fan_speed": 150, "hysteresis": 3}
		]
	}`
	configPath := createTempConfig(t, content)

	_, err := loadConfiguration(configPath)
	if err == nil {
		t.Error("Expected error for fan_speed > 100, got nil")
	}
}

func TestLoadConfiguration_NegativeFanSpeed(t *testing.T) {
	content := `{
		"time_to_update": 5,
		"temperature_ranges": [
			{"min_temperature": 0, "max_temperature": 100, "fan_speed": -10, "hysteresis": 3}
		]
	}`
	configPath := createTempConfig(t, content)

	_, err := loadConfiguration(configPath)
	if err == nil {
		t.Error("Expected error for negative fan_speed, got nil")
	}
}

func TestLoadConfiguration_NegativeHysteresis(t *testing.T) {
	content := `{
		"time_to_update": 5,
		"temperature_ranges": [
			{"min_temperature": 0, "max_temperature": 100, "fan_speed": 50, "hysteresis": -5}
		]
	}`
	configPath := createTempConfig(t, content)

	_, err := loadConfiguration(configPath)
	if err == nil {
		t.Error("Expected error for negative hysteresis, got nil")
	}
}

func TestLoadConfiguration_NonContiguousRanges(t *testing.T) {
	content := `{
		"time_to_update": 5,
		"temperature_ranges": [
			{"min_temperature": 0, "max_temperature": 40, "fan_speed": 30, "hysteresis": 3},
			{"min_temperature": 50, "max_temperature": 100, "fan_speed": 70, "hysteresis": 3}
		]
	}`
	configPath := createTempConfig(t, content)

	_, err := loadConfiguration(configPath)
	if err == nil {
		t.Error("Expected error for non-contiguous ranges, got nil")
	}
}

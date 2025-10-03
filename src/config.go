package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	Source           string
	Destination      string
	Keep             int
	CleanupAtPercent int
	ExcludeList      string
	LogFile          string
	LockFile         string
	DryRun           bool
	ForceSystemRsync bool
	ShowProgress     bool
	RsyncBin         string
}

type ConfigFile struct {
	Source           string `json:"source"`
	Destination      string `json:"destination"`
	Keep             int    `json:"keep"`
	CleanupAtPercent int    `json:"cleanup_at_percent"`
	ExcludeList      string `json:"exclude_list"`
	LogFile          string `json:"log_file"`
	LockFile         string `json:"lock_file"`
	DryRun           bool   `json:"dry_run"`
	ForceSystemRsync bool   `json:"force_system_rsync"`
	ShowProgress     bool   `json:"show_progress"`
}

func LoadConfig(filename string) (Config, error) {
	config := DefaultConfig

	// Try to load from file
	if filename != "" {
		if data, err := os.ReadFile(filename); err == nil {
			var configFile ConfigFile
			if err := json.Unmarshal(data, &configFile); err == nil {
				config.Source = configFile.Source
				config.Destination = configFile.Destination
				config.Keep = configFile.Keep
				config.CleanupAtPercent = configFile.CleanupAtPercent
				config.ExcludeList = configFile.ExcludeList
				config.LockFile = configFile.LockFile
				config.LogFile = configFile.LogFile
				config.DryRun = configFile.DryRun
				config.ForceSystemRsync = configFile.ForceSystemRsync
				config.ShowProgress = configFile.ShowProgress
			}
		}
	}

	// Basic validation
	if config.Source == "" || config.Destination == "" {
		return config, fmt.Errorf("source and destination paths are required")
	}
	if config.Keep < 1 {
		config.Keep = 7 // Set reasonable default
	}
	if config.CleanupAtPercent < 50 || config.CleanupAtPercent > 95 {
		config.CleanupAtPercent = 90 // Set reasonable default
	}

	return config, nil
}

func SaveConfig(config Config, filename string) error {
	configFile := ConfigFile{
		Source:           config.Source,
		Destination:      config.Destination,
		Keep:             config.Keep,
		CleanupAtPercent: config.CleanupAtPercent,
		ExcludeList:      config.ExcludeList,
		LockFile:         config.LockFile,
		LogFile:          config.LogFile,
		DryRun:           config.DryRun,
		ForceSystemRsync: config.ForceSystemRsync,
	}

	data, err := json.MarshalIndent(configFile, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}

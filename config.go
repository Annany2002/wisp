package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// LocationConfig holds directives for a 'location' block.
type LocationConfig struct {
	Path      string
	Root      string
	ProxyPass string
}

// ServerConfig holds directives for a 'server' block.
type ServerConfig struct {
	Listen     int
	ServerName string
	Locations  []LocationConfig
}

// ParseConfig reads and parses a Wisp configuration file.
func ParseConfig(filePath string) (*ServerConfig, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open config file: %w", err)
	}
	defer file.Close()

	var config ServerConfig
	var currentLoc *LocationConfig
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		directive := parts[0]

		if directive == "location" && len(parts) == 3 && parts[2] == "{" {
			loc := LocationConfig{Path: parts[1]}
			config.Locations = append(config.Locations, loc)
			currentLoc = &config.Locations[len(config.Locations)-1] // Get a pointer to the new location
			continue
		}

		// Check for closing brace '}'
		if directive == "}" {
			currentLoc = nil // Exit location block context
			continue
		}

		// Handle directives inside a location block
		if currentLoc != nil && len(parts) >= 2 {
			value := strings.Trim(parts[1], ";")
			switch directive {
			case "root":
				currentLoc.Root = value
			case "proxy_pass":
				currentLoc.ProxyPass = value
			}
			continue
		}

		// Handle server-level directives
		if len(parts) >= 2 {
			value := strings.Trim(parts[1], ";")
			switch directive {
			case "listen":
				config.Listen, _ = strconv.Atoi(value)
			case "server_name":
				config.ServerName = value
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	return &config, nil
}

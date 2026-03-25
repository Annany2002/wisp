package config

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
	Listen            int
	ServerName        string
	Locations         []LocationConfig
	SSLCertificate    string // the certificate file
	SSLCertificateKey string // the key file
}

// Config is the top-level configuration containing all server blocks.
type Config struct {
	Servers []ServerConfig
}

// Parse reads and parses a Wisp configuration file.
// It returns a Config containing all parsed server blocks.
func Parse(filePath string) (*Config, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open config file: %w", err)
	}
	defer file.Close()

	var cfg Config
	var currentServer *ServerConfig
	var currentLoc *LocationConfig
	depth := 0 // Track brace nesting depth.
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

		// Handle 'server {' — start a new server block.
		if directive == "server" && len(parts) == 2 && parts[1] == "{" {
			cfg.Servers = append(cfg.Servers, ServerConfig{})
			currentServer = &cfg.Servers[len(cfg.Servers)-1]
			currentLoc = nil
			depth = 1
			continue
		}

		// Handle 'location <path> {' — start a location block within a server.
		if directive == "location" && len(parts) == 3 && parts[2] == "{" {
			if currentServer == nil {
				return nil, fmt.Errorf("location block outside of server block")
			}
			loc := LocationConfig{Path: parts[1]}
			currentServer.Locations = append(currentServer.Locations, loc)
			currentLoc = &currentServer.Locations[len(currentServer.Locations)-1]
			depth = 2
			continue
		}

		// Handle closing brace '}'.
		if directive == "}" {
			if depth == 2 {
				// Closing a location block.
				currentLoc = nil
				depth = 1
			} else if depth == 1 {
				// Closing a server block.
				currentServer = nil
				currentLoc = nil
				depth = 0
			}
			continue
		}

		if len(parts) < 2 {
			continue
		}

		value := strings.Trim(parts[1], ";")

		// Handle directives inside a location block.
		if currentLoc != nil {
			switch directive {
			case "root":
				currentLoc.Root = value
			case "proxy_pass":
				currentLoc.ProxyPass = value
			}
			continue
		}

		// Handle server-level directives.
		if currentServer != nil {
			switch directive {
			case "listen":
				currentServer.Listen, _ = strconv.Atoi(value)
			case "server_name":
				currentServer.ServerName = value
			case "ssl_certificate":
				currentServer.SSLCertificate = value
			case "ssl_certificate_key":
				currentServer.SSLCertificateKey = value
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	if len(cfg.Servers) == 0 {
		return nil, fmt.Errorf("no server blocks found in config")
	}

	return &cfg, nil
}

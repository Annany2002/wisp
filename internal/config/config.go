package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// UpstreamBackend represents a single backend server in an upstream group.
type UpstreamBackend struct {
	Address string
	Weight  int
}

// UpstreamConfig holds directives for an 'upstream' block.
type UpstreamConfig struct {
	Name     string
	Method   string // "round_robin" (default) or "least_conn"
	Backends []UpstreamBackend
}

// ProxyHeader is a single proxy_set_header directive (preserves order).
type ProxyHeader struct {
	Name  string
	Value string
}

// LocationConfig holds directives for a 'location' block.
type LocationConfig struct {
	Path             string
	Root             string
	ProxyPass        string
	ProxySetHeaders  []ProxyHeader
}

// ServerConfig holds directives for a 'server' block.
type ServerConfig struct {
	Listen            int
	ServerName        string
	Locations         []LocationConfig
	SSLCertificate    string // the certificate file
	SSLCertificateKey string // the key file
}

// Config is the top-level configuration containing all server and upstream blocks.
type Config struct {
	Servers   []ServerConfig
	Upstreams []UpstreamConfig
}

// Parse reads and parses a Wisp configuration file.
func Parse(filePath string) (*Config, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open config file: %w", err)
	}
	defer file.Close()

	var cfg Config
	var currentServer *ServerConfig
	var currentLoc *LocationConfig
	var currentUpstream *UpstreamConfig

	// blockType tracks what top-level block we're in: "server", "upstream", or "".
	blockType := ""
	depth := 0
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		directive := parts[0]

		// Handle 'upstream <name> {'.
		if directive == "upstream" && len(parts) == 3 && parts[2] == "{" {
			if depth != 0 {
				return nil, fmt.Errorf("upstream block cannot be nested")
			}
			cfg.Upstreams = append(cfg.Upstreams, UpstreamConfig{
				Name:   parts[1],
				Method: "round_robin",
			})
			currentUpstream = &cfg.Upstreams[len(cfg.Upstreams)-1]
			blockType = "upstream"
			depth = 1
			continue
		}

		// Handle 'server {' at top level.
		if directive == "server" && len(parts) == 2 && parts[1] == "{" && depth == 0 {
			cfg.Servers = append(cfg.Servers, ServerConfig{})
			currentServer = &cfg.Servers[len(cfg.Servers)-1]
			currentLoc = nil
			blockType = "server"
			depth = 1
			continue
		}

		// Handle 'location <path> {'.
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
				currentLoc = nil
				depth = 1
			} else if depth == 1 {
				currentServer = nil
				currentLoc = nil
				currentUpstream = nil
				blockType = ""
				depth = 0
			}
			continue
		}

		if len(parts) < 2 {
			continue
		}

		value := strings.Trim(parts[1], ";")

		// Handle directives inside an upstream block.
		if blockType == "upstream" && currentUpstream != nil {
			switch directive {
			case "method":
				if value != "round_robin" && value != "least_conn" {
					return nil, fmt.Errorf("unknown upstream method: %s", value)
				}
				currentUpstream.Method = value
			case "server":
				backend := UpstreamBackend{Address: value, Weight: 1}
				// Parse optional weight: server 127.0.0.1:3001 weight=3;
				for _, p := range parts[2:] {
					p = strings.Trim(p, ";")
					if strings.HasPrefix(p, "weight=") {
						w, err := strconv.Atoi(strings.TrimPrefix(p, "weight="))
						if err == nil && w > 0 {
							backend.Weight = w
						}
					}
				}
				currentUpstream.Backends = append(currentUpstream.Backends, backend)
			}
			continue
		}

		// Handle directives inside a location block.
		if currentLoc != nil {
			switch directive {
			case "root":
				currentLoc.Root = value
			case "proxy_pass":
				currentLoc.ProxyPass = value
			case "proxy_set_header":
				// proxy_set_header <Name> <value...>;
				if len(parts) < 3 {
					return nil, fmt.Errorf("proxy_set_header requires name and value")
				}
				rest := strings.TrimSuffix(strings.Join(parts[2:], " "), ";")
				rest = strings.TrimSpace(rest)
				currentLoc.ProxySetHeaders = append(currentLoc.ProxySetHeaders, ProxyHeader{
					Name:  parts[1],
					Value: rest,
				})
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

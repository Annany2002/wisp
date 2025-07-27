// wisp/router.go

package main

import "strings"

// routeRequest finds the best location configuration for a given request.
// It uses a longest prefix matching algorithm.
func RouteRequest(req *Request, config *ServerConfig) *LocationConfig {
	var bestMatch *LocationConfig
	longestMatchLen := 0

	for i, location := range config.Locations {
		// Check if the request URI has the location's path as a prefix.
		if strings.HasPrefix(req.URI, location.Path) {
			// If this match is longer than the previous best match, it's the new best.
			if len(location.Path) > longestMatchLen {
				longestMatchLen = len(location.Path)
				bestMatch = &config.Locations[i] // Get a pointer to the location in the original slice
			}
		}
	}

	return bestMatch
}

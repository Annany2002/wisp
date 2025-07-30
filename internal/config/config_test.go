package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfig(t *testing.T) {
	// A sample config content for testing.
	configContent := `
# Test config for Wisp
server {
    listen      8888;
    server_name test.local;

    location / {
        root /var/www/test;
    }

    location /api/ {
        proxy_pass http://127.0.0.1:9090;
    }
}
`
	// Create a temporary directory and file for our test config.
	// This ensures our test is self-contained and doesn't rely on external files.
	tempDir := t.TempDir()
	tempConfigFile := filepath.Join(tempDir, "wisp.conf")
	if err := os.WriteFile(tempConfigFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write temporary config file: %v", err)
	}

	// Run the parser on our test file.
	config, err := Parse(tempConfigFile)
	if err != nil {
		t.Fatalf("ParseConfig() returned an unexpected error: %v", err)
	}

	// Assert that the parsed values are correct.
	if config.Listen != 8888 {
		t.Errorf("expected Listen to be 8888, got %d", config.Listen)
	}

	if config.ServerName != "test.local" {
		t.Errorf("expected ServerName to be 'test.local', got '%s'", config.ServerName)
	}

	if len(config.Locations) != 2 {
		t.Fatalf("expected 2 locations, got %d", len(config.Locations))
	}

	// Check the first location block.
	if config.Locations[0].Path != "/" || config.Locations[0].Root != "/var/www/test" {
		t.Errorf("unexpected values for first location: %+v", config.Locations[0])
	}

	// Check the second location block.
	if config.Locations[1].Path != "/api/" || config.Locations[1].ProxyPass != "http://127.0.0.1:9090" {
		t.Errorf("unexpected values for second location: %+v", config.Locations[1])
	}
}

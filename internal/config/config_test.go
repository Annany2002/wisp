package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfig(t *testing.T) {
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
	tempDir := t.TempDir()
	tempConfigFile := filepath.Join(tempDir, "wisp.conf")
	if err := os.WriteFile(tempConfigFile, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write temporary config file: %v", err)
	}

	cfg, err := Parse(tempConfigFile)
	if err != nil {
		t.Fatalf("Parse() returned an unexpected error: %v", err)
	}

	if len(cfg.Servers) != 1 {
		t.Fatalf("expected 1 server block, got %d", len(cfg.Servers))
	}

	srv := cfg.Servers[0]

	if srv.Listen != 8888 {
		t.Errorf("expected Listen to be 8888, got %d", srv.Listen)
	}

	if srv.ServerName != "test.local" {
		t.Errorf("expected ServerName to be 'test.local', got '%s'", srv.ServerName)
	}

	if len(srv.Locations) != 2 {
		t.Fatalf("expected 2 locations, got %d", len(srv.Locations))
	}

	if srv.Locations[0].Path != "/" || srv.Locations[0].Root != "/var/www/test" {
		t.Errorf("unexpected values for first location: %+v", srv.Locations[0])
	}

	if srv.Locations[1].Path != "/api/" || srv.Locations[1].ProxyPass != "http://127.0.0.1:9090" {
		t.Errorf("unexpected values for second location: %+v", srv.Locations[1])
	}
}

func TestParseMultipleServerBlocks(t *testing.T) {
	configContent := `
server {
    listen 8080;
    server_name web.local;

    location / {
        root /var/www/web;
    }
}

server {
    listen 8443;
    server_name api.local;

    ssl_certificate     ./cert.pem;
    ssl_certificate_key ./key.pem;

    location /api/ {
        proxy_pass http://127.0.0.1:3000;
    }
}
`
	tempDir := t.TempDir()
	tempConfigFile := filepath.Join(tempDir, "wisp.conf")
	os.WriteFile(tempConfigFile, []byte(configContent), 0644)

	cfg, err := Parse(tempConfigFile)
	if err != nil {
		t.Fatalf("Parse() returned an unexpected error: %v", err)
	}

	if len(cfg.Servers) != 2 {
		t.Fatalf("expected 2 server blocks, got %d", len(cfg.Servers))
	}

	if cfg.Servers[0].Listen != 8080 {
		t.Errorf("expected first server Listen 8080, got %d", cfg.Servers[0].Listen)
	}
	if cfg.Servers[1].Listen != 8443 {
		t.Errorf("expected second server Listen 8443, got %d", cfg.Servers[1].Listen)
	}
	if cfg.Servers[1].SSLCertificate != "./cert.pem" {
		t.Errorf("expected SSL certificate path, got '%s'", cfg.Servers[1].SSLCertificate)
	}
}

func TestParseNoServerBlock(t *testing.T) {
	configContent := `
# Empty config with no server blocks
`
	tempDir := t.TempDir()
	tempConfigFile := filepath.Join(tempDir, "wisp.conf")
	os.WriteFile(tempConfigFile, []byte(configContent), 0644)

	_, err := Parse(tempConfigFile)
	if err == nil {
		t.Error("expected error for config with no server blocks, got nil")
	}
}

func TestParseLocationOutsideServer(t *testing.T) {
	configContent := `
location / {
    root /var/www;
}
`
	tempDir := t.TempDir()
	tempConfigFile := filepath.Join(tempDir, "wisp.conf")
	os.WriteFile(tempConfigFile, []byte(configContent), 0644)

	_, err := Parse(tempConfigFile)
	if err == nil {
		t.Error("expected error for location outside server block, got nil")
	}
}

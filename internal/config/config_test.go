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

func TestParseUpstreamBlock(t *testing.T) {
	configContent := `
upstream backend {
    method round_robin;
    server 127.0.0.1:3001;
    server 127.0.0.1:3002;
    server 127.0.0.1:3003 weight=3;
}

server {
    listen 8080;

    location /api/ {
        proxy_pass http://backend;
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

	if len(cfg.Upstreams) != 1 {
		t.Fatalf("expected 1 upstream block, got %d", len(cfg.Upstreams))
	}

	up := cfg.Upstreams[0]
	if up.Name != "backend" {
		t.Errorf("expected upstream name 'backend', got '%s'", up.Name)
	}
	if up.Method != "round_robin" {
		t.Errorf("expected method 'round_robin', got '%s'", up.Method)
	}
	if len(up.Backends) != 3 {
		t.Fatalf("expected 3 backends, got %d", len(up.Backends))
	}
	if up.Backends[0].Address != "127.0.0.1:3001" {
		t.Errorf("expected first backend '127.0.0.1:3001', got '%s'", up.Backends[0].Address)
	}
	if up.Backends[2].Weight != 3 {
		t.Errorf("expected third backend weight 3, got %d", up.Backends[2].Weight)
	}
	if up.Backends[0].Weight != 1 {
		t.Errorf("expected default weight 1, got %d", up.Backends[0].Weight)
	}
}

func TestParseUpstreamLeastConn(t *testing.T) {
	configContent := `
upstream api {
    method least_conn;
    server 10.0.0.1:8080;
    server 10.0.0.2:8080;
}

server {
    listen 80;

    location / {
        proxy_pass http://api;
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

	if cfg.Upstreams[0].Method != "least_conn" {
		t.Errorf("expected method 'least_conn', got '%s'", cfg.Upstreams[0].Method)
	}
}

func TestParseUpstreamInvalidMethod(t *testing.T) {
	configContent := `
upstream bad {
    method random;
    server 127.0.0.1:3001;
}

server {
    listen 80;
    location / {
        proxy_pass http://bad;
    }
}
`
	tempDir := t.TempDir()
	tempConfigFile := filepath.Join(tempDir, "wisp.conf")
	os.WriteFile(tempConfigFile, []byte(configContent), 0644)

	_, err := Parse(tempConfigFile)
	if err == nil {
		t.Error("expected error for invalid upstream method, got nil")
	}
}

func TestParseProxySetHeader(t *testing.T) {
	configContent := `
server {
    listen 8080;
    location /api/ {
        proxy_pass http://backend;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Custom wisp value;
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

	loc := cfg.Servers[0].Locations[0]
	if len(loc.ProxySetHeaders) != 4 {
		t.Fatalf("expected 4 proxy_set_header entries, got %d", len(loc.ProxySetHeaders))
	}

	want := []struct{ name, value string }{
		{"Host", "$host"},
		{"X-Real-IP", "$remote_addr"},
		{"X-Forwarded-For", "$proxy_add_x_forwarded_for"},
		{"X-Custom", "wisp value"},
	}
	for i, w := range want {
		got := loc.ProxySetHeaders[i]
		if got.Name != w.name || got.Value != w.value {
			t.Errorf("entry %d: expected (%s=%s), got (%s=%s)", i, w.name, w.value, got.Name, got.Value)
		}
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

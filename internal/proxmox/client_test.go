package proxmox

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewClient(t *testing.T) {
	client, err := NewClient("https://proxmox.local:8006", false)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	if client.BaseURL() != "https://proxmox.local:8006" {
		t.Errorf("expected base URL 'https://proxmox.local:8006', got '%s'", client.BaseURL())
	}
}

func TestNewClient_InvalidURL(t *testing.T) {
	_, err := NewClient("://invalid", false)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestClient_Forward(t *testing.T) {
	var receivedAuth string
	var receivedMethod string
	var receivedPath string
	var receivedBody string
	var receivedContentType string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		receivedContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":"success"}`))
	}))
	defer mockServer.Close()

	client, err := NewClient(mockServer.URL, true)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	authHeader := "PVEAPIToken=user@pve!token=secret123"
	resp, err := client.Forward(http.MethodPost, "/api2/json/nodes/pve/qemu", strings.NewReader("vmid=100"), "application/x-www-form-urlencoded", authHeader)
	if err != nil {
		t.Fatalf("Forward failed: %v", err)
	}
	defer resp.Body.Close()

	if receivedAuth != authHeader {
		t.Errorf("expected auth header '%s', got '%s'", authHeader, receivedAuth)
	}
	if receivedMethod != http.MethodPost {
		t.Errorf("expected method POST, got '%s'", receivedMethod)
	}
	if receivedPath != "/api2/json/nodes/pve/qemu" {
		t.Errorf("expected path '/api2/json/nodes/pve/qemu', got '%s'", receivedPath)
	}
	if receivedBody != "vmid=100" {
		t.Errorf("expected body 'vmid=100', got '%s'", receivedBody)
	}
	if receivedContentType != "application/x-www-form-urlencoded" {
		t.Errorf("expected content-type 'application/x-www-form-urlencoded', got '%s'", receivedContentType)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	if string(respBody) != `{"data":"success"}` {
		t.Errorf("expected response body '{\"data\":\"success\"}', got '%s'", string(respBody))
	}
}

func TestClient_Forward_WithQueryString(t *testing.T) {
	var receivedPath string
	var receivedQuery string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	client, _ := NewClient(mockServer.URL, true)

	resp, err := client.Forward(http.MethodGet, "/api2/json/nodes/pve/qemu?full=1", nil, "", "")
	if err != nil {
		t.Fatalf("Forward failed: %v", err)
	}
	resp.Body.Close()

	if receivedPath != "/api2/json/nodes/pve/qemu" {
		t.Errorf("expected path '/api2/json/nodes/pve/qemu', got '%s'", receivedPath)
	}
	if receivedQuery != "full=1" {
		t.Errorf("expected query 'full=1', got '%s'", receivedQuery)
	}
}

func TestClient_ForwardRequest(t *testing.T) {
	var receivedAuth string
	var receivedPath string
	var receivedQuery string

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedPath = r.URL.Path
		receivedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer mockServer.Close()

	client, _ := NewClient(mockServer.URL, true)

	authHeader := "PVEAPIToken=user@pve!token=secret"
	originalReq := httptest.NewRequest(http.MethodGet, "/api2/json/cluster/resources?type=vm", nil)
	originalReq.Header.Set("Accept", "application/json")
	originalReq.Header.Set("Authorization", authHeader)

	resp, err := client.ForwardRequest(originalReq, nil)
	if err != nil {
		t.Fatalf("ForwardRequest failed: %v", err)
	}
	resp.Body.Close()

	if receivedAuth != authHeader {
		t.Errorf("expected auth header '%s', got '%s'", authHeader, receivedAuth)
	}
	if receivedPath != "/api2/json/cluster/resources" {
		t.Errorf("expected path '/api2/json/cluster/resources', got '%s'", receivedPath)
	}
	if receivedQuery != "type=vm" {
		t.Errorf("expected query 'type=vm', got '%s'", receivedQuery)
	}
}

func TestClient_Forward_ServerError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"errors":{"message":"internal error"}}`))
	}))
	defer mockServer.Close()

	client, _ := NewClient(mockServer.URL, true)

	resp, err := client.Forward(http.MethodGet, "/api2/json/nodes", nil, "", "")
	if err != nil {
		t.Fatalf("Forward should not error on server error, got: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", resp.StatusCode)
	}
}

func TestClient_Forward_ConnectionError(t *testing.T) {
	client, _ := NewClient("http://localhost:1", true)

	_, err := client.Forward(http.MethodGet, "/api2/json/nodes", nil, "", "")
	if err == nil {
		t.Fatal("expected error for connection failure")
	}
}

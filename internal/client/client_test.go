package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestModelSupportRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/v1/dedicated/support" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.URL.Query().Get("modelName"); got != "org/model" {
			t.Fatalf("modelName = %q", got)
		}
		if got := r.URL.Query().Get("engine"); got != "VLLM" {
			t.Fatalf("engine = %q", got)
		}

		_ = json.NewEncoder(w).Encode(EngineSupport{
			Supported:         true,
			Known:             true,
			SupportingEngines: []string{"VLLM", "SGLANG"},
		})
	}))
	defer server.Close()

	client, err := New(server.URL+"/api/v1", "test-key", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	support, err := client.ModelSupport(context.Background(), ModelSupportRequest{
		Engine:    "VLLM",
		ModelName: "org/model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !support.Supported {
		t.Fatal("expected model to be supported")
	}
}

func TestCreateDeploymentRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/dedicated/deployments" {
			t.Fatalf("path = %s", r.URL.Path)
		}

		var request DedicatedDeployment
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.DeploymentName != "example" {
			t.Fatalf("deploymentName = %q", request.DeploymentName)
		}
		if len(request.DeviceConfigs) != 1 || !request.DeviceConfigs[0].Selected {
			t.Fatalf("deviceConfigs = %#v", request.DeviceConfigs)
		}

		request.ID = 123
		request.ExternalAlias = "acct-example"
		request.Status = &DeploymentStatus{Status: "STARTING"}
		_ = json.NewEncoder(w).Encode(request)
	}))
	defer server.Close()

	client, err := New(server.URL+"/api/v1", "test-key", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	created, err := client.CreateDeployment(context.Background(), DedicatedDeployment{
		DeploymentName: "example",
		ModelName:      "org/model",
		DeviceConfigs: []DedicatedDeviceConfig{
			{Device: "H100_80GB", Count: 1, Selected: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != 123 {
		t.Fatalf("id = %d", created.ID)
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(&APIError{StatusCode: http.StatusNotFound}) {
		t.Fatal("expected IsNotFound to detect 404")
	}
	if IsNotFound(&APIError{StatusCode: http.StatusInternalServerError}) {
		t.Fatal("expected IsNotFound to reject non-404")
	}
}

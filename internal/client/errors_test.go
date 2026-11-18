package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := New(server.URL+"/api/v1", "test-key", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestNewValidatesEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantErr  bool
		wantURL  string
	}{
		{name: "empty uses default", endpoint: "", wantURL: DefaultEndpoint},
		{name: "whitespace uses default", endpoint: "   ", wantURL: DefaultEndpoint},
		{name: "absolute URL", endpoint: "https://example.test/api/v1", wantURL: "https://example.test/api/v1"},
		{name: "relative path", endpoint: "/api/v1", wantErr: true},
		{name: "missing scheme", endpoint: "api.parasail.io/api/v1", wantErr: true},
		{name: "invalid host", endpoint: "http://[::1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.endpoint, "key", 0)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("New(%q) succeeded, want error", tt.endpoint)
				}
				return
			}
			if err != nil {
				t.Fatalf("New(%q) error = %v", tt.endpoint, err)
			}
			if got := client.endpoint.String(); got != tt.wantURL {
				t.Fatalf("endpoint = %q, want %q", got, tt.wantURL)
			}
			if client.httpClient.Timeout != 60*time.Second {
				t.Fatalf("default timeout = %s, want 60s", client.httpClient.Timeout)
			}
		})
	}
}

func TestAPIErrorStatusCodes(t *testing.T) {
	tests := []struct {
		name         string
		status       int
		body         string
		wantNotFound bool
		wantMessage  string
	}{
		{
			name:         "not found",
			status:       http.StatusNotFound,
			body:         `{"message":"deployment not found"}`,
			wantNotFound: true,
			wantMessage:  `parasail api returned HTTP 404: {"message":"deployment not found"}`,
		},
		{
			name:        "unauthorized",
			status:      http.StatusUnauthorized,
			body:        "invalid api key\n",
			wantMessage: "parasail api returned HTTP 401: invalid api key",
		},
		{
			name:        "bad request",
			status:      http.StatusBadRequest,
			body:        `Deployment name must be between 2 and 32 characters.`,
			wantMessage: "parasail api returned HTTP 400: Deployment name must be between 2 and 32 characters.",
		},
		{
			name:        "server error without body",
			status:      http.StatusInternalServerError,
			wantMessage: "parasail api returned HTTP 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			})

			_, err := client.GetDeployment(context.Background(), 42)
			if err == nil {
				t.Fatal("GetDeployment succeeded, want error")
			}

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error %T is not *APIError", err)
			}
			if apiErr.StatusCode != tt.status {
				t.Fatalf("StatusCode = %d, want %d", apiErr.StatusCode, tt.status)
			}
			if got := err.Error(); got != tt.wantMessage {
				t.Fatalf("Error() = %q, want %q", got, tt.wantMessage)
			}
			if got := IsNotFound(err); got != tt.wantNotFound {
				t.Fatalf("IsNotFound = %t, want %t", got, tt.wantNotFound)
			}
		})
	}
}

func TestIsNotFoundWrapped(t *testing.T) {
	wrapped := fmt.Errorf("reading deployment: %w", &APIError{StatusCode: http.StatusNotFound})
	if !IsNotFound(wrapped) {
		t.Fatal("IsNotFound should detect a wrapped 404")
	}
	if IsNotFound(errors.New("not found")) {
		t.Fatal("IsNotFound should ignore non-API errors")
	}
	if IsNotFound(nil) {
		t.Fatal("IsNotFound(nil) should be false")
	}
}

func TestDecodeError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html>gateway</html>")
	})

	_, err := client.ListDeployments(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode response body") {
		t.Fatalf("err = %v, want decode error", err)
	}
	if IsNotFound(err) {
		t.Fatal("decode errors must not look like 404s")
	}
}

func TestEmptySuccessBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	deployments, err := client.ListDeployments(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if deployments != nil {
		t.Fatalf("deployments = %#v, want nil", deployments)
	}
}

func TestRequestTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(release)
		server.Close()
	})

	client, err := New(server.URL, "key", 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.ListDeployments(context.Background())
	if err == nil || !strings.Contains(err.Error(), "send request") {
		t.Fatalf("err = %v, want send request timeout", err)
	}
}

func TestContextCancellation(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := client.ListDeployments(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestRequestHeaders(t *testing.T) {
	t.Run("with body and key", func(t *testing.T) {
		client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("accept"); got != "application/json" {
				t.Errorf("accept = %q", got)
			}
			if got := r.Header.Get("content-type"); got != "application/json" {
				t.Errorf("content-type = %q", got)
			}
			if got := r.Header.Get("authorization"); got != "Bearer test-key" {
				t.Errorf("authorization = %q", got)
			}
			_, _ = io.WriteString(w, `{"id":1}`)
		})
		if _, err := client.CreateDeployment(context.Background(), DedicatedDeployment{DeploymentName: "x"}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("without body or key", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("content-type"); got != "" {
				t.Errorf("content-type = %q, want empty for GET", got)
			}
			if got := r.Header.Get("authorization"); got != "" {
				t.Errorf("authorization = %q, want empty without key", got)
			}
			_, _ = io.WriteString(w, `[]`)
		}))
		t.Cleanup(server.Close)

		client, err := New(server.URL, "", time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.ListDeployments(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestEndpointPathJoining(t *testing.T) {
	for _, suffix := range []string{"/api/v1", "/api/v1/"} {
		t.Run(suffix, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/dedicated/deployments" {
					t.Errorf("path = %q", r.URL.Path)
				}
				_, _ = io.WriteString(w, `[]`)
			}))
			t.Cleanup(server.Close)

			client, err := New(server.URL+suffix, "key", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.ListDeployments(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeploymentLifecycleRoutes(t *testing.T) {
	type call struct {
		method string
		path   string
	}

	var calls []call
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, call{method: r.Method, path: r.URL.Path})
		switch r.Method {
		case http.MethodPut:
			var body DedicatedDeployment
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode PUT body: %v", err)
			}
			body.ID = 7
			_ = json.NewEncoder(w).Encode(body)
		case http.MethodGet:
			_, _ = io.WriteString(w, `{"id":7,"deploymentName":"example","status":{"status":"ONLINE"}}`)
		default:
			w.WriteHeader(http.StatusOK)
		}
	})

	ctx := context.Background()
	got, err := client.GetDeployment(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status == nil || got.Status.Status != "ONLINE" {
		t.Fatalf("status = %#v", got.Status)
	}

	updated, err := client.UpdateDeployment(ctx, 7, DedicatedDeployment{DeploymentName: "example", Replicas: 2})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != 7 || updated.Replicas != 2 {
		t.Fatalf("updated = %#v", updated)
	}

	for _, fn := range []func(context.Context, int64) error{
		client.PauseDeployment,
		client.ResumeDeployment,
		client.DeleteDeployment,
	} {
		if err := fn(ctx, 7); err != nil {
			t.Fatal(err)
		}
	}

	want := []call{
		{http.MethodGet, "/api/v1/dedicated/deployments/7"},
		{http.MethodPut, "/api/v1/dedicated/deployments/7"},
		{http.MethodPost, "/api/v1/dedicated/deployments/7/pause"},
		{http.MethodPost, "/api/v1/dedicated/deployments/7/resume"},
		{http.MethodDelete, "/api/v1/dedicated/deployments/7"},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("call %d = %#v, want %#v", i, calls[i], want[i])
		}
	}
}

func TestDeviceConfigsQuery(t *testing.T) {
	tests := []struct {
		name    string
		request DeviceConfigsRequest
		want    string
	}{
		{
			name:    "model only",
			request: DeviceConfigsRequest{ModelName: "org/model"},
			want:    "modelName=org%2Fmodel",
		},
		{
			name: "all fields",
			request: DeviceConfigsRequest{
				Engine:              "VLLM",
				ModelName:           "org/model",
				ModelAccessKey:      "hf_x",
				DraftModelName:      "org/draft",
				DraftModelAccessKey: "hf_y",
				ModelAccessKeyFrom:  99,
			},
			want: "draftModelAccessKey=hf_y&draftModelName=org%2Fdraft&engine=VLLM&modelAccessKey=hf_x&modelAccessKeyFrom=99&modelName=org%2Fmodel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/dedicated/devices" {
					t.Errorf("path = %q", r.URL.Path)
				}
				if r.URL.RawQuery != tt.want {
					t.Errorf("query = %q, want %q", r.URL.RawQuery, tt.want)
				}
				_, _ = io.WriteString(w, `[{"device":"RTX5090","count":1,"cost":1.25,"available":true,"recommended":true}]`)
			})

			configs, err := client.DeviceConfigs(context.Background(), tt.request)
			if err != nil {
				t.Fatal(err)
			}
			if len(configs) != 1 || configs[0].Device != "RTX5090" || configs[0].Cost == nil || *configs[0].Cost != 1.25 {
				t.Fatalf("configs = %#v", configs)
			}
		})
	}
}

func TestModelSupportOmitsZeroAccessKeyFrom(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("modelAccessKeyFrom") {
			t.Errorf("query unexpectedly includes modelAccessKeyFrom: %q", r.URL.RawQuery)
		}
		if r.URL.Query().Has("engine") {
			t.Errorf("query unexpectedly includes empty engine: %q", r.URL.RawQuery)
		}
		_, _ = io.WriteString(w, `{"supported":false,"known":true,"messages":[{"level":"ERROR","content":"too big"}]}`)
	})

	support, err := client.ModelSupport(context.Background(), ModelSupportRequest{ModelName: "org/model"})
	if err != nil {
		t.Fatal(err)
	}
	if support.Supported || !support.Known || len(support.Messages) != 1 || support.Messages[0].Level != "ERROR" {
		t.Fatalf("support = %#v", support)
	}
}

package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	parasail "github.com/loewenthal-corp/terraform-provider-parasail/internal/client"
)

// fakeParasail is an in-memory stand-in for the Parasail dedicated API. It
// echoes stored deployments the way the live API does (name, alias, selected
// device configs, scale-down policy) so provider logic can be exercised end to
// end with the Terraform binary but without a network or API key.
type fakeParasail struct {
	t      *testing.T
	server *httptest.Server

	mu          sync.Mutex
	nextID      int64
	deployments map[int64]*parasail.DedicatedDeployment
	// statuses is the sequence of statuses GET returns for new or updated
	// deployments; the last entry repeats.
	statuses []string
	gets     map[int64]int
	requests []string
}

func newFakeParasail(t *testing.T) *fakeParasail {
	t.Helper()

	f := &fakeParasail{
		t:           t,
		nextID:      1000,
		deployments: map[int64]*parasail.DedicatedDeployment{},
		statuses:    []string{"ONLINE"},
		gets:        map[int64]int{},
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeParasail) endpoint() string {
	return f.server.URL + "/api/v1"
}

func (f *fakeParasail) setStatuses(statuses ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses = statuses
}

func (f *fakeParasail) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.deployments)
}

func (f *fakeParasail) get(id int64) *parasail.DedicatedDeployment {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.deployments[id]
	if !ok {
		return nil
	}
	copied := *d
	return &copied
}

func (f *fakeParasail) only() *parasail.DedicatedDeployment {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, d := range f.deployments {
		copied := *d
		return &copied
	}
	return nil
}

func (f *fakeParasail) mutate(id int64, fn func(*parasail.DedicatedDeployment)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f.deployments[id])
}

func (f *fakeParasail) remove(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.deployments, id)
}

func (f *fakeParasail) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Header.Get("authorization") != "Bearer test-key" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	route := strings.TrimPrefix(r.URL.Path, "/api/v1")
	f.requests = append(f.requests, r.Method+" "+route)

	switch {
	case r.Method == http.MethodGet && route == "/dedicated/devices":
		writeJSON(w, []parasail.DedicatedDeviceConfig{
			{Device: "RTX5090", Count: 1, DisplayName: "RTX5090", Cost: float64Ptr(0.65), Recommended: true},
			{Device: "H100SXM", Count: 1, DisplayName: "H100SXM", Cost: float64Ptr(2.75), Available: true},
			{Device: "H100SXM", Count: 2, DisplayName: "H100SXM x 2", Cost: float64Ptr(5.5), Available: true},
		})
	case r.Method == http.MethodGet && route == "/dedicated/support":
		writeJSON(w, parasail.EngineSupport{
			Supported:         r.URL.Query().Get("modelName") != "unknown/model",
			Known:             r.URL.Query().Get("modelName") != "unknown/model",
			SupportingEngines: []string{"VLLM"},
			Messages:          []parasail.LogMessage{{Level: "INFO", Content: "ok"}},
		})
	case r.Method == http.MethodGet && route == "/dedicated/deployments":
		out := make([]parasail.DedicatedDeployment, 0, len(f.deployments))
		for _, d := range f.deployments {
			out = append(out, *d)
		}
		writeJSON(w, out)
	case r.Method == http.MethodPost && route == "/dedicated/deployments":
		var body parasail.DedicatedDeployment
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.nextID++
		body.ID = f.nextID
		f.store(&body)
		writeJSON(w, body)
	default:
		f.serveDeployment(w, r, route)
	}
}

func (f *fakeParasail) serveDeployment(w http.ResponseWriter, r *http.Request, route string) {
	idText, ok := strings.CutPrefix(route, "/dedicated/deployments/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	existing, ok := f.deployments[id]
	if !ok {
		http.Error(w, "deployment not found", http.StatusNotFound)
		return
	}

	switch r.Method {
	case http.MethodGet:
		f.gets[id]++
		index := min(f.gets[id], len(f.statuses)) - 1
		existing.Status = &parasail.DeploymentStatus{Status: f.statuses[index]}
		if existing.Status.Status == "ERROR" {
			existing.Status.StatusMessage = "out of capacity"
		}
		writeJSON(w, existing)
	case http.MethodPut:
		var body parasail.DedicatedDeployment
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		body.ID = id
		f.gets[id] = 0
		f.store(&body)
		writeJSON(w, body)
	case http.MethodDelete:
		delete(f.deployments, id)
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// store mimics the live API: it adds server-side fields, drops write-only
// keys, and keeps echoing the scale-down threshold for the NONE policy.
func (f *fakeParasail) store(d *parasail.DedicatedDeployment) {
	d.AccountName = "acct"
	d.ExternalAlias = "acct-" + d.DeploymentName
	d.DisplayName = d.ExternalAlias
	d.BaseURL = "https://api.parasail.io/v1"
	d.ModelAccessKey = ""
	d.DraftModelAccessKey = ""
	if d.ScaleDownPolicy == "NONE" {
		d.ScaleDownThreshold = 28800000
	}
	d.Status = &parasail.DeploymentStatus{Status: "STARTING"}
	stored := *d
	f.deployments[d.ID] = &stored
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func float64Ptr(v float64) *float64 {
	return &v
}

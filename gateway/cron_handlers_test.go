package gateway

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/cron"
)

func newTestCronGateway(t *testing.T) (*Gateway, *cron.Service) {
	t.Helper()

	tmp := filepath.Join(t.TempDir(), "cron.json")
	if err := os.WriteFile(tmp, []byte(`{"version":1,"jobs":[]}`), 0644); err != nil {
		t.Fatalf("write cron file: %v", err)
	}
	store, err := cron.Load(tmp)
	if err != nil {
		t.Fatalf("cron.Load() error = %v", err)
	}
	svc := cron.NewService(store, nil, nil)

	gw := newTestGatewayFull(t)
	gw.SetCron(svc)
	return gw, svc
}

// ---------------------------------------------------------------------------
// handleCronListJobs
// ---------------------------------------------------------------------------

func TestCronListJobsNilService(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/cron/jobs", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil service: status = %d", rec.Code)
	}
}

func TestCronListJobsEmpty(t *testing.T) {
	t.Parallel()

	gw, _ := newTestCronGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/cron/jobs", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty list: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload cronJobListResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Count != 0 {
		t.Fatalf("count = %d, want 0", payload.Count)
	}
}

// ---------------------------------------------------------------------------
// handleCronCreateJob
// ---------------------------------------------------------------------------

func TestCronCreateJobSuccess(t *testing.T) {
	t.Parallel()

	gw, _ := newTestCronGateway(t)
	handler := gw.Handler()

	body := `{"name":"daily-report","schedule":{"kind":"cron","expression":"0 9 * * *"},"payload":{"content":"generate report"}}`
	rec := doRequest(t, handler, http.MethodPost, "/operator/cron/jobs", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload cronJobResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.Job.Name != "daily-report" {
		t.Fatalf("name = %q, want daily-report", payload.Job.Name)
	}
	if payload.Job.ID == "" {
		t.Fatal("job ID is empty")
	}
	if !payload.Job.Enabled {
		t.Fatal("expected enabled=true by default")
	}
}

func TestCronCreateJobRejectsLegacyPayload(t *testing.T) {
	t.Parallel()

	gw, _ := newTestCronGateway(t)
	handler := gw.Handler()

	body := `{"name":"daily-report","schedule":{"cron":"0 9 * * *"},"prompt":"generate report","agent":"ops:daily","enabled":true}`
	rec := doRequest(t, handler, http.MethodPost, "/operator/cron/jobs", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("legacy create: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCronCreateJobMissingContent(t *testing.T) {
	t.Parallel()

	gw, _ := newTestCronGateway(t)
	handler := gw.Handler()

	body := `{"name":"empty","schedule":{"kind":"cron","expression":"0 9 * * *"},"payload":{"content":""}}`
	rec := doRequest(t, handler, http.MethodPost, "/operator/cron/jobs", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing content: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCronCreateJobInvalidJSON(t *testing.T) {
	t.Parallel()

	gw, _ := newTestCronGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/cron/jobs", "not-json")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCronCreateJobRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw, svc := newTestCronGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPost, "/operator/cron/jobs", `{"name":"daily-report","schedule":{"kind":"cron","expression":"0 9 * * *"},"payload":{"content":"generate report"}} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing json: status = %d body=%s", rec.Code, rec.Body.String())
	}

	jobs := svc.Store().List()
	if len(jobs) != 0 {
		t.Fatalf("jobs = %#v, want empty", jobs)
	}
}

func TestCronCreateJobNilService(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodPost, "/operator/cron/jobs",
		`{"name":"test","schedule":{"kind":"cron","expression":"0 9 * * *"},"payload":{"content":"hi"}}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil service: status = %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// handleCronGetJob
// ---------------------------------------------------------------------------

func TestCronGetJobSuccess(t *testing.T) {
	t.Parallel()

	gw, _ := newTestCronGateway(t)
	handler := gw.Handler()

	// Create a job first.
	body := `{"name":"get-me","schedule":{"kind":"cron","expression":"0 9 * * *"},"payload":{"content":"test"}}`
	createRec := doRequest(t, handler, http.MethodPost, "/operator/cron/jobs", body)
	var created cronJobResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodGet, "/operator/cron/jobs/"+created.Job.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var got cronJobResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.Job.Name != "get-me" {
		t.Fatalf("name = %q, want get-me", got.Job.Name)
	}
}

func TestCronGetJobNotFound(t *testing.T) {
	t.Parallel()

	gw, _ := newTestCronGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/cron/jobs/nonexistent", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// ---------------------------------------------------------------------------
// handleCronUpdateJob
// ---------------------------------------------------------------------------

func TestCronUpdateJobSuccess(t *testing.T) {
	t.Parallel()

	gw, _ := newTestCronGateway(t)
	handler := gw.Handler()

	// Create a job.
	createRec := doRequest(t, handler, http.MethodPost, "/operator/cron/jobs",
		`{"name":"original","schedule":{"kind":"cron","expression":"0 9 * * *"},"payload":{"content":"test"}}`)
	var created cronJobResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	// Patch the name.
	rec := doRequest(t, handler, http.MethodPatch, "/operator/cron/jobs/"+created.Job.ID,
		`{"name":"updated"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update: status = %d body=%s", rec.Code, rec.Body.String())
	}

	var updated cronJobResponse
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if updated.Job.Name != "updated" {
		t.Fatalf("name = %q, want updated", updated.Job.Name)
	}
}

func TestCronUpdateJobNotFound(t *testing.T) {
	t.Parallel()

	gw, _ := newTestCronGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodPatch, "/operator/cron/jobs/nonexistent",
		`{"name":"nope"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCronUpdateJobRejectsTrailingJSON(t *testing.T) {
	t.Parallel()

	gw, svc := newTestCronGateway(t)
	handler := gw.Handler()

	createRec := doRequest(t, handler, http.MethodPost, "/operator/cron/jobs",
		`{"name":"original","schedule":{"kind":"cron","expression":"0 9 * * *"},"payload":{"content":"test"}}`)
	var created cronJobResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodPatch, "/operator/cron/jobs/"+created.Job.ID, `{"name":"updated"} {"extra":true}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing json: status = %d body=%s", rec.Code, rec.Body.String())
	}

	job, err := svc.Store().Get(created.Job.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if job.Name != "original" {
		t.Fatalf("name = %q, want original", job.Name)
	}
}

// ---------------------------------------------------------------------------
// handleCronDeleteJob
// ---------------------------------------------------------------------------

func TestCronDeleteJobSuccess(t *testing.T) {
	t.Parallel()

	gw, _ := newTestCronGateway(t)
	handler := gw.Handler()

	// Create then delete.
	createRec := doRequest(t, handler, http.MethodPost, "/operator/cron/jobs",
		`{"name":"delete-me","schedule":{"kind":"cron","expression":"0 9 * * *"},"payload":{"content":"test"}}`)
	var created cronJobResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	rec := doRequest(t, handler, http.MethodDelete, "/operator/cron/jobs/"+created.Job.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: status = %d body=%s", rec.Code, rec.Body.String())
	}

	// Verify it is gone.
	getRec := doRequest(t, handler, http.MethodGet, "/operator/cron/jobs/"+created.Job.ID, "")
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("after delete: status = %d, want %d", getRec.Code, http.StatusNotFound)
	}
}

func TestCronDeleteJobNotFound(t *testing.T) {
	t.Parallel()

	gw, _ := newTestCronGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodDelete, "/operator/cron/jobs/nonexistent", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found: status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

// ---------------------------------------------------------------------------
// handleCronStatus
// ---------------------------------------------------------------------------

func TestCronStatusNilService(t *testing.T) {
	t.Parallel()

	gw := newTestGatewayFull(t)
	handler := gw.Handler()
	rec := doRequest(t, handler, http.MethodGet, "/operator/cron/status", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil service: status = %d", rec.Code)
	}
}

func TestCronStatusReturnsStatus(t *testing.T) {
	t.Parallel()

	gw, _ := newTestCronGateway(t)
	handler := gw.Handler()

	rec := doRequest(t, handler, http.MethodGet, "/operator/cron/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: code = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload cronServiceStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if payload.JobCount != 0 {
		t.Fatalf("job_count = %d, want 0", payload.JobCount)
	}
}

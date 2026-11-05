package agent

// Cron job handler tests over the real store: lazy list, create
// guards, full-row update rules, delete, and the bundle's missing
// cronJobs key. Route mounting is pinned in backend's tests.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/db"
	"github.com/radch-ai/ava/backend/internal/testdb"
)

// cronStore opens a fresh store holding one regular agent and one
// catalog model row jobs can reference.
func cronStore(t *testing.T) *db.Queries {
	t.Helper()
	sqlDB := testdb.Open(t)
	if _, err := sqlDB.Exec("INSERT INTO models (id) VALUES ('m1'), ('m2')"); err != nil {
		t.Fatalf("seed models: %v", err)
	}
	queries := db.New(sqlDB)
	if err := queries.CreateAgent(t.Context(), db.CreateAgentParams{
		ID: "research", Name: "Research", Icon: "globe",
		SoulPersonality: "curious", SoulIdentity: "verifies claims",
		OperatingInstructions: "cite everything", ToolsContext: "",
	}); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return queries
}

func TestGetCronJobsEmptyList(t *testing.T) {
	queries := cronStore(t)
	rec, err := serve(t, "GET /api/agent/{id}/cron-jobs", GetCronJobs(queries), http.MethodGet, "/api/agent/research/cron-jobs", "")
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"cronJobs":[]}` {
		t.Errorf("body = %s, want {\"cronJobs\":[]}", got)
	}
}

func TestCreateCronJobMintsID(t *testing.T) {
	queries := cronStore(t)

	custom := "*/5 * * * *"
	body := `{"time":"09:00","frequency":"custom","cronExpression":"*/5 * * * *","model":"auto","prompt":"hi"}`
	rec, err := serve(t, "POST /api/agent/{id}/cron-jobs", CreateCronJob(queries), http.MethodPost, "/api/agent/research/cron-jobs", body)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, want 201 (body %s)", rec.Code, rec.Body)
	}
	var job apitypes.CronJob
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if job.ID == "" || job.Time != "09:00" || job.Frequency != apitypes.CronFrequencyCustom {
		t.Errorf("job = %+v", job)
	}
	if job.CronExpression == nil || *job.CronExpression != custom {
		t.Errorf("cronExpression = %v, want %q", job.CronExpression, custom)
	}
	if job.Model != "auto" {
		t.Errorf("model = %q, want auto", job.Model)
	}

	rec, err = serve(t, "POST /api/agent/{id}/cron-jobs", CreateCronJob(queries), http.MethodPost, "/api/agent/research/cron-jobs",
		`{"time":"10:00","frequency":"daily","cronExpression":"ignored","model":"m1","prompt":"yo"}`)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("code = %d, want 201 (body %s)", rec.Code, rec.Body)
	}
	var daily apitypes.CronJob
	if err := json.Unmarshal(rec.Body.Bytes(), &daily); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if daily.ID == "" || daily.ID == job.ID {
		t.Errorf("second id = %q, want fresh mint distinct from %q", daily.ID, job.ID)
	}
	if daily.CronExpression != nil {
		t.Errorf("daily cronExpression = %v, want none", daily.CronExpression)
	}
	if daily.Model != "m1" {
		t.Errorf("model = %q, want m1", daily.Model)
	}
}

func TestCreateCronJobGuards(t *testing.T) {
	queries := cronStore(t)

	rec, err := serve(t, "POST /api/agent/{id}/cron-jobs", CreateCronJob(queries), http.MethodPost, "/api/agent/research/cron-jobs", "{bad json")
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Invalid request body") {
		t.Errorf("bad body = %d %s, want 400", rec.Code, rec.Body)
	}

	rec, err = serve(t, "POST /api/agent/{id}/cron-jobs", CreateCronJob(queries), http.MethodPost, "/api/agent/ghost/cron-jobs",
		`{"time":"09:00","frequency":"daily","prompt":"x"}`)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `Agent \"ghost\" not found`) {
		t.Errorf("unknown agent = %d %s, want 404", rec.Code, rec.Body)
	}
}

func TestUpdateCronJobFullRewrite(t *testing.T) {
	queries := cronStore(t)

	rec, err := serve(t, "POST /api/agent/{id}/cron-jobs", CreateCronJob(queries), http.MethodPost, "/api/agent/research/cron-jobs",
		`{"time":"09:00","frequency":"custom","cronExpression":"0 9 * * *","model":"m1","prompt":"old"}`)
	if err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d %v", rec.Code, err)
	}
	var seeded apitypes.CronJob
	if err := json.Unmarshal(rec.Body.Bytes(), &seeded); err != nil {
		t.Fatalf("decode seed: %v", err)
	}

	// custom -> daily clears the expression; model auto -> NULL.
	rec, err = serve(t, "PATCH /api/agent/{id}/cron-jobs/{jobId}", UpdateCronJob(queries), http.MethodPatch, "/api/agent/research/cron-jobs/"+seeded.ID,
		`{"time":"10:00","frequency":"daily","cronExpression":"0 9 * * *","model":"auto","prompt":"new"}`)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	var job apitypes.CronJob
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if job.CronExpression != nil {
		t.Errorf("daily cronExpression = %v, want none", job.CronExpression)
	}
	if job.Model != "auto" {
		t.Errorf("model = %q, want auto", job.Model)
	}
	if job.Prompt != "new" || job.Time != "10:00" {
		t.Errorf("job = %+v", job)
	}

	// daily -> custom stores the expression; concrete model roundtrips.
	rec, err = serve(t, "PATCH /api/agent/{id}/cron-jobs/{jobId}", UpdateCronJob(queries), http.MethodPatch, "/api/agent/research/cron-jobs/"+seeded.ID,
		`{"time":"11:00","frequency":"custom","cronExpression":"15 3 * * *","model":"m2","prompt":"new"}`)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if job.CronExpression == nil || *job.CronExpression != "15 3 * * *" {
		t.Errorf("cronExpression = %v", job.CronExpression)
	}
	if job.Model != "m2" {
		t.Errorf("model = %q, want m2", job.Model)
	}
}

func TestUpdateCronJobUnknowns(t *testing.T) {
	queries := cronStore(t)

	rec, err := serve(t, "PATCH /api/agent/{id}/cron-jobs/{jobId}", UpdateCronJob(queries), http.MethodPatch, "/api/agent/ghost/cron-jobs/j1",
		`{"time":"09:00","frequency":"daily","prompt":"x"}`)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `Agent \"ghost\" not found`) {
		t.Errorf("unknown agent = %d %s, want 404", rec.Code, rec.Body)
	}

	rec, err = serve(t, "PATCH /api/agent/{id}/cron-jobs/{jobId}", UpdateCronJob(queries), http.MethodPatch, "/api/agent/research/cron-jobs/missing",
		`{"time":"09:00","frequency":"daily","prompt":"x"}`)
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `Cron job \"missing\" not found`) {
		t.Errorf("unknown job = %d %s, want 404", rec.Code, rec.Body)
	}
}

func TestDeleteCronJob(t *testing.T) {
	queries := cronStore(t)

	rec, err := serve(t, "POST /api/agent/{id}/cron-jobs", CreateCronJob(queries), http.MethodPost, "/api/agent/research/cron-jobs",
		`{"time":"09:00","frequency":"daily","prompt":"x"}`)
	if err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("seed: %d %v", rec.Code, err)
	}
	var seeded apitypes.CronJob
	if err := json.Unmarshal(rec.Body.Bytes(), &seeded); err != nil {
		t.Fatalf("decode seed: %v", err)
	}

	rec, err = serve(t, "DELETE /api/agent/{id}/cron-jobs/{jobId}", DeleteCronJob(queries), http.MethodDelete, "/api/agent/research/cron-jobs/"+seeded.ID, "")
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Errorf("delete = %d %q, want 204 empty", rec.Code, rec.Body)
	}

	rec, err = serve(t, "DELETE /api/agent/{id}/cron-jobs/{jobId}", DeleteCronJob(queries), http.MethodDelete, "/api/agent/research/cron-jobs/"+seeded.ID, "")
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `Cron job \"`+seeded.ID+`\" not found`) {
		t.Errorf("redelete = %d %s, want 404", rec.Code, rec.Body)
	}
}

func TestDetailBundleHasNoCronJobs(t *testing.T) {
	queries := cronStore(t)
	rec, err := serve(t, "GET /api/agent/{id}/config", GetAgentConfig(queries), http.MethodGet, "/api/agent/research/config", "")
	if err != nil {
		t.Fatalf("serve: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "cronJobs") {
		t.Errorf("bundle carries cronJobs: %s", rec.Body)
	}
}

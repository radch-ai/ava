// The cron job endpoints: GET/POST /api/agent/{id}/cron-jobs and
// PATCH/DELETE /api/agent/{id}/cron-jobs/{jobId}. Jobs are fetched
// lazily - they do not ride the agent detail bundle.
package agent

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/radch-ai/ava/backend/apitypes"
	"github.com/radch-ai/ava/backend/internal/api"
	"github.com/radch-ai/ava/backend/internal/db"
)

// jobFromRow reads one stored row into the wire shape.
func jobFromRow(row db.CronJob) apitypes.CronJob {
	return apitypes.CronJob{
		ID:             row.ID,
		Time:           row.Time,
		Frequency:      apitypes.CronFrequency(row.Frequency),
		CronExpression: stringPtrNil(row.CronExpression),
		Model:          modelFromDB(row.Model),
		Prompt:         row.Prompt,
	}
}

// rowFields applies the shared row rules to a wire job: the cron
// expression is stored only for "custom" frequency, and model
// ""/"auto" stores NULL.
func rowFields(job apitypes.CronJob) (sql.NullString, sql.NullString) {
	cronExpression := sql.NullString{}
	if job.Frequency == apitypes.CronFrequencyCustom && job.CronExpression != nil {
		cronExpression = sql.NullString{String: *job.CronExpression, Valid: true}
	}
	return cronExpression, modelToDB(job.Model)
}

// GetCronJobs answers the lazy list: the agent's jobs, oldest first.
func GetCronJobs(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")

		if _, err := queries.GetAgentID(r.Context(), id); errors.Is(err, sql.ErrNoRows) {
			return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Agent %q not found", id))
		} else if err != nil {
			return fmt.Errorf("get agent %q: %w", id, err)
		}

		rows, err := queries.ListCronJobs(r.Context(), id)
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("list cron jobs for %q: %w", id, err)
		}
		jobs := make([]apitypes.CronJob, 0, len(rows))
		for _, row := range rows {
			jobs = append(jobs, jobFromRow(row))
		}
		return api.WriteJSON(w, http.StatusOK, map[string][]apitypes.CronJob{"cronJobs": jobs})
	}
}

// CreateCronJob stores one new job with a server-minted id.
func CreateCronJob(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req apitypes.CronJob
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		}
		id := r.PathValue("id")

		if _, err := queries.GetAgentID(r.Context(), id); errors.Is(err, sql.ErrNoRows) {
			return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Agent %q not found", id))
		} else if err != nil {
			return fmt.Errorf("get agent %q: %w", id, err)
		}

		jobID := uuid.NewString()
		cronExpression, model := rowFields(req)
		if err := queries.CreateCronJob(r.Context(), db.CreateCronJobParams{
			ID:             jobID,
			AgentID:        id,
			Time:           req.Time,
			Frequency:      string(req.Frequency),
			CronExpression: cronExpression,
			Model:          model,
			Prompt:         req.Prompt,
		}); err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("create cron job for %q: %w", id, err)
		}

		row, err := queries.GetCronJob(r.Context(), db.GetCronJobParams{ID: jobID, AgentID: id})
		if err != nil {
			return fmt.Errorf("read back cron job %q for %q: %w", jobID, id, err)
		}
		return api.WriteJSON(w, http.StatusCreated, jobFromRow(row))
	}
}

// UpdateCronJob rewrites a job's editable columns wholesale.
func UpdateCronJob(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		var req apitypes.CronJob
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return api.WriteError(w, http.StatusBadRequest, "Invalid request body")
		}
		id := r.PathValue("id")
		jobID := r.PathValue("jobId")

		if _, err := queries.GetAgentID(r.Context(), id); errors.Is(err, sql.ErrNoRows) {
			return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Agent %q not found", id))
		} else if err != nil {
			return fmt.Errorf("get agent %q: %w", id, err)
		}

		jobParams := db.GetCronJobParams{ID: jobID, AgentID: id}
		if _, err := queries.GetCronJob(r.Context(), jobParams); errors.Is(err, sql.ErrNoRows) {
			return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Cron job %q not found", jobID))
		} else if err != nil {
			return fmt.Errorf("get cron job %q for %q: %w", jobID, id, err)
		}

		cronExpression, model := rowFields(req)
		if err := queries.UpdateCronJob(r.Context(), db.UpdateCronJobParams{
			Time:           req.Time,
			Frequency:      string(req.Frequency),
			CronExpression: cronExpression,
			Model:          model,
			Prompt:         req.Prompt,
			ID:             jobID,
			AgentID:        id,
		}); err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("update cron job %q for %q: %w", jobID, id, err)
		}

		row, err := queries.GetCronJob(r.Context(), jobParams)
		if err != nil {
			return fmt.Errorf("read back cron job %q for %q: %w", jobID, id, err)
		}
		return api.WriteJSON(w, http.StatusOK, jobFromRow(row))
	}
}

// DeleteCronJob removes one scoped job.
func DeleteCronJob(queries *db.Queries) api.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		id := r.PathValue("id")
		jobID := r.PathValue("jobId")

		if _, err := queries.GetAgentID(r.Context(), id); errors.Is(err, sql.ErrNoRows) {
			return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Agent %q not found", id))
		} else if err != nil {
			return fmt.Errorf("get agent %q: %w", id, err)
		}

		n, err := queries.DeleteCronJob(r.Context(), db.DeleteCronJobParams{ID: jobID, AgentID: id})
		if err != nil {
			_ = api.WriteError(w, http.StatusInternalServerError, "Internal server error")
			return fmt.Errorf("delete cron job %q for %q: %w", jobID, id, err)
		}
		if n == 0 {
			return api.WriteError(w, http.StatusNotFound, fmt.Sprintf("Cron job %q not found", jobID))
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
}

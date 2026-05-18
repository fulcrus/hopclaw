package gateway

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/cron"
)

type operatorCronSurface struct {
	cron *cron.Service
}

func newOperatorCronSurface(service *cron.Service) *operatorCronSurface {
	return &operatorCronSurface{cron: service}
}

func (s *operatorCronSurface) RegisterRoutes(mux *http.ServeMux, mountAuthed func(*http.ServeMux, string, func(http.ResponseWriter, *http.Request))) {
	if mux == nil || mountAuthed == nil {
		return
	}
	mountAuthed(mux, "GET /operator/cron/jobs", s.handleCronListJobs)
	mountAuthed(mux, "POST /operator/cron/jobs", s.handleCronCreateJob)
	mountAuthed(mux, "GET /operator/cron/jobs/{id}", s.handleCronGetJob)
	mountAuthed(mux, "PATCH /operator/cron/jobs/{id}", s.handleCronUpdateJob)
	mountAuthed(mux, "DELETE /operator/cron/jobs/{id}", s.handleCronDeleteJob)
	mountAuthed(mux, "POST /operator/cron/jobs/{id}/run", s.handleCronTriggerJob)
	mountAuthed(mux, "GET /operator/cron/status", s.handleCronStatus)
}

func (s *operatorCronSurface) handleCronListJobs(w http.ResponseWriter, r *http.Request) {
	if s.cron == nil {
		gwError(w, http.StatusServiceUnavailable, "cron service not available")
		return
	}
	jobs := s.cron.Store().List()
	gwJSON(w, http.StatusOK, cronJobListResponse{Items: jobs, Count: len(jobs)})
}

func (s *operatorCronSurface) handleCronCreateJob(w http.ResponseWriter, r *http.Request) {
	if s.cron == nil {
		gwError(w, http.StatusServiceUnavailable, "cron service not available")
		return
	}
	var req cronJobCreateRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Payload.Content) == "" {
		gwError(w, http.StatusBadRequest, "payload.content is required")
		return
	}

	now := time.Now().UTC()
	if _, err := cron.NextRunTime(req.Schedule, now); err != nil {
		gwError(w, http.StatusBadRequest, fmt.Sprintf("invalid schedule: %v", err))
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	id, err := generateCronID()
	if err != nil {
		gwError(w, http.StatusInternalServerError, "generate id failed")
		return
	}

	job := cron.Job{
		ID:           id,
		Name:         strings.TrimSpace(req.Name),
		Enabled:      enabled,
		Schedule:     req.Schedule,
		Payload:      req.Payload,
		Delivery:     req.Delivery,
		SessionKey:   strings.TrimSpace(req.SessionKey),
		Model:        strings.TrimSpace(req.Model),
		AutomationID: strings.TrimSpace(req.AutomationID),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	next, _ := cron.NextRunTime(req.Schedule, now)
	job.NextRunAt = next

	if err := s.cron.Store().Add(job); err != nil {
		gwError(w, http.StatusConflict, err.Error())
		return
	}
	if err := s.cron.Store().Save(); err != nil {
		gwError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %v", err))
		return
	}
	s.cron.Rearm()

	gwJSON(w, http.StatusCreated, cronJobResponse{Job: job})
}

func (s *operatorCronSurface) handleCronGetJob(w http.ResponseWriter, r *http.Request) {
	if s.cron == nil {
		gwError(w, http.StatusServiceUnavailable, "cron service not available")
		return
	}
	id := r.PathValue("id")
	job, err := s.cron.Store().Get(id)
	if err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	gwJSON(w, http.StatusOK, cronJobResponse{Job: *job})
}

func (s *operatorCronSurface) handleCronUpdateJob(w http.ResponseWriter, r *http.Request) {
	if s.cron == nil {
		gwError(w, http.StatusServiceUnavailable, "cron service not available")
		return
	}
	id := r.PathValue("id")
	var req cronJobPatchRequest
	if !decodeOperatorJSONBody(w, r, &req) {
		return
	}

	now := time.Now().UTC()
	current, err := s.cron.Store().Get(id)
	if err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	updated := *current
	if req.Name != nil {
		updated.Name = strings.TrimSpace(*req.Name)
	}
	if req.Enabled != nil {
		updated.Enabled = *req.Enabled
	}
	if req.Schedule != nil {
		updated.Schedule = *req.Schedule
		next, nextErr := cron.NextRunTime(*req.Schedule, now)
		if nextErr != nil {
			gwError(w, http.StatusBadRequest, fmt.Sprintf("invalid schedule: %v", nextErr))
			return
		}
		updated.NextRunAt = next
	}
	if req.Payload != nil {
		updated.Payload = *req.Payload
	}
	if req.Delivery != nil {
		updated.Delivery = req.Delivery
	}
	if req.SessionKey != nil {
		updated.SessionKey = strings.TrimSpace(*req.SessionKey)
	}
	if req.Model != nil {
		updated.Model = strings.TrimSpace(*req.Model)
	}
	if req.AutomationID != nil {
		updated.AutomationID = strings.TrimSpace(*req.AutomationID)
	}
	updated.UpdatedAt = now
	if updateErr := s.cron.Store().Update(id, func(j *cron.Job) {
		*j = updated
	}); updateErr != nil {
		gwError(w, http.StatusNotFound, updateErr.Error())
		return
	}
	if err := s.cron.Store().Save(); err != nil {
		gwError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %v", err))
		return
	}
	s.cron.Rearm()

	gwJSON(w, http.StatusOK, cronJobResponse{Job: updated})
}

func (s *operatorCronSurface) handleCronDeleteJob(w http.ResponseWriter, r *http.Request) {
	if s.cron == nil {
		gwError(w, http.StatusServiceUnavailable, "cron service not available")
		return
	}
	id := r.PathValue("id")
	if _, err := s.cron.Store().Get(id); err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := s.cron.Store().Remove(id); err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := s.cron.Store().Save(); err != nil {
		gwError(w, http.StatusInternalServerError, fmt.Sprintf("save failed: %v", err))
		return
	}
	s.cron.Rearm()

	gwJSON(w, http.StatusOK, idOKResponse{OK: true, ID: id})
}

func (s *operatorCronSurface) handleCronTriggerJob(w http.ResponseWriter, r *http.Request) {
	if s.cron == nil {
		gwError(w, http.StatusServiceUnavailable, "cron service not available")
		return
	}
	id := r.PathValue("id")
	if _, err := s.cron.Store().Get(id); err != nil {
		gwError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := s.cron.TriggerJob(r.Context(), id); err != nil {
		gwError(w, gatewayHTTPStatusForError(err, http.StatusInternalServerError), err.Error())
		return
	}
	gwJSON(w, http.StatusAccepted, idOKResponse{OK: true, ID: id})
}

func (s *operatorCronSurface) handleCronStatus(w http.ResponseWriter, r *http.Request) {
	if s.cron == nil {
		gwError(w, http.StatusServiceUnavailable, "cron service not available")
		return
	}
	jobs := s.cron.Store().List()
	gwJSON(w, http.StatusOK, cronServiceStatusResponse{
		Running:  s.cron.IsRunning(),
		JobCount: len(jobs),
	})
}

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"go.uber.org/zap"

	"github.com/Shisa-Fosho/services/internal/platform/data"
)

// fakeAuditRepo captures audit actions for assertions, with an optional
// failure injection.
type fakeAuditRepo struct {
	mu       sync.Mutex
	actions  []data.AdminAuditAction
	writeErr error
}

func (f *fakeAuditRepo) RecordAdminAction(_ context.Context, action *data.AdminAuditAction) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, *action)
	return nil
}

func (f *fakeAuditRepo) recorded() []data.AdminAuditAction {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]data.AdminAuditAction, len(f.actions))
	copy(out, f.actions)
	return out
}

// runAudit runs the audit middleware around handler with the given admin
// address in context and returns the response recorder + fake repo.
func runAudit(t *testing.T, adminAddr string, writeErr error, handler http.Handler, path string) (*httptest.ResponseRecorder, *fakeAuditRepo) {
	t.Helper()
	repo := &fakeAuditRepo{writeErr: writeErr}
	mw := AuditAdminAction(repo, zap.NewNop())(handler)

	req := httptest.NewRequest(http.MethodPost, path, nil)
	if adminAddr != "" {
		req = req.WithContext(WithAdminAddress(req.Context(), adminAddr))
	}
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)
	return rec, repo
}

func TestAuditAdminAction_Records2xx(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	rec, repo := runAudit(t, "0xadmin", nil, handler, "/admin/events/abc")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	got := repo.recorded()
	if len(got) != 1 {
		t.Fatalf("recorded %d actions, want 1", len(got))
	}
	if got[0].AdminAddress != "0xadmin" {
		t.Errorf("admin_address = %q, want 0xadmin", got[0].AdminAddress)
	}
	if got[0].Method != http.MethodPost {
		t.Errorf("method = %q, want POST", got[0].Method)
	}
	if got[0].Path != "/admin/events/abc" {
		t.Errorf("path = %q, want /admin/events/abc", got[0].Path)
	}
	if got[0].Status != http.StatusOK {
		t.Errorf("status = %d, want 200", got[0].Status)
	}
}

func TestAuditAdminAction_Skips4xx(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
	})
	_, repo := runAudit(t, "0xadmin", nil, handler, "/admin/markets/abc/pause")
	if len(repo.recorded()) != 0 {
		t.Errorf("expected no audit entries for 409, got %d", len(repo.recorded()))
	}
}

func TestAuditAdminAction_Skips5xx(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	_, repo := runAudit(t, "0xadmin", nil, handler, "/admin/events/abc")
	if len(repo.recorded()) != 0 {
		t.Errorf("expected no audit entries for 500, got %d", len(repo.recorded()))
	}
}

func TestAuditAdminAction_WriteFailureDoesNotAffectResponse(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rec, _ := runAudit(t, "0xadmin", errors.New("db down"), handler, "/admin/events/abc")
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 even with audit write failure", rec.Code)
	}
}

func TestAuditAdminAction_MissingAdminSkipsWrite(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	_, repo := runAudit(t, "", nil, handler, "/admin/events/abc")
	if len(repo.recorded()) != 0 {
		t.Errorf("expected no audit entries when admin address is missing, got %d", len(repo.recorded()))
	}
}

func TestAuditAdminAction_RecordsImplicit200(t *testing.T) {
	t.Parallel()
	// Handler that writes body without calling WriteHeader — stdlib implicitly
	// sets 200. Ensure the wrapper captures that too.
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	_, repo := runAudit(t, "0xadmin", nil, handler, "/admin/events/abc")
	if len(repo.recorded()) != 1 {
		t.Fatalf("recorded %d, want 1", len(repo.recorded()))
	}
	if repo.recorded()[0].Status != http.StatusOK {
		t.Errorf("status = %d, want 200 (implicit)", repo.recorded()[0].Status)
	}
}

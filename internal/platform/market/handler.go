package market

import (
	"errors"
	"net/http"

	"go.uber.org/zap"

	"github.com/Shisa-Fosho/services/internal/shared/httputil"
)

// Handler implements the platform service's market-domain HTTP endpoints.
// Admin-only mutation handlers live here; public read handlers will be added
// alongside them as P3 (Market API) lands.
type Handler struct {
	repo   Repository
	logger *zap.Logger
}

// NewHandler creates a new market handler.
func NewHandler(repo Repository, logger *zap.Logger) *Handler {
	return &Handler{repo: repo, logger: logger}
}

// RegisterAdminRoutes wires the admin-only category endpoints onto mux. The
// provided adminMiddleware is expected to stack JWT authentication and the
// admin-wallet check — see platformauth.Authenticate composed with
// platformauth.RequireAdmin in cmd/platform/main.go.
func (handler *Handler) RegisterAdminRoutes(mux *http.ServeMux, adminMiddleware func(http.Handler) http.Handler) {
	mux.Handle("POST /admin/categories", adminMiddleware(http.HandlerFunc(handler.createCategory)))
	mux.Handle("PUT /admin/categories/{id}", adminMiddleware(http.HandlerFunc(handler.updateCategory)))
	mux.Handle("DELETE /admin/categories/{id}", adminMiddleware(http.HandlerFunc(handler.deleteCategory)))
}

// categoryRequest is the shape both POST and PUT accept.
type categoryRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// categoryResponse is the JSON shape returned for a single category.
type categoryResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func toCategoryResponse(cat *Category) categoryResponse {
	return categoryResponse{ID: cat.ID, Name: cat.Name, Slug: cat.Slug}
}

func (handler *Handler) createCategory(w http.ResponseWriter, r *http.Request) {
	var req categoryRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name == "" || req.Slug == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "name and slug are required")
		return
	}

	cat := &Category{Name: req.Name, Slug: req.Slug}
	if err := handler.repo.CreateCategory(r.Context(), cat); err != nil {
		if errors.Is(err, ErrDuplicateSlug) {
			httputil.ErrorResponse(w, http.StatusConflict, "slug already in use")
			return
		}
		handler.logger.Error("creating category", zap.Error(err))
		httputil.ErrorResponse(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = httputil.EncodeJSON(w, http.StatusCreated, toCategoryResponse(cat))
}

func (handler *Handler) updateCategory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "id is required")
		return
	}

	var req categoryRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name == "" || req.Slug == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "name and slug are required")
		return
	}

	if err := handler.repo.UpdateCategory(r.Context(), id, req.Name, req.Slug); err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			httputil.ErrorResponse(w, http.StatusNotFound, "category not found")
		case errors.Is(err, ErrDuplicateSlug):
			httputil.ErrorResponse(w, http.StatusConflict, "slug already in use")
		default:
			handler.logger.Error("updating category", zap.Error(err), zap.String("id", id))
			httputil.ErrorResponse(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	updated, err := handler.repo.GetCategory(r.Context(), id)
	if err != nil {
		handler.logger.Error("fetching updated category", zap.Error(err), zap.String("id", id))
		httputil.ErrorResponse(w, http.StatusInternalServerError, "internal error")
		return
	}
	_ = httputil.EncodeJSON(w, http.StatusOK, toCategoryResponse(updated))
}

func (handler *Handler) deleteCategory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "id is required")
		return
	}

	if err := handler.repo.DeleteCategory(r.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			httputil.ErrorResponse(w, http.StatusNotFound, "category not found")
			return
		}
		handler.logger.Error("deleting category", zap.Error(err), zap.String("id", id))
		httputil.ErrorResponse(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

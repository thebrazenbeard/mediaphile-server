package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/thebrazenbeard/mediaphile-server/internal/auth"
	"github.com/thebrazenbeard/mediaphile-server/internal/catalog"
)

type principalContextKey struct{}

func principalFromRequest(r *http.Request) (catalog.Principal, bool) {
	p, ok := r.Context().Value(principalContextKey{}).(catalog.Principal)
	return p, ok
}

func requirePrincipal(deps Dependencies, admin bool, next http.Handler) http.Handler {
	if deps.Auth == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r.Header.Get("Authorization"))
		principal, err := deps.Auth.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "authentication required")
			return
		}
		if admin && !principal.Admin {
			writeError(w, http.StatusForbidden, "ADMIN_REQUIRED", "administrator permission required")
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}

func bootstrap(deps Dependencies) http.HandlerFunc {
	type request struct {
		BootstrapSecret string `json:"bootstrapSecret"`
		Username        string `json:"username"`
		Password        string `json:"password"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Auth == nil {
			writeError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication service is unavailable")
			return
		}
		var in request
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body is invalid")
			return
		}
		principal, token, err := deps.Auth.Bootstrap(r.Context(), in.BootstrapSecret, in.Username, in.Password)
		switch {
		case errors.Is(err, auth.ErrAlreadyInitialized):
			writeError(w, http.StatusConflict, "ALREADY_INITIALIZED", "server has already been initialized")
		case errors.Is(err, auth.ErrInvalidBootstrap):
			writeError(w, http.StatusUnauthorized, "INVALID_BOOTSTRAP", "bootstrap secret is invalid")
		case errors.Is(err, auth.ErrInvalidCredentials):
			writeError(w, http.StatusBadRequest, "INVALID_CREDENTIALS", "username and password are required")
		case err != nil:
			writeError(w, http.StatusInternalServerError, "AUTH_ERROR", "could not initialize server")
		default:
			writeJSON(w, http.StatusCreated, map[string]any{"token": token, "user": map[string]any{"id": principal.ID, "username": principal.Username, "admin": principal.Admin}})
		}
	}
}

func login(deps Dependencies) http.HandlerFunc {
	type request struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Auth == nil {
			writeError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication service is unavailable")
			return
		}
		var in request
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body is invalid")
			return
		}
		principal, token, err := deps.Auth.Login(r.Context(), in.Username, in.Password)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "username or password is invalid")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": map[string]any{"id": principal.ID, "username": principal.Username, "admin": principal.Admin}})
	}
}

func logout(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Auth == nil {
			writeError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication service is unavailable")
			return
		}
		if err := deps.Auth.Logout(r.Context(), bearerToken(r.Header.Get("Authorization"))); err != nil {
			writeError(w, http.StatusUnauthorized, "INVALID_SESSION", "session is invalid")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

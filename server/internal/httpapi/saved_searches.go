package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/go-chi/chi/v5"
)

func savedSearchStore(searchers ...posts.Searcher) (posts.SavedSearchStore, bool) {
	if len(searchers) == 0 || searchers[0] == nil {
		return nil, false
	}
	store, ok := searchers[0].(posts.SavedSearchStore)
	return store, ok
}

func savedSearchActor(r *http.Request) (string, bool) {
	actor, ok := actorFromRequest(r)
	return strings.TrimSpace(actor.ID), ok && strings.TrimSpace(actor.ID) != ""
}

func listSavedSearches(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := savedSearchActor(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		store, ok := savedSearchStore(searchers...)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "saved searches unavailable")
			return
		}
		result, err := store.ListSavedSearches(r.Context(), actor)
		if err != nil {
			writeSavedSearchError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			SavedSearches []posts.SavedSearch `json:"saved_searches"`
		}{SavedSearches: result})
	}
}

func createSavedSearch(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := savedSearchActor(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		store, ok := savedSearchStore(searchers...)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "saved searches unavailable")
			return
		}
		var request posts.CreateSavedSearchRequest
		if err := decodeJSONBody(w, r, &request, "saved search"); err != nil {
			return
		}
		validated, err := posts.ValidateCreateSavedSearch(request)
		if err != nil {
			writeSavedSearchError(w, err)
			return
		}
		result, err := store.CreateSavedSearch(r.Context(), actor, validated)
		if err != nil {
			writeSavedSearchError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func updateSavedSearch(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := savedSearchActor(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		store, ok := savedSearchStore(searchers...)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "saved searches unavailable")
			return
		}
		var request posts.UpdateSavedSearchRequest
		if err := decodeJSONBody(w, r, &request, "saved search"); err != nil {
			return
		}
		validated, err := posts.ValidateUpdateSavedSearch(request)
		if err != nil {
			writeSavedSearchError(w, err)
			return
		}
		result, err := store.UpdateSavedSearch(r.Context(), actor, chi.URLParam(r, "id"), validated)
		if err != nil {
			writeSavedSearchError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func deleteSavedSearch(searchers ...posts.Searcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := savedSearchActor(r)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		store, ok := savedSearchStore(searchers...)
		if !ok {
			writeJSONError(w, http.StatusServiceUnavailable, "saved searches unavailable")
			return
		}
		if err := store.DeleteSavedSearch(r.Context(), actor, chi.URLParam(r, "id")); err != nil {
			writeSavedSearchError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeSavedSearchError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, posts.ErrInvalidSavedSearch), errors.Is(err, posts.ErrInvalidQuery):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, posts.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "saved search not found")
	case errors.Is(err, posts.ErrUnavailable):
		writeJSONError(w, http.StatusServiceUnavailable, "saved searches unavailable")
	default:
		writeJSONError(w, http.StatusInternalServerError, "saved search operation failed")
	}
}

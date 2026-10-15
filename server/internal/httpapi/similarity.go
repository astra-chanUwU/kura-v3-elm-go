package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/auth"
	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
	"github.com/go-chi/chi/v5"
)

// similarityActor resolves the capability owner for the protected
// similarity controls through the identity contract. The similarity
// status read is a GET, which the deployment write middleware leaves
// public, so the status handler checks this explicitly; the index POST
// checks it too so the requirement never depends on middleware alone.
func similarityActor(r *http.Request) (string, bool) {
	actor, ok := actorFromRequest(r)
	if !ok || strings.TrimSpace(actor.ID) == "" {
		return "", false
	}
	if !auth.HasWriteCapability(actor) {
		return "", false
	}
	return strings.TrimSpace(actor.ID), true
}

// getSimilarPosts serves the frozen public near-copy read.
// Coordinator route:
// api.Get("/posts/{id}/similar", getSimilarPosts(deps.Similarity)).
func getSimilarPosts(reader posts.SimilarityReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(chi.URLParam(r, "id"))
		if parsed, err := strconv.ParseInt(id, 10, 64); err != nil || parsed <= 0 || id != strconv.FormatInt(parsed, 10) {
			writeJSONError(w, http.StatusNotFound, "post not found")
			return
		}
		if reader == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "similarity unavailable")
			return
		}
		query := r.URL.Query()
		distance, err := parseSimilarDistance(query.Get("distance"), query.Has("distance"))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "distance must be an integer from 0 to 16")
			return
		}
		limit, err := parseSimilarLimit(query.Get("limit"), query.Has("limit"))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "limit must be an integer from 1 to 60")
			return
		}
		cursor := ""
		if query.Has("cursor") {
			cursor = query.Get("cursor")
			if strings.TrimSpace(cursor) == "" {
				writeJSONError(w, http.StatusBadRequest, "invalid similar cursor")
				return
			}
		}
		page, err := reader.ListSimilarPosts(r.Context(), id, distance, limit, cursor)
		if err != nil {
			switch {
			case errors.Is(err, posts.ErrNotFound):
				writeJSONError(w, http.StatusNotFound, "post not found")
			case errors.Is(err, posts.ErrInvalidCursor):
				writeJSONError(w, http.StatusBadRequest, "invalid similar cursor")
			case errors.Is(err, posts.ErrInvalidQuery):
				writeJSONError(w, http.StatusBadRequest, "invalid similar request")
			case errors.Is(err, posts.ErrUnavailable):
				writeJSONError(w, http.StatusServiceUnavailable, "similarity unavailable")
			default:
				writeJSONError(w, http.StatusInternalServerError, "similar lookup failed")
			}
			return
		}
		if page.Matches == nil {
			page.Matches = []posts.SimilarMatch{}
		}
		writeJSON(w, http.StatusOK, page)
	}
}

type similarityIndexHTTPRequest struct {
	PostIDs []string `json:"post_ids"`
}

// postSimilarityIndex enqueues fingerprint jobs for the protected batch.
// Coordinator route:
// api.Post("/posts/similarity/index", postSimilarityIndex(deps.SimilarityIndex)).
func postSimilarityIndex(indexer posts.SimilarityIndexer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := similarityActor(r); !ok {
			writeJSONError(w, http.StatusUnauthorized, "similarity capability required")
			return
		}
		if indexer == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "similarity unavailable")
			return
		}
		var request similarityIndexHTTPRequest
		if err := decodeJSONBody(w, r, &request, "similarity index"); err != nil {
			return
		}
		if _, err := posts.ValidateSimilarityIndexIDs(request.PostIDs); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
		response, err := indexer.IndexSimilarityPosts(r.Context(), request.PostIDs)
		if err != nil {
			switch {
			case errors.Is(err, posts.ErrNotFound):
				writeJSONError(w, http.StatusNotFound, "post not found")
			case errors.Is(err, posts.ErrInvalidQuery):
				writeJSONError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, posts.ErrUnavailable):
				writeJSONError(w, http.StatusServiceUnavailable, "similarity unavailable")
			default:
				writeJSONError(w, http.StatusInternalServerError, "similarity index failed")
			}
			return
		}
		if response.Posts == nil {
			response.Posts = []posts.SimilarityIndexResult{}
		}
		writeJSON(w, http.StatusOK, response)
	}
}

// getSimilarityStatus serves the protected paged index state used by the
// explicit index tool to queue missing posts. It carries no media URLs
// and no hashes.
// Coordinator route:
// api.Get("/posts/similarity/status", getSimilarityStatus(deps.SimilarityIndex)).
func getSimilarityStatus(indexer posts.SimilarityIndexer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := similarityActor(r); !ok {
			writeJSONError(w, http.StatusUnauthorized, "similarity capability required")
			return
		}
		if indexer == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "similarity unavailable")
			return
		}
		query := r.URL.Query()
		limit, err := parseSimilarityStatusLimit(query.Get("limit"), query.Has("limit"))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "limit must be an integer from 1 to 100")
			return
		}
		cursor := ""
		if query.Has("cursor") {
			cursor = query.Get("cursor")
			if strings.TrimSpace(cursor) == "" {
				writeJSONError(w, http.StatusBadRequest, "invalid similarity status cursor")
				return
			}
		}
		page, err := indexer.GetSimilarityStatus(r.Context(), limit, cursor)
		if err != nil {
			switch {
			case errors.Is(err, posts.ErrInvalidCursor):
				writeJSONError(w, http.StatusBadRequest, "invalid similarity status cursor")
			case errors.Is(err, posts.ErrInvalidQuery):
				writeJSONError(w, http.StatusBadRequest, "invalid similarity status request")
			case errors.Is(err, posts.ErrUnavailable):
				writeJSONError(w, http.StatusServiceUnavailable, "similarity unavailable")
			default:
				writeJSONError(w, http.StatusInternalServerError, "similarity status failed")
			}
			return
		}
		if page.Posts == nil {
			page.Posts = []posts.SimilarityStatusItem{}
		}
		writeJSON(w, http.StatusOK, page)
	}
}

func parseSimilarDistance(raw string, present bool) (int, error) {
	if !present {
		return posts.DefaultSimilarDistance, nil
	}
	if raw == "" {
		return 0, errors.New("invalid distance")
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid distance")
		}
	}
	distance, err := strconv.Atoi(raw)
	if err != nil || distance < 0 || distance > posts.MaxSimilarDistance {
		return 0, errors.New("invalid distance")
	}
	return distance, nil
}

func parseSimilarLimit(raw string, present bool) (int, error) {
	if !present {
		return posts.DefaultSimilarLimit, nil
	}
	if raw == "" {
		return 0, errors.New("invalid limit")
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid limit")
		}
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > posts.MaxSimilarLimit {
		return 0, errors.New("invalid limit")
	}
	return limit, nil
}

func parseSimilarityStatusLimit(raw string, present bool) (int, error) {
	if !present {
		return posts.DefaultSimilarityStatusLimit, nil
	}
	if raw == "" {
		return 0, errors.New("invalid limit")
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, errors.New("invalid limit")
		}
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > posts.MaxSimilarityStatusLimit {
		return 0, errors.New("invalid limit")
	}
	return limit, nil
}

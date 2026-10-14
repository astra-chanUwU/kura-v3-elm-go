package client

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

// RatingTarget uses an explicitly supplied, independent rating version.
type RatingTarget struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type RatingRequest struct {
	Posts  []RatingTarget `json:"posts"`
	Rating string         `json:"rating"`
}

type RatingOutcome struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	Rating  string `json:"rating"`
	Changed bool   `json:"changed"`
}

type RatingResponse struct {
	Posts []RatingOutcome `json:"posts"`
}

type RatingRevision struct {
	Version        int    `json:"version"`
	PreviousRating string `json:"previous_rating"`
	Rating         string `json:"rating"`
	CreatedAt      string `json:"created_at"`
}

func ValidRating(value string) bool {
	switch value {
	case "unrated", "safe", "questionable", "explicit":
		return true
	default:
		return false
	}
}

// EditRatings never retries a write. Its receipt must account for exactly the
// submitted targets and their independent versions before a save is confirmed.
func (c *Client) EditRatings(ctx context.Context, request RatingRequest) (RatingResponse, error) {
	if !ValidRating(request.Rating) || len(request.Posts) < 1 || len(request.Posts) > 100 {
		return RatingResponse{}, fmt.Errorf("rating edit requires 1..100 posts and unrated, safe, questionable, or explicit")
	}
	expected := make(map[string]int, len(request.Posts))
	for _, target := range request.Posts {
		id, err := strconv.ParseInt(target.ID, 10, 64)
		_, duplicate := expected[target.ID]
		if err != nil || id < 1 || strconv.FormatInt(id, 10) != target.ID || target.Version < 0 || target.Version > 2147483647 || duplicate {
			return RatingResponse{}, fmt.Errorf("rating targets require unique positive IDs and non-negative versions")
		}
		expected[target.ID] = target.Version
	}
	var receipt struct {
		Posts []struct {
			ID      *string `json:"id"`
			Version *int    `json:"version"`
			Rating  *string `json:"rating"`
			Changed *bool   `json:"changed"`
		} `json:"posts"`
	}
	if err := c.postJSON(ctx, "/api/posts/ratings", request, &receipt); err != nil {
		var apiError *APIError
		if errors.As(err, &apiError) && apiError.Status >= 500 {
			return RatingResponse{}, &RequestError{Err: err, Uncertain: true}
		}
		return RatingResponse{}, err
	}
	invalidReceipt := func() (RatingResponse, error) {
		return RatingResponse{}, &RequestError{Err: fmt.Errorf("invalid rating acknowledgement; check the posts before retrying"), Uncertain: true}
	}
	if len(receipt.Posts) != len(expected) {
		return invalidReceipt()
	}
	result := RatingResponse{Posts: make([]RatingOutcome, 0, len(receipt.Posts))}
	for _, item := range receipt.Posts {
		if item.ID == nil || item.Version == nil || item.Rating == nil || item.Changed == nil {
			return invalidReceipt()
		}
		base, submitted := expected[*item.ID]
		if !submitted || *item.Rating != request.Rating || *item.Version < 0 || *item.Version > 2147483647 {
			return invalidReceipt()
		}
		wanted := base
		if *item.Changed {
			wanted++
		}
		if *item.Version != wanted {
			return invalidReceipt()
		}
		delete(expected, *item.ID)
		result.Posts = append(result.Posts, RatingOutcome{ID: *item.ID, Version: *item.Version, Rating: *item.Rating, Changed: *item.Changed})
	}
	return result, nil
}

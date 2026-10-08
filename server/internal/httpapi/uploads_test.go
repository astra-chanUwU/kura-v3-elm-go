package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/astra-chanUwU/kura-v3-elm-go/server/internal/posts"
)

type fakeUploader struct {
	received posts.UploadRequest
}

func (f *fakeUploader) SearchPosts(context.Context, string, *posts.Cursor, int) (posts.SearchPage, error) {
	return posts.SearchPage{}, nil
}

func (f *fakeUploader) CreateUpload(_ context.Context, request posts.UploadRequest) (posts.PostDetail, error) {
	f.received = request
	return posts.PostDetail{PostSummary: posts.PostSummary{ID: "9001", MediaType: "image/jpeg", Width: 2, Height: 2}}, nil
}

func TestUploadRoutePassesMultipartMetadata(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "../sample.jpg")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("image bytes"))
	_ = writer.WriteField("tags", "cat, demo")
	_ = writer.WriteField("tags", "portrait")
	_ = writer.WriteField("source", "imports/sample.jpg")
	_ = writer.WriteField("artist", "Kura")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	fake := &fakeUploader{}
	request := httptest.NewRequest(http.MethodPost, "/api/uploads", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	NewRouter(fake).ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var detail posts.PostDetail
	if err := json.NewDecoder(response.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	if detail.ID != "9001" || fake.received.Filename != "sample.jpg" || len(fake.received.Tags) != 2 || fake.received.Source != "imports/sample.jpg" || fake.received.Artist != "Kura" {
		t.Fatalf("unexpected upload: %#v", fake.received)
	}
}

func TestUploadRouteRequiresFile(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/uploads", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	NewRouter(&fakeUploader{}).ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing file status=%d body=%s", response.Code, response.Body.String())
	}
}

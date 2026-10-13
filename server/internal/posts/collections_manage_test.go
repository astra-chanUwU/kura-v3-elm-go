package posts

import (
	"context"
	"errors"
	"testing"
)

func TestValidateCollectionManageRequest(t *testing.T) {
	request, err := ValidateCollectionManageRequest(CollectionManageRequest{Name: "  Archive  ", ExpectedVersion: 0}, true)
	if err != nil || request.Name != "Archive" || request.ExpectedVersion != 0 {
		t.Fatalf("validated=%#v err=%v", request, err)
	}
	for _, request := range []CollectionManageRequest{{Name: "x", ExpectedVersion: -1}, {Name: "  ", ExpectedVersion: 0}, {Name: string(make([]byte, 0)), ExpectedVersion: 0}} {
		if _, err := ValidateCollectionManageRequest(request, true); err == nil {
			t.Fatalf("expected invalid request: %#v", request)
		}
	}
	tooLong := make([]rune, maxCollectionNameLength+1)
	for i := range tooLong {
		tooLong[i] = 'x'
	}
	if _, err := ValidateCollectionManageRequest(CollectionManageRequest{Name: string(tooLong)}, true); !errors.Is(err, ErrInvalidCollections) {
		t.Fatalf("long name error=%v", err)
	}
	if _, err := ValidateCollectionManageRequest(CollectionManageRequest{ExpectedVersion: -1}, false); !errors.Is(err, ErrInvalidCollections) {
		t.Fatalf("negative delete version error=%v", err)
	}
}

func TestCollectionManagerUnavailableBackend(t *testing.T) {
	var backend *PostgresBackend
	if _, err := backend.RenameCollection(context.Background(), "1", CollectionManageRequest{Name: "x"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("rename error=%v", err)
	}
	if _, err := backend.DeleteCollection(context.Background(), "1", 0); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("delete error=%v", err)
	}
}

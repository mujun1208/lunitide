package attachmentapp

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestUploadWaitersRespectCancellationAndPreserveCommit(t *testing.T) {
	store := newBlockingCreateStore()
	service := NewService(store, NewDirFileStorage(t.TempDir()))
	service.uploadDir = t.TempDir()
	id, request := prepareConcurrentUpload(t, service, []byte("preserve persisted attachment"))
	committed := make(chan error, 1)
	go func() {
		_, err := service.CommitUpload(context.Background(), id, request.ProjectID, request.SessionID)
		committed <- err
	}()
	<-store.started
	defer func() {
		close(store.release)
		if err := <-committed; err != nil {
			t.Error(err)
		}
	}()
	for _, operation := range []string{"commit", "abort"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() {
				if operation == "commit" {
					_, err := service.CommitUpload(ctx, id, request.ProjectID, request.SessionID)
					done <- err
				} else {
					done <- service.AbortUpload(ctx, id, request.ProjectID, request.SessionID)
				}
			}()
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("upload waiter ignored cancellation")
			}
		})
	}
	if store.createCount() != 1 {
		t.Fatal("cancelled waiter started a duplicate commit")
	}
}

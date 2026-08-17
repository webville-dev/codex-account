package store_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/webville-dev/codex-account/internal/store"
)

func TestLockExcludesConcurrentHolders(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + "/.lock"
	ctx := context.Background()
	unlock, err := store.Lock(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	blocked := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		_, err := store.Lock(ctx, path)
		blocked <- err
	}()
	if err := <-blocked; err == nil {
		t.Fatal("second lock should wait and then fail")
	}
	unlock()
	wg.Wait()

	unlock2, err := store.Lock(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	unlock2()
}

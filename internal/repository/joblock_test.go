package repository_test

import (
	"context"
	"errors"
	"testing"
)

// Two replicas are two processes holding separate connections, so the lock has
// to be exclusive across connections rather than within a process. Nesting the
// second attempt inside the first is the closest a single test can get to that,
// and it is the case that matters: while one instance is working, no other may
// start the same job.
func TestWithJobLockExcludesASecondHolder(t *testing.T) {
	repo, _ := rawPool(t)
	ctx := context.Background()

	const key = int64(9_990_101)

	var innerRan bool
	outerRan, err := repo.WithJobLock(ctx, key, func(ctx context.Context) error {
		ran, err := repo.WithJobLock(ctx, key, func(context.Context) error {
			innerRan = true
			return nil
		})
		if err != nil {
			return err
		}
		if ran {
			return errors.New("a second holder acquired a lock that was already held")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("outer: %v", err)
	}
	if !outerRan {
		t.Fatal("the first caller must run")
	}
	if innerRan {
		t.Fatal("the job ran twice while the lock was held")
	}
}

// Releasing matters as much as taking: a lock held past its tick would stop the
// job running ever again, on any replica, until the process restarted.
func TestWithJobLockReleasesForTheNextTick(t *testing.T) {
	repo, _ := rawPool(t)
	ctx := context.Background()

	const key = int64(9_990_102)

	for i := 0; i < 3; i++ {
		ran, err := repo.WithJobLock(ctx, key, func(context.Context) error { return nil })
		if err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
		if !ran {
			t.Fatalf("tick %d did not run; the previous tick left the lock held", i)
		}
	}
}

// A failing job must still release, or one bad tick disables the job forever.
func TestWithJobLockReleasesAfterFailure(t *testing.T) {
	repo, _ := rawPool(t)
	ctx := context.Background()

	const key = int64(9_990_103)
	sentinel := errors.New("job failed")

	ran, err := repo.WithJobLock(ctx, key, func(context.Context) error { return sentinel })
	if !ran {
		t.Fatal("expected the job to run")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithJobLock must surface the job's error, got %v", err)
	}

	ran, err = repo.WithJobLock(ctx, key, func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("second attempt: %v", err)
	}
	if !ran {
		t.Fatal("the lock was not released after the job returned an error")
	}
}

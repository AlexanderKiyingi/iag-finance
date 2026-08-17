package repository

import (
	"context"
	"fmt"
)

// Advisory lock keys for background jobs that must run on exactly one instance.
//
// Every replica of this service starts every worker, so without a lock each
// tick happens once per replica. For jobs whose effect is external — an email,
// a published event, an invoice — that is not wasted work but duplicated
// consequence, and the second replica is the moment it starts happening.
//
// Keys are arbitrary but must stay stable and distinct; a collision would make
// two unrelated jobs take turns instead of running concurrently. They share a
// namespace with the migration lock, which uses a two-int key.
const (
	JobLockOverdueNotifier int64 = 8_140_001
	JobLockOutboxRelay     int64 = 8_140_002
)

// WithJobLock runs fn only if this instance wins the named advisory lock, and
// reports whether it ran. A instance that does not win returns (false, nil) —
// that is the normal, expected outcome on every replica but one, not an error.
//
// The lock is session-scoped, so it is taken on a dedicated connection held for
// the duration of fn and released explicitly. Postgres drops it automatically
// if the process dies, which is what makes this safe against a replica that
// disappears mid-tick: the next tick on another replica simply acquires it.
//
// fn should be a single pass of work, not a loop — the lock is held for exactly
// as long as fn runs, and a long hold is a job no other replica can take over.
func (r *Repository) WithJobLock(ctx context.Context, key int64, fn func(context.Context) error) (bool, error) {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("joblock: acquire connection: %w", err)
	}
	defer conn.Release()

	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, key).Scan(&acquired); err != nil {
		return false, fmt.Errorf("joblock: try lock %d: %w", key, err)
	}
	if !acquired {
		return false, nil
	}
	defer func() {
		// Release on the same connection that took it; a different one holds no
		// claim to this lock. Detached from ctx so an already-cancelled tick
		// still unlocks rather than leaving the lock held.
		if _, err := conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, key); err != nil {
			// The lock is session-scoped and the pool does not reset session
			// state between users, so returning this connection would hand the
			// lock to whoever gets it next and block the job forever. Close the
			// underlying connection instead: the pool discards it, the session
			// ends, and Postgres drops the lock with it.
			_ = conn.Conn().Close(context.WithoutCancel(ctx))
		}
	}()

	return true, fn(ctx)
}

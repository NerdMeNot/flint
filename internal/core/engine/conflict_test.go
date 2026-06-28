package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryOnConflict(t *testing.T) {
	ctx := context.Background()
	deadlock := &pgconn.PgError{Code: "40P01"}

	t.Run("retries then succeeds", func(t *testing.T) {
		calls := 0
		err := retryOnConflict(ctx, func() error {
			calls++
			if calls < 2 {
				return deadlock
			}
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, 2, calls)
	})

	t.Run("gives up after conflictRetries on persistent conflict", func(t *testing.T) {
		calls := 0
		err := retryOnConflict(ctx, func() error { calls++; return deadlock })
		require.Error(t, err)
		assert.Equal(t, conflictRetries, calls)
	})

	t.Run("passes non-conflict errors straight through", func(t *testing.T) {
		calls := 0
		boom := errors.New("boom")
		err := retryOnConflict(ctx, func() error { calls++; return boom })
		assert.Equal(t, boom, err)
		assert.Equal(t, 1, calls)
	})

	t.Run("serialization failure is retryable", func(t *testing.T) {
		assert.True(t, isSerializationConflict(&pgconn.PgError{Code: "40001"}))
		assert.True(t, isSerializationConflict(deadlock))
		assert.False(t, isSerializationConflict(errors.New("nope")))
	})
}

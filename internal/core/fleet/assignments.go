package fleet

import (
	"context"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// GetAssignment loads one assignment row (stream binding, cancel watching).
func (f *Fleet) GetAssignment(ctx context.Context, id string) (db.StepAssignment, error) {
	return db.New(f.pool).GetAssignment(ctx, id)
}

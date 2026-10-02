// Package groups owns membership, roles and membership epochs (developer 1).
package groups

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// Access is the boundary used by messaging (developer 2). Authorization and message
// persistence must share a database transaction and group lock to prevent a removal/send race.
// The implementation must not treat a cached membership check as sufficient.
type Access interface {
	CanSend(ctx context.Context, tx pgx.Tx, groupID, userID string, epoch int64) error
}

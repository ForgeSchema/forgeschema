package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/ForgeSchema/forgeschema/schema"
)

type VerifyResult struct {
	Status              string              `json:"status"`
	Schema              string              `json:"schema"`
	ExpectedFingerprint string              `json:"expected_fingerprint"`
	ActualFingerprint   string              `json:"actual_fingerprint"`
	Differences         []schema.Difference `json:"differences"`
}

// Verify checks one complete supported managed schema in the inspector's
// read-only repeatable-read snapshot. It reads no application rows or history,
// performs no repairs, and says nothing about changes after the snapshot.
// Callers own db and should supply a context deadline.
func Verify(ctx context.Context, db *sql.DB, expected schema.Document) (VerifyResult, error) {
	empty := VerifyResult{}
	target, err := schema.Normalize(expected)
	if err != nil {
		return empty, err
	}
	if db == nil {
		return empty, fmt.Errorf("database is required")
	}
	actual, err := Inspect(ctx, db, target.Schema)
	if err != nil {
		return empty, err
	}
	differences, err := schema.Compare(target, actual)
	if err != nil {
		return empty, err
	}
	e, err := target.Fingerprint()
	if err != nil {
		return empty, err
	}
	a, err := actual.Fingerprint()
	if err != nil {
		return empty, err
	}
	status := "match"
	if len(differences) > 0 {
		status = "drift"
	}
	return VerifyResult{Status: status, Schema: target.Schema, ExpectedFingerprint: e, ActualFingerprint: a, Differences: differences}, nil
}

package evidence

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// store_gorm.go is the durable, real evidence Store. It keeps the exact receipt
// bytes (the `data` column holds the full JSON Evidence) alongside indexed
// columns for querying. Because the receipt is verified by recomputing its hash
// from canonical content, round-tripping through JSON is safe and lossless.
//
// The chain is append-only: there is intentionally no Update/Delete method.

// evidenceRow is the GORM model backing the evidence_records table. It is private
// to this package; callers work with *Evidence.
type evidenceRow struct {
	Seq       uint64    `gorm:"primaryKey;autoIncrement:false" json:"seq"`
	ID        string    `gorm:"type:varchar(64);uniqueIndex;not null" json:"id"`
	Action    string    `gorm:"size:128;index" json:"action"`
	Subject   string    `gorm:"size:256;index" json:"subject"`
	Actor     string    `gorm:"size:128;index" json:"actor"`
	Hash      string    `gorm:"size:64;index" json:"hash"`
	PrevHash  string    `gorm:"size:64" json:"prev_hash"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
	Data      string    `gorm:"type:text;not null" json:"data"` // full Evidence JSON
}

func (evidenceRow) TableName() string { return "evidence_records" }

// GORMStore is a durable Store backed by the platform's GORM DB (PostgreSQL, or
// SQLite in tests). It runs its own AutoMigrate so it does not couple to pkg/store.
type GORMStore struct {
	db *gorm.DB
}

// NewGORMStore opens a durable store on db and ensures the schema exists.
func NewGORMStore(db *gorm.DB) (*GORMStore, error) {
	if db == nil {
		return nil, fmt.Errorf("evidence: GORMStore requires a *gorm.DB")
	}
	if err := db.AutoMigrate(&evidenceRow{}); err != nil {
		return nil, fmt.Errorf("evidence: auto-migrate evidence_records: %w", err)
	}
	return &GORMStore{db: db}, nil
}

func toRow(e *Evidence) (*evidenceRow, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("evidence: marshal record: %w", err)
	}
	return &evidenceRow{
		Seq:       e.Seq,
		ID:        e.ID,
		Action:    e.Action,
		Subject:   e.Subject,
		Actor:     e.Actor,
		Hash:      e.Hash,
		PrevHash:  e.PrevHash,
		CreatedAt: e.Timestamp,
		Data:      string(data),
	}, nil
}

func fromRow(r *evidenceRow) (*Evidence, error) {
	var e Evidence
	if err := json.Unmarshal([]byte(r.Data), &e); err != nil {
		return nil, fmt.Errorf("evidence: unmarshal record %s: %w", r.ID, err)
	}
	return &e, nil
}

// Append inserts a new record. Duplicate Seq (unique PK) surfaces as an error.
func (g *GORMStore) Append(ctx context.Context, e *Evidence) error {
	row, err := toRow(e)
	if err != nil {
		return err
	}
	return g.db.WithContext(ctx).Create(row).Error
}

// AppendChained reads the chain head and inserts the built record inside a single
// transaction, so Seq/PrevHash stay consistent under concurrent writers. If two
// writers race for the same Seq, the loser hits the unique PK constraint and the
// whole build is retried against the new head.
func (g *GORMStore) AppendChained(ctx context.Context, build ChainedBuilder) (*Evidence, error) {
	const maxAttempts = 8
	var result *Evidence
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err := g.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var row evidenceRow
			ferr := tx.Order("seq DESC").First(&row).Error
			var last *Evidence
			if ferr == nil {
				l, uerr := fromRow(&row)
				if uerr != nil {
					return uerr
				}
				last = l
			} else if ferr != gorm.ErrRecordNotFound {
				return ferr
			}
			rec, berr := build(last)
			if berr != nil {
				return berr
			}
			newRow, berr := toRow(rec)
			if berr != nil {
				return berr
			}
			if cerr := tx.Create(newRow).Error; cerr != nil {
				return cerr
			}
			result = rec
			return nil
		})
		if err == nil {
			return result, nil
		}
		if isSeqContention(err) {
			continue // another writer took our Seq (or the DB was momentarily locked); rebuild
		}
		return nil, err
	}
	return nil, fmt.Errorf("evidence: append aborted after %d contended attempts", maxAttempts)
}

// isSeqContention reports whether err is a retryable write conflict: a unique
// constraint violation on the Seq PK (Postgres 23505 / SQLite "UNIQUE constraint
// failed") from two writers racing for the same Seq, OR transient DB-lock/busy
// contention (SQLite "database is locked" / "table is locked" / SQLITE_BUSY).
// All of these mean "retry against the new head", not a hard failure.
func isSeqContention(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "unique") ||
		strings.Contains(s, "duplicate") ||
		strings.Contains(s, "23505") ||
		strings.Contains(s, "constraint failed") ||
		strings.Contains(s, "database is locked") ||
		strings.Contains(s, "table is locked") ||
		strings.Contains(s, "database table is locked") ||
		strings.Contains(s, "busy")
}

// Last returns the highest-Seq record, or (nil, nil) when the chain is empty.
func (g *GORMStore) Last(ctx context.Context) (*Evidence, error) {
	var row evidenceRow
	err := g.db.WithContext(ctx).Order("seq DESC").First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return fromRow(&row)
}

// Get returns the record with id, or (nil, nil) when not found.
func (g *GORMStore) Get(ctx context.Context, id string) (*Evidence, error) {
	var row evidenceRow
	err := g.db.WithContext(ctx).Where("id = ?", id).First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return fromRow(&row)
}

// List returns matching records newest-first, bounded by f.Limit (default 100).
func (g *GORMStore) List(ctx context.Context, f Filter) ([]*Evidence, error) {
	q := g.db.WithContext(ctx).Model(&evidenceRow{})
	if f.Action != "" {
		q = q.Where("action = ?", f.Action)
	}
	if f.Subject != "" {
		q = q.Where("subject = ?", f.Subject)
	}
	if f.Actor != "" {
		q = q.Where("actor = ?", f.Actor)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	var rows []evidenceRow
	if err := q.Order("seq DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rowsToEvidence(rows)
}

// All returns the full chain in ascending Seq order (for export/verification).
func (g *GORMStore) All(ctx context.Context) ([]*Evidence, error) {
	var rows []evidenceRow
	if err := g.db.WithContext(ctx).Order("seq ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rowsToEvidence(rows)
}

// Count returns the number of stored records.
func (g *GORMStore) Count(ctx context.Context) (int64, error) {
	var n int64
	err := g.db.WithContext(ctx).Model(&evidenceRow{}).Count(&n).Error
	return n, err
}

func rowsToEvidence(rows []evidenceRow) ([]*Evidence, error) {
	out := make([]*Evidence, 0, len(rows))
	for i := range rows {
		e, err := fromRow(&rows[i])
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	_ "github.com/mattn/go-sqlite3"
	"github.com/spf13/cobra"
)

type evidenceRow struct {
	ID        string    `json:"id"`
	Seq       uint64    `json:"seq"`
	Action    string    `json:"action"`
	Subject   string    `json:"subject"`
	Actor     string    `json:"actor"`
	PrevHash  string    `json:"prev_hash"`
	CreatedAt time.Time `json:"created_at"`
	Data      string    `json:"data"`
	Signature string    `json:"signature"`
	KeyID     string    `json:"key_id"`
}

func newMigrateTenantsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate-tenants <db_path> <default_tenant>",
		Short: "Migrate evidence records to include tenant_id",
		Long: `Migrate existing evidence records to include tenant_id field.

This command migrates all NULL tenant_id values in the evidence_records table
to a specified default tenant, while recomputing hashes to maintain chain integrity.

Usage example:
  cafctl migrate-tenants /path/to/evidence.db default`,

		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return fmt.Errorf("usage: migrate-tenants <db_path> <default_tenant>")
			}

			dbPath := strings.TrimSpace(args[0])
			defaultTenant := strings.TrimSpace(args[1])

			if defaultTenant == "" {
				defaultTenant = "default"
			}

			fmt.Printf("Migrating evidence records to tenant_id='%s'\n", defaultTenant)

			if err := migrateTenants(dbPath, defaultTenant); err != nil {
				return fmt.Errorf("migration failed: %w", err)
			}

			fmt.Printf("Migration completed successfully!\n")
			return nil
		},
	}

	return cmd
}

func migrateTenants(dbPath, defaultTenant string) error {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('evidence_records') WHERE name='tenant_id'`).Scan(&count)
	if err != nil {
		return fmt.Errorf("check tenant_id column: %w", err)
	}

	if count == 0 {
		return fmt.Errorf("tenant_id column does not exist in evidence_records table")
	}

	rows, err := db.Query(`SELECT id, seq, prev_hash, created_at, data, signature, key_id FROM evidence_records WHERE tenant_id IS NULL AND seq > 0 ORDER BY seq ASC`)
	if err != nil {
		return fmt.Errorf("query null tenant records: %w", err)
	}
	defer rows.Close()

	batchSize := 100
	batch := make([]struct {
		id      string
		record  *evidence.Evidence
		newHash string
	}, 0, batchSize)

	for rows.Next() {
		var row evidenceRow
		if err := rows.Scan(&row.ID, &row.Seq, &row.PrevHash, &row.CreatedAt, &row.Data, &row.Signature, &row.KeyID); err != nil {
			fmt.Printf("Warning: scan row: %v\n", err)
			continue
		}

		var e evidence.Evidence
		if err := json.Unmarshal([]byte(row.Data), &e); err != nil {
			fmt.Printf("Warning: unmarshal record %s: %v\n", row.ID, err)
			continue
		}

		e.TenantID = defaultTenant

		hash := computeEvidenceHash(&e)
		batch = append(batch, struct {
			id      string
			record  *evidence.Evidence
			newHash string
		}{id: row.ID, record: &e, newHash: hash})

		if len(batch) >= batchSize {
			if err := updateRecordsInBatch(db, batch); err != nil {
				return fmt.Errorf("update batch: %w", err)
			}
			batch = batch[:0]
		}
	}

	if len(batch) > 0 {
		if err := updateRecordsInBatch(db, batch); err != nil {
			return fmt.Errorf("update final batch: %w", err)
		}
	}

	return nil
}

func updateRecordsInBatch(db *sql.DB, batch []struct {
	id      string
	record  *evidence.Evidence
	newHash string
}) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`UPDATE evidence_records SET hash = ?, tenant_id = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, item := range batch {
		_, err := stmt.Exec(item.newHash, item.record.TenantID, item.id)
		if err != nil {
			return fmt.Errorf("update record %s: %w", item.id, err)
		}
	}

	return tx.Commit()
}

func computeEvidenceHash(e *evidence.Evidence) string {
	content := struct {
		ID         string              `json:"id"`
		Seq        uint64              `json:"seq"`
		PrevHash   string              `json:"prev_hash"`
		Timestamp  time.Time           `json:"timestamp"`
		Actor      string              `json:"actor"`
		Action     string              `json:"action"`
		Subject    string              `json:"subject"`
		RunMode    string              `json:"run_mode"`
		Backends   []evidence.BackendFact `json:"backends"`
		InputHash  string              `json:"input_hash"`
		OutputHash string             `json:"output_hash"`
		Payload    json.RawMessage    `json:"payload,omitempty"`
		KeyID      string              `json:"key_id"`
		TenantID   string              `json:"tenant_id,omitempty"`
	}{
		ID:         e.ID,
		Seq:        e.Seq,
		PrevHash:   e.PrevHash,
		Timestamp:  e.Timestamp,
		Actor:      e.Actor,
		Action:     e.Action,
		Subject:    e.Subject,
		RunMode:    e.RunMode,
		Backends:   e.Backends,
		InputHash:  e.InputHash,
		OutputHash: e.OutputHash,
		Payload:    e.Payload,
		KeyID:      e.KeyID,
		TenantID:   e.TenantID,
	}

	jsonBytes, _ := json.Marshal(content)
	hash := sha256.Sum256(jsonBytes)
	return hex.EncodeToString(hash[:])
}

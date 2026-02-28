//go:build ignore

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	dbURL    = "postgres://myappuser:password@localhost:5432/LoanOriginationDB?sslmode=disable"
	tenantID = "00000000-0000-0000-0000-000000000000"
)

func main() {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("Unable to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// 1. Ensure a system user exists for tagged_by.
	systemUserID := "11111111-1111-1111-1111-111111111111"
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, full_name, role_code, status, user_id, tenant_id, username, email, display_name)
		VALUES ($1, 'System Seeder', 'ADMIN', 'ACTIVE', $2::uuid, $3::uuid, 'system_seeder', 'system@seed.local', 'System Seeder')
		ON CONFLICT (tenant_id, lower(username)) DO NOTHING
	`, systemUserID, systemUserID, tenantID)
	if err != nil {
		slog.Error("Failed to upsert system user", "error", err)
		os.Exit(1)
	}
	slog.Info("System user ensured", "user_id", systemUserID)

	// 2. Fetch all existing case IDs.
	rows, err := pool.Query(ctx, `SELECT id::text FROM cases WHERE tenant_id = $1::uuid ORDER BY created_at`, tenantID)
	if err != nil {
		slog.Error("Failed to query cases", "error", err)
		os.Exit(1)
	}
	defer rows.Close()

	var caseIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			slog.Error("Failed to scan case id", "error", err)
			os.Exit(1)
		}
		caseIDs = append(caseIDs, id)
	}
	if err := rows.Err(); err != nil {
		slog.Error("Row iteration error", "error", err)
		os.Exit(1)
	}

	if len(caseIDs) == 0 {
		slog.Warn("No cases found — nothing to tag")
		return
	}
	slog.Info("Found cases to tag", "count", len(caseIDs))

	// 3. Define tag sets per case (rotating through patterns).
	type tag struct {
		code  string
		value *string
	}
	strPtr := func(s string) *string { return &s }

	tagSets := [][]tag{
		// Case 0: complexity + skills + VIP
		{
			{code: "STANDARD_2", value: nil},
			{code: "CREDIT_ANALYST", value: nil},
			{code: "SENIOR_UNDERWRITER", value: nil},
			{code: "VIP", value: nil},
			{code: "DOC_MISSING_ID", value: strPtr("Government-issued ID not provided")},
			{code: "PANEL_EXCEPTION", value: strPtr("Non-panel solicitor approved")},
		},
		// Case 1: simple + doc errors
		{
			{code: "SIMPLE", value: nil},
			{code: "KYC_ANALYST", value: nil},
			{code: "DOC_EXPIRED_CERT", value: strPtr("Certificate of Title expired")},
			{code: "ERROR_INCOME_MISMATCH", value: strPtr("Stated income vs payslip discrepancy")},
		},
		// Case 2: non-standard + many tags (to test overflow "+N more")
		{
			{code: "NON_STANDARD", value: nil},
			{code: "FRAUD_SPECIALIST", value: nil},
			{code: "LEGAL_REVIEWER", value: nil},
			{code: "COMPLIANCE_OFFICER", value: nil},
			{code: "HIGH_NET_WORTH", value: nil},
			{code: "COMPLEX_STRUCTURE", value: strPtr("Multi-entity borrower")},
			{code: "DOC_UNSIGNED", value: strPtr("Loan agreement unsigned")},
		},
	}

	// 4. Insert tags.
	inserted := 0
	for i, caseID := range caseIDs {
		tags := tagSets[i%len(tagSets)]
		for _, t := range tags {
			_, err := pool.Exec(ctx, `
				INSERT INTO case_tags (tenant_id, case_id, tag_code, tag_value, tagged_by)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid)
				ON CONFLICT (tenant_id, case_id, tag_code) WHERE removed_at IS NULL
				DO UPDATE SET tag_value = EXCLUDED.tag_value
			`, tenantID, caseID, t.code, t.value, systemUserID)
			if err != nil {
				slog.Error("Failed to insert tag", "case_id", caseID, "tag_code", t.code, "error", err)
				continue
			}
			inserted++
		}
		slog.Info(fmt.Sprintf("Tagged case %d/%d", i+1, len(caseIDs)), "case_id", caseID, "tags", len(tags))
	}

	slog.Info("Seeding complete", "total_tags_inserted", inserted)
}

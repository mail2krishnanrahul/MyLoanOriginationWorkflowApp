package docverification

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SeedDocVerificationTestData seeds all teams, roles, users, and permissions
// needed for local development and integration testing of the
// HOME_LOAN_DOC_VERIFICATION workflow. Idempotent.
func SeedDocVerificationTestData(ctx context.Context, pool *pgxpool.Pool) error {
	const tenantID = "00000000-0000-0000-0000-000000000000"

	// 1. Seed roles
	roles := []struct {
		Code        string
		DisplayName string
		Permissions []string
	}{
		{
			Code:        RoleLoanOfficer,
			DisplayName: "Loan Officer",
			Permissions: []string{
				"case:view", "case:allocate:self", "document:review",
				"document:tag_error", "case:tag", "deal_structure:view",
				"credit_memo:review", "additional_info:request",
				"adhoc_task:create", "case:submit_for_qa",
			},
		},
		{
			Code:        RoleQAOfficer,
			DisplayName: "QA Officer",
			Permissions: []string{
				"case:view", "qa:review", "qa:approve", "qa:reject",
				"document:view", "case:tag",
			},
		},
		{
			Code:        RoleTeamLead,
			DisplayName: "Team Lead",
			Permissions: []string{
				"case:view", "case:allocate:any", "case:prioritise",
				"document:view", "case:tag", "qa:view",
				"reporting:view",
			},
		},
		{
			Code:        RoleBanker,
			DisplayName: "Banker",
			Permissions: []string{
				"case:view:own", "additional_info:submit",
			},
		},
		{
			Code:        RoleSupportOfficer,
			DisplayName: "Support Officer",
			Permissions: []string{
				"case:view", "case:tag", "additional_info:request",
			},
		},
	}

	for _, r := range roles {
		permsJSON, _ := json.Marshal(r.Permissions)
		_, err := pool.Exec(ctx, `
			INSERT INTO roles (tenant_id, role_code, display_name, is_system_role, permissions)
			VALUES ($1::uuid, $2, $3, false, $4::text[])
			ON CONFLICT (tenant_id, role_code) DO UPDATE
			    SET display_name = EXCLUDED.display_name,
			        permissions  = EXCLUDED.permissions,
			        updated_at   = now()
		`, tenantID, r.Code, r.DisplayName, string(permsJSON))
		if err != nil {
			return fmt.Errorf("seed roles: %s: %w", r.Code, err)
		}
	}
	slog.Info("roles seeded", "count", len(roles))

	// 2. Seed teams
	teams := []struct {
		Code        string
		DisplayName string
		TeamType    string
	}{
		{TeamSupportLead, "Support Lead Team", "PROCESSING"},
		{TeamDocPrep, "Document Preparation Team", "PROCESSING"},
		{TeamQA, "Quality Assurance Team", "UNDERWRITING"},
		{TeamBanker, "Banker Liaison Team", "OPERATIONS"},
	}

	for _, t := range teams {
		_, err := pool.Exec(ctx, `
			INSERT INTO teams (tenant_id, team_code, display_name, team_type)
			VALUES ($1::uuid, $2, $3, $4)
			ON CONFLICT (tenant_id, team_code) DO UPDATE
			    SET display_name = EXCLUDED.display_name,
			        updated_at   = now()
		`, tenantID, t.Code, t.DisplayName, t.TeamType)
		if err != nil {
			return fmt.Errorf("seed teams: %s: %w", t.Code, err)
		}
	}
	slog.Info("teams seeded", "count", len(teams))

	// 3. Seed 20 test users (5 loan officers, 3 QA officers, 2 team leads,
	//    5 bankers, 5 support officers) with synthetic credentials.
	type testUser struct {
		Email       string
		DisplayName string
		RoleCode    string
		TeamCode    string
		Skills      []string
	}
	users := []testUser{
		// Loan Officers
		{"lo1@docver.test", "Alice Nguyen", RoleLoanOfficer, TeamDocPrep, []string{"RESIDENTIAL_LENDING", "TRUST_STRUCTURES"}},
		{"lo2@docver.test", "Ben Okafor", RoleLoanOfficer, TeamDocPrep, []string{"COMMERCIAL_LENDING", "COMPANY_BORROWERS"}},
		{"lo3@docver.test", "Clara Mendez", RoleLoanOfficer, TeamDocPrep, []string{"SMSF_LENDING", "FOREIGN_NATIONALS"}},
		{"lo4@docver.test", "David Kim", RoleLoanOfficer, TeamDocPrep, []string{"CONSTRUCTION_FINANCE", "HIGH_NET_WORTH"}},
		{"lo5@docver.test", "Eva Patel", RoleLoanOfficer, TeamDocPrep, []string{"RESIDENTIAL_LENDING", "PANEL_SOLICITOR_COORD"}},
		// QA Officers
		{"qa1@docver.test", "Frank Liu", RoleQAOfficer, TeamQA, []string{"QA_REVIEW", "CREDIT_MEMO_REVIEW"}},
		{"qa2@docver.test", "Grace Hassan", RoleQAOfficer, TeamQA, []string{"QA_REVIEW"}},
		{"qa3@docver.test", "Henry Brown", RoleQAOfficer, TeamQA, []string{"QA_REVIEW", "COMMERCIAL_LENDING"}},
		// Team Leads
		{"tl1@docver.test", "Iris Wong", RoleTeamLead, TeamSupportLead, []string{"RESIDENTIAL_LENDING", "CREDIT_MEMO_REVIEW"}},
		{"tl2@docver.test", "James Obi", RoleTeamLead, TeamDocPrep, []string{"COMMERCIAL_LENDING", "HIGH_NET_WORTH"}},
		// Bankers
		{"banker1@docver.test", "Karen Smith", RoleBanker, TeamBanker, nil},
		{"banker2@docver.test", "Leo Tran", RoleBanker, TeamBanker, nil},
		{"banker3@docver.test", "Mia Johansson", RoleBanker, TeamBanker, nil},
		{"banker4@docver.test", "Nadia Costa", RoleBanker, TeamBanker, nil},
		{"banker5@docver.test", "Omar Fattah", RoleBanker, TeamBanker, nil},
		// Support Officers
		{"support1@docver.test", "Priya Das", RoleSupportOfficer, TeamSupportLead, nil},
		{"support2@docver.test", "Quentin Hall", RoleSupportOfficer, TeamSupportLead, nil},
		{"support3@docver.test", "Rachel Park", RoleSupportOfficer, TeamSupportLead, nil},
		{"support4@docver.test", "Sam Osei", RoleSupportOfficer, TeamSupportLead, nil},
		{"support5@docver.test", "Tara Murphy", RoleSupportOfficer, TeamSupportLead, nil},
	}

	for _, u := range users {
		skillsJSON, _ := json.Marshal(u.Skills)
		username := u.Email[:len(u.Email)-len("@docver.test")]

		var userID string
		err := pool.QueryRow(ctx, `
			INSERT INTO users (
				tenant_id, username, email, display_name, full_name,
				role_code, status, auth_provider, metadata
			)
			VALUES (
				$1::uuid, $2, $3, $4, $4,
				$5, 'ACTIVE', 'LOCAL',
				jsonb_build_object('skills', $6::jsonb)
			)
			ON CONFLICT (tenant_id, LOWER(email)) DO UPDATE
			    SET display_name = EXCLUDED.display_name,
			        metadata     = EXCLUDED.metadata,
			        updated_at   = now()
			RETURNING user_id::text
		`, tenantID, username, u.Email, u.DisplayName, u.RoleCode, string(skillsJSON)).Scan(&userID)
		if err != nil {
			return fmt.Errorf("seed user %s: %w", u.Email, err)
		}

		// Assign role
		_, err = pool.Exec(ctx, `
			INSERT INTO user_roles (user_id, role_id, tenant_id, assigned_by)
			SELECT $1::uuid, role_id, $2::uuid, $1::uuid
			FROM roles
			WHERE tenant_id = $2::uuid AND role_code = $3
			ON CONFLICT DO NOTHING
		`, userID, tenantID, u.RoleCode)
		if err != nil {
			return fmt.Errorf("seed user_role %s: %w", u.Email, err)
		}

		// Add to team
		_, err = pool.Exec(ctx, `
			INSERT INTO team_members (team_id, user_id, tenant_id, role_in_team, added_by)
			SELECT team_id, $1::uuid, $2::uuid, 'MEMBER', $1::uuid
			FROM teams
			WHERE tenant_id = $2::uuid AND team_code = $3
			ON CONFLICT DO NOTHING
		`, userID, tenantID, u.TeamCode)
		if err != nil {
			return fmt.Errorf("seed team_member %s-%s: %w", u.Email, u.TeamCode, err)
		}
	}
	slog.Info("test users seeded", "count", len(users))

	return nil
}

// SeedDocVerificationNotificationConfig seeds notification templates and
// triggers for all workflow events. Idempotent.
func SeedDocVerificationNotificationConfig(ctx context.Context, pool *pgxpool.Pool) error {
	const tenantID = "00000000-0000-0000-0000-000000000000"
	type notifTemplate struct {
		Code    string
		Channel string
		Subject string
		Body    string
	}
	templates := []notifTemplate{
		{
			Code:    "DOC_VER_CASE_ALLOCATED",
			Channel: "IN_APP",
			Subject: "New case allocated to you",
			Body:    "Case {{.reference_number}} ({{.complexity}}) has been allocated to you. SLA: {{.sla_due_at}}.",
		},
		{
			Code:    "DOC_VER_ADDITIONAL_INFO_REQUESTED",
			Channel: "IN_APP",
			Subject: "Additional information requested",
			Body:    "Additional documents are required for case {{.reference_number}}. Please submit by {{.due_date}}.",
		},
		{
			Code:    "DOC_VER_QA_REVIEW_REQUIRED",
			Channel: "IN_APP",
			Subject: "Case ready for QA review",
			Body:    "Case {{.reference_number}} has been submitted for QA review.",
		},
		{
			Code:    "DOC_VER_QA_REJECTED",
			Channel: "IN_APP",
			Subject: "QA Review Rejected — Remediation Required",
			Body:    "Case {{.reference_number}} was rejected during QA review. Please action the remediation items.",
		},
		{
			Code:    "DOC_VER_QA_APPROVED",
			Channel: "IN_APP",
			Subject: "QA Approved",
			Body:    "Case {{.reference_number}} has been approved by QA.",
		},
		{
			Code:    "DOC_VER_NON_STANDARD_FLAGGED",
			Channel: "IN_APP",
			Subject: "Non-Standard Case — Team Lead Review Required",
			Body:    "Case {{.reference_number}} has been flagged as NON_STANDARD and requires Team Lead review.",
		},
	}

	for _, t := range templates {
		code := t.Code
		channel := t.Channel
		subject := t.Subject
		body := t.Body
		_, err := pool.Exec(ctx, `
			INSERT INTO notification_templates (
				template_code, case_type_code, channel,
				subject_template, body_template, language_code, status
			) VALUES ($1, $2, $3, $4, $5, 'en-AU', 'ACTIVE')
			ON CONFLICT (template_code, language_code) DO UPDATE
			    SET body_template    = EXCLUDED.body_template,
			        subject_template = EXCLUDED.subject_template,
			        updated_at       = now()
		`, code, CaseTypeCode, channel, subject, body)
		if err != nil {
			return fmt.Errorf("seed notification template %s: %w", code, err)
		}
	}

	// Seed triggers
	type trigger struct {
		Code      string
		EventType string
		Template  string
		Recipient string
	}
	triggers := []trigger{
		{"DOC_VER_ALLOCATED_TRIGGER", "CASE_ALLOCATED", "DOC_VER_CASE_ALLOCATED", "TASK_ASSIGNEE"},
		{"DOC_VER_ADDL_INFO_TRIGGER", "ADDITIONAL_INFO_REQUESTED", "DOC_VER_ADDITIONAL_INFO_REQUESTED", "BORROWER"},
		{"DOC_VER_QA_REVIEW_TRIGGER", "CASE_SUBMITTED_FOR_QA", "DOC_VER_QA_REVIEW_REQUIRED", "TASK_ASSIGNEE"},
		{"DOC_VER_QA_REJECTED_TRIGGER", "QA_REJECTED", "DOC_VER_QA_REJECTED", "CASE_OWNER"},
		{"DOC_VER_QA_APPROVED_TRIGGER", "QA_APPROVED", "DOC_VER_QA_APPROVED", "CASE_OWNER"},
		{"DOC_VER_NONSTANDARD_TRIGGER", "NON_STANDARD_CASE_FLAGGED", "DOC_VER_NON_STANDARD_FLAGGED", "SUPERVISOR"},
	}

	_ = tenantID
	for _, tr := range triggers {
		_, err := pool.Exec(ctx, `
			INSERT INTO notification_triggers (
				trigger_code, case_type_code, event_type,
				template_code, recipient_type, priority, is_enabled
			) VALUES ($1, $2, $3, $4, $5, 'NORMAL', true)
			ON CONFLICT (trigger_code) DO UPDATE
			    SET template_code  = EXCLUDED.template_code,
			        recipient_type = EXCLUDED.recipient_type,
			        updated_at     = now()
		`, tr.Code, CaseTypeCode, tr.EventType, tr.Template, tr.Recipient)
		if err != nil {
			return fmt.Errorf("seed notification trigger %s: %w", tr.Code, err)
		}
	}

	slog.Info("notification config seeded",
		"templates", len(templates),
		"triggers", len(triggers))
	return nil
}

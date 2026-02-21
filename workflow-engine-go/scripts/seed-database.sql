-- Add standard Case Types
INSERT INTO lookup_case_types (code, name, description, active)
VALUES ('HOME_LOAN', 'Home Loan Application', 'Standard residential mortgage processing workflow', true)
ON CONFLICT (code) DO NOTHING;

-- Add standard Document Types
INSERT INTO lookup_document_types (code, name, description, required, case_type_code, document_category)
VALUES 
    ('INCOME_PROOF', 'Proof of Income', 'Recent payslips or tax returns', true, 'HOME_LOAN', 'FINANCIAL'),
    ('ID_DOCUMENT', 'Identity Document', 'Government issued ID or Passport', true, 'HOME_LOAN', 'IDENTITY'),
    ('PROPERTY_VALUATION', 'Property Valuation Report', 'Independent assessment of property value', false, 'HOME_LOAN', 'PROPERTY')
ON CONFLICT (code) DO NOTHING;

-- Add User Roles
INSERT INTO roles (id, name, description, is_system)
VALUES 
    ('role:loan_officer', 'Loan Officer', 'Initiates and manages customer applications', true),
    ('role:underwriter', 'Underwriter', 'Assesses risk and approves/rejects loans', true),
    ('role:supervisor', 'Supervisor', 'Manages team operations and escalations', true),
    ('role:compliance', 'Compliance Officer', 'Oversees regulatory adherence and audits', true)
ON CONFLICT (id) DO NOTHING;

-- Map basic permissions to roles 
-- Assuming permissions exist like 'case:read', 'case:create', 'approval:execute'
-- For the seed, we rely on the schema defaults, but a production app would load a dense RBAC matrix here.

-- Add Sample Users
INSERT INTO users (id, scim_id, username, email, first_name, last_name, active)
VALUES 
    ('usr:001', 'scim:001', 'loanofficer1', 'loanofficer1@bank.local', 'Sarah', 'Jenkins', true),
    ('usr:002', 'scim:002', 'underwriter1', 'underwriter1@bank.local', 'Michael', 'Chen', true),
    ('usr:003', 'scim:003', 'supervisor1', 'supervisor1@bank.local', 'David', 'Thompson', true),
    ('usr:004', 'scim:004', 'compliance1', 'compliance1@bank.local', 'Alisha', 'Patel', true)
ON CONFLICT (id) DO NOTHING;

-- Assign Roles to Users
INSERT INTO user_roles (user_id, role_id)
VALUES 
    ('usr:001', 'role:loan_officer'),
    ('usr:002', 'role:underwriter'),
    ('usr:003', 'role:supervisor'),
    ('usr:004', 'role:compliance')
ON CONFLICT (user_id, role_id) DO NOTHING;

-- Add common Workbaskets
INSERT INTO workbaskets (id, name, description, criteria)
VALUES 
    ('wb:general_intake', 'General Intake', 'New applications requiring initial review', '{"stage": "INTAKE"}'),
    ('wb:escalations', 'Supervisor Escalations', 'Cases breaching SLAs or requiring overrides', '{"sla_breach": true}')
ON CONFLICT (id) DO NOTHING;

-- Add basic Worker Skills for Task Routing
INSERT INTO skills (id, code, name, description)
VALUES 
    ('skill:credit_auth_500k', 'CREDIT_AUTH_500K', 'Credit Authority Up To $500k', 'Authorized to approve loans up to half a million'),
    ('skill:kyc_expert', 'KYC_EXPERT', 'KYC Verification Expert', 'Trained in comprehensive identity and background checks')
ON CONFLICT (id) DO NOTHING;

-- Assign Skills to Users
INSERT INTO worker_skills (user_id, skill_id, proficiency_level)
VALUES 
    ('usr:002', 'skill:credit_auth_500k', 100),
    ('usr:001', 'skill:kyc_expert', 80)
ON CONFLICT (user_id, skill_id) DO NOTHING;

-- Insert SLA Config for HOME_LOAN
INSERT INTO sla_configurations (id, entity_type, entity_id, target_duration, critical_duration)
VALUES ('sla:hl_default', 'CASE_TYPE', 'HOME_LOAN', '168h', '240h') -- 7 days target, 10 days critical
ON CONFLICT (id) DO NOTHING;

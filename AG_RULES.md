# AGENT_RULES.md

This document establishes the comprehensive technical governance framework, coding standards, and engineering best practices for enterprise Go development. All team members, contributors, and automated systems must adhere to these standards.

---

## 1. Governance & Process

### 1.1 Requirements Analysis
- **Specification Review Protocol**: Before implementation, conduct thorough requirements analysis. Document all gaps, ambiguities, risks, and assumptions in a Requirements Analysis Document (RAD).
- **Stakeholder Sign-off**: Obtain formal approval from technical leads and product owners before proceeding.
- **Impact Assessment**: Evaluate impact on existing systems, dependencies, performance, and security posture.
- **Always Commit to GitHub**: Ensure all code changes are committed to GitHub before proceeding.

### 1.2 Implementation Planning
- **Mandatory Planning Documents**: Create detailed `implementation_plan.md` for all medium-to-large initiatives including:
  - Technical approach and architecture decisions
  - Component breakdown and interaction diagrams
  - Risk mitigation strategies
  - Rollback procedures
  - Performance and security considerations
  - Resource requirements and timeline estimates
- **Approval Gate**: Implementation plans require formal approval from technical leadership before development begins.
- **Plan Versioning**: Maintain plans under version control with change history.

### 1.3 Task Management
- **Work Breakdown Structure**: Decompose all work into manageable tasks documented in `task.md` or project management system.
- **Status Tracking**: Update task status daily (Not Started, In Progress, Blocked, In Review, Complete).
- **Dependency Mapping**: Document inter-task dependencies and critical paths.
- **Retrospectives**: Conduct regular reviews to identify process improvements.

### 1.4 Documentation Standards
- **Code Documentation**: Maintain comprehensive Go doc comments for all exported types, functions, and packages.
- **Architecture Decision Records (ADRs)**: Document significant architectural decisions with context, alternatives considered, and rationale.
- **Runbooks**: Create operational runbooks for deployment, monitoring, incident response, and disaster recovery.
- **API Documentation**: Maintain OpenAPI/Swagger specifications for all REST endpoints.
- **Requirements Documentation**: Maintain `requirements.md` for high-level system requirements.

---

## 2. Software Engineering Principles

### 2.1 Go Design Principles (Mandatory)
- **Single Responsibility**: Each package and file should have a clear, focused purpose. Maximum file size: 500 lines.
- **Interface Segregation**: Define small, focused interfaces (1-3 methods). "The bigger the interface, the weaker the abstraction." — Rob Pike.
- **Composition over Inheritance**: Use struct embedding and interfaces for code reuse. Go has no inheritance; prefer composition.
- **Accept Interfaces, Return Structs**: Functions should accept interfaces for flexibility and return concrete types for clarity.
- **Dependency Injection**: Pass dependencies explicitly via constructor functions (`NewXxxService(repo Repository)`). Avoid package-level globals.

### 2.2 Design Principles
- **KISS (Keep It Simple)**: Favor simplicity over cleverness. Complex solutions require exceptional justification.
- **DRY (Don't Repeat Yourself)**: Extract duplicate logic into reusable utilities, services, or packages. Three-strike rule: refactor on third occurrence.
- **YAGNI (You Aren't Gonna Need It)**: Implement only what is required now. Avoid speculative generalization.
- **Separation of Concerns**: Clearly separate business logic, data access, HTTP handling, and infrastructure concerns.
- **Fail Fast**: Validate inputs and preconditions early. Return meaningful errors immediately upon detecting invalid state.

### 2.3 Clean Code Standards

#### 2.3.1 Code Structure
- **Function Complexity**: Maximum cyclomatic complexity: 10. Functions exceeding this must be refactored.
- **Function Length**: Target maximum: 40 lines. Hard limit: 60 lines.
- **File Size**: Target maximum: 300 lines. Hard limit: 500 lines.
- **Parameter Count**: Maximum: 4 parameters. Use option structs or functional options for more complex cases.
- **Nesting Depth**: Maximum: 3 levels. Deeply nested code must be refactored using early returns (guard clauses).

#### 2.3.2 Naming Conventions
- **Packages**: Short, lowercase, single-word (`repository`, `engine`, `model`). No underscores or mixedCaps.
- **Exported Types**: PascalCase, noun-based (`UserService`, `OrderRepository`).
- **Interfaces**: PascalCase, typically `-er` suffix for single-method interfaces (`Reader`, `Validator`). Multi-method interfaces use noun names (`Repository`, `Orchestrator`).
- **Functions/Methods**: PascalCase if exported, camelCase if unexported, verb-based (`CalculateTotal()`, `validateInput()`).
- **Variables**: camelCase, descriptive (`userID`, `orderTotal`). Short variable names only in small scopes (`i`, `ctx`, `tx`, `err`).
- **Constants**: PascalCase for exported, camelCase for unexported. Use typed constants with `iota` for enumerations.
- **Boolean Variables/Methods**: Prefix with `Is`, `Has`, `Can` (`IsValid`, `HasPermission`).
- **Avoid Abbreviations**: Use full words unless abbreviation is industry standard (e.g., `ID`, `URL`, `HTTP`, `SQL`).
- **Acronyms**: All caps for acronyms (`userID`, `httpClient`, `sqlDB`), not `userId`, `httpClient`.

#### 2.3.3 Comments & Documentation
- **Self-Documenting Code**: Code should explain *what* it does through clear naming and structure.
- **Comments for Why**: Use comments to explain *why* decisions were made, not what the code does.
- **Go Doc Comment Requirements**:
  - All exported packages (in `doc.go` or first file), types, functions, and methods
  - Comments start with the name of the thing being documented: `// HandleEvent dispatches domain events to the appropriate handler.`
  - Describe edge cases, error conditions, and assumptions
- **TODO Comments**: Must include ticket number and owner (`// TODO(JIRA-123, @username): Description`).
- **Remove Dead Code**: Never comment out code. Remove it (version control preserves history).

#### 2.3.4 Code Smells to Avoid
- **Long Functions/Files**: Violates SRP. Refactor immediately.
- **Long Parameter Lists**: Use option structs or functional options pattern.
- **Primitive Obsession**: Wrap primitives in domain-specific types using `type` declarations (e.g., `type CaseStatus string`).
- **Feature Envy**: Functions accessing data of other packages excessively should be moved.
- **Data Clumps**: Repeated groups of parameters should become structs.
- **Magic Numbers/Strings**: Extract to named constants.

---

## 3. Database Management

### 3.1 Schema Management (golang-migrate Mandatory)
- **Version Control**: ALL database changes must be managed through golang-migrate migration files under version control.
- **Migration Standards**:
  - One logical change per migration pair (up + down)
  - Include rollback instructions in `.down.sql`
  - Use meaningful sequence numbers and descriptions
  - Add descriptive SQL comments
- **Naming Convention**: `{6-digit seq}_{snake_case_description}.{up|down}.sql` (e.g., `000009_add_tasks_sla_deadline.up.sql`).
- **Review Process**: Database changes require code review and DBA approval for production.

### 3.2 DDL Safety Rules
- **Production Safety**:
  - NEVER use `DROP TABLE`, `DROP COLUMN`, or destructive DDL in the same release as code changes
  - Always use `IF NOT EXISTS` / `IF EXISTS` guards
  - Add new columns as nullable or with defaults (no table rewrites)
- **Zero-Downtime Migrations**: Follow two-phase deploy pattern (add column → deploy code → backfill → next deploy uses column).
- **Schema Validation**: Verify all foreign keys, constraints, and indexes at code review time.

### 3.3 Database Design Standards
- **Normalization**: Minimum 3NF unless denormalization is explicitly justified for performance.
- **Constraints**:
  - Enforce referential integrity with foreign keys
  - Use unique constraints for natural keys
  - Apply NOT NULL constraints where appropriate
  - Use check constraints for data validation
- **Indexing Strategy**:
  - Index all foreign keys
  - Index columns used in WHERE, JOIN, ORDER BY clauses
  - Use partial indexes for status-filtered queries
  - Use `CREATE INDEX CONCURRENTLY` for large tables
  - Monitor and optimize based on query patterns
- **Naming Conventions**:
  - Tables: plural, lowercase with underscores (`users`, `order_items`)
  - Columns: lowercase with underscores (`first_name`, `created_at`)
  - Indexes: `idx_tablename_columnname`
  - Foreign keys: `fk_tablename_referenced_table`

### 3.4 Data Integrity
- **Audit Columns**: Include `created_at`, `created_by`, `updated_at`, `updated_by` on all tables.
- **Soft Deletes**: Use `deleted_at` timestamp for logical deletes where applicable.
- **Optimistic Locking**: Implement `row_version` columns for concurrent update protection.
- **Transactions**: Use appropriate isolation levels. Default to READ COMMITTED. Always use explicit `tx.Begin/Commit/Rollback`.

---

## 4. Security Standards

### 4.1 Secrets Management (Zero Tolerance)
- **Prohibition**: NEVER commit secrets, credentials, API keys, tokens, or certificates to version control.
- **Detection**: Configure pre-commit hooks to scan for secrets (use tools like git-secrets, truffleHog).
- **Storage**:
  - Use environment variables for local development
  - Use enterprise secret managers (HashiCorp Vault, AWS Secrets Manager, Azure Key Vault) for production
  - Rotate secrets regularly per policy
- **Remediation**: If secrets are committed, immediately:
  1. Rotate the compromised credentials
  2. Remove from Git history (`git filter-branch` or BFG Repo-Cleaner)
  3. Report incident per security policy

### 4.2 Input Validation & Sanitization
- **Validate at Boundary**: All external input must be validated at the HTTP handler layer.
- **Whitelist Approach**: Define allowed inputs rather than blacklisting dangerous ones.
- **Validation Pattern**: Validate with explicit checks in handler or a dedicated `validate()` method on request structs. Return structured error responses.
- **SQL Injection**: Use parameterized queries (`$1`, `$2`, ...) exclusively. NEVER concatenate SQL strings.
- **XSS Prevention**: Sanitize output when rendering user-generated content. Use `html/template` for HTML.
- **Path Traversal**: Validate file paths and restrict to expected directories using `filepath.Clean`.

### 4.3 Authentication & Authorization
- **Framework**: Use a well-tested middleware stack (e.g., custom JWT middleware or `go-chi/jwtauth`). Do not implement custom crypto.
- **Password Policy**:
  - Minimum complexity requirements per enterprise policy
  - Use bcrypt (`golang.org/x/crypto/bcrypt`) or Argon2 for hashing (NEVER MD5 or SHA-1)
  - Implement account lockout after failed attempts
- **JWT Standards**:
  - Use appropriate expiration times (access: 15-30 min, refresh: 7-30 days)
  - Sign with RS256 or ES256 (not HS256 in distributed systems)
  - Validate all claims (iss, aud, exp, nbf)
- **Authorization**:
  - Implement role-based (RBAC) or attribute-based (ABAC) access control via middleware
  - Deny by default; explicitly grant access
  - Validate authorization on every protected resource access

### 4.4 Dependency Management
- **Scanning**: Run dependency vulnerability scans (`govulncheck`, Snyk) in CI/CD pipeline.
- **Update Policy**:
  - Critical vulnerabilities: Patch within 24 hours
  - High vulnerabilities: Patch within 7 days
  - Medium/Low: Address during regular maintenance
- **Go Modules**: Use `go mod tidy` to keep dependencies clean. Pin versions in `go.sum`.
- **License Compliance**: Verify all dependencies comply with enterprise licensing policy.

### 4.5 Additional Security Controls
- **HTTPS Only**: Enforce TLS 1.2+ for all communications. No plaintext protocols.
- **CORS**: Configure strict CORS policies via middleware. Avoid wildcard origins.
- **Security Headers**: Implement Content-Security-Policy, X-Frame-Options, HSTS, etc. via middleware.
- **Rate Limiting**: Implement API rate limiting via middleware (e.g., `golang.org/x/time/rate`).
- **Audit Logging**: Log all security-relevant events (authentication, authorization failures, data access).
- **Error Handling**: Never expose stack traces or sensitive system information in error responses. Return structured JSON errors.

---

## 5. Version Control Standards

### 5.1 Branching Strategy
- **Model**: Git Flow or Trunk-Based Development (as defined by team).
- **Branch Types**:
  - `main`/`master`: Production-ready code only
  - `develop`: Integration branch for features
  - `feature/*`: New features (`feature/JIRA-123-user-login`)
  - `bugfix/*`: Bug fixes (`bugfix/JIRA-456-fix-nil-pointer`)
  - `hotfix/*`: Critical production fixes
  - `release/*`: Release preparation
- **Protection**: Enforce branch protection rules on `main` and `develop` (require reviews, status checks).

### 5.2 Commit Standards
- **Message Format**: Follow Conventional Commits specification:
  ```
  <type>(<scope>): <subject>

  <body>

  <footer>
  ```
- **Types**: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `perf`, `ci`
- **Examples**:
  - `feat(auth): add OAuth2 login support`
  - `fix(order): resolve calculation error for discounts`
  - `docs(readme): update installation instructions`
- **Atomic Commits**: Each commit should represent one logical change.
- **Commit Frequency**: Commit early and often. Minimum once per day for work in progress.

### 5.3 Code Review Process
- **Mandatory Reviews**: All code requires peer review before merge.
- **Reviewer Responsibilities**:
  - Verify adherence to coding standards
  - Check for security vulnerabilities
  - Validate test coverage
  - Assess maintainability and design
- **Author Responsibilities**:
  - Keep PRs small (< 400 lines preferred)
  - Provide clear description and context
  - Respond to feedback promptly
  - Resolve all conversations before merge
- **Approval Requirements**: Minimum 2 approvals for production merges.

### 5.4 Repository Hygiene
- **`.gitignore`**: Maintain comprehensive `.gitignore` for IDE files, build artifacts, OS files, local configs, and vendor directory (if not vendoring).
- **Binary Files**: Avoid committing binaries. Use artifact repositories or Git LFS if necessary.
- **Large Files**: Keep repository size manageable. Archive or remove obsolete large files.
- **Cleanup**: Regularly delete merged branches.

---

## 6. Testing Standards

### 6.1 Test Coverage Requirements
- **Unit Tests**: Minimum 80% line coverage, 70% branch coverage.
- **Integration Tests**: Cover all critical business flows and external integrations.
- **End-to-End Tests**: Automate key user journeys for regression detection.
- **Coverage Reporting**: Generate coverage reports with `go test -coverprofile` in CI/CD and fail builds below threshold.

### 6.2 Test Design Principles
- **Independence**: Tests must not depend on execution order or shared state.
- **Determinism**: Tests must produce consistent results. No flaky tests.
- **Speed**: Unit tests should execute in milliseconds. Optimize slow tests.
- **Clarity**: Test names should describe what is being tested and expected outcome (`TestCreateCase_InvalidCaseType_ReturnsError`).
- **AAA Pattern**: Structure tests as Arrange, Act, Assert.

### 6.3 Test Types & Tools
- **Unit Testing**: Go standard `testing` package + `testify/assert` for assertions
- **Integration Testing**: `testcontainers-go` for database containers, `httptest` for HTTP handlers
- **E2E Testing**: Playwright (for UI), `net/http` client for API-level E2E
- **Performance Testing**: `testing.B` benchmarks for micro-benchmarks, k6 or Vegeta for load testing
- **Security Testing**: OWASP ZAP, `govulncheck`

### 6.4 Test Data Management
- **Isolation**: Use separate test databases (Testcontainers). Never test against production data.
- **Fixtures**: Use helper functions or table-driven tests for test data creation.
- **Cleanup**: Ensure proper cleanup after tests (use `t.Cleanup()` or deferred rollbacks).
- **Synthetic Data**: Generate realistic but synthetic data for testing.

### 6.5 Continuous Testing
- **CI Integration**: Run full test suite on every commit.
- **Fast Feedback**: Fail fast on test failures. Prioritize fast tests.
- **Test Quarantine**: Isolate flaky tests for investigation; do not disable permanently.

---

## 7. Logging & Monitoring Standards

### 7.1 Logging Framework
- **Standard**: `log/slog` (Go 1.21+ structured logging) as the primary logging package. For pre-1.21 codebases, use `zerolog` or `zap`.
- **Configuration**: Externalize logging configuration via environment variables (`LOG_LEVEL`, `LOG_FORMAT`).
- **Log Output**: JSON format for production (machine-parsable), text format for development (human-readable).
- **Retention**: Define retention policy per environment (e.g., 30 days production, 7 days development).

### 7.2 Log Levels (Strict Definitions)
- **ERROR** (`slog.LevelError`):
  - System failures requiring immediate attention
  - Errors that prevent operation completion
  - Data corruption or loss scenarios
  - **Action Required**: On-call alert
- **WARN** (`slog.LevelWarn`):
  - Degraded functionality but operation continues
  - Recoverable errors or fallback scenarios
  - Deprecated API usage
  - Resource constraints (approaching limits)
  - **Action Required**: Investigation within SLA
- **INFO** (`slog.LevelInfo`):
  - Application lifecycle events (startup, shutdown, deployment)
  - Significant business events (case creation, stage transitions)
  - Configuration changes
  - Scheduled job execution
  - **Audience**: Operations team, business analysts
- **DEBUG** (`slog.LevelDebug`):
  - Detailed execution flow for troubleshooting
  - Function entry/exit with parameters
  - State transitions
  - Query execution details
  - **Audience**: Developers during troubleshooting

### 7.3 Logging Best Practices
- **Structured Logging**: Use key-value pairs for machine parsing:
  ```go
  slog.Info("user login successful",
      "user_id", userID,
      "ip_address", ipAddress,
      "timestamp", time.Now())
  ```
- **Correlation IDs**: Include request/trace IDs in all log statements for distributed tracing. Use `context.Context` to propagate.
- **Contextual Information**: Use `slog.With()` to attach context to a logger instance for the request lifecycle.
- **Performance**: Structured loggers avoid unnecessary string allocation. Avoid `fmt.Sprintf` in log calls:
  ```go
  // Correct
  slog.Debug("processing order", "order_id", orderID)

  // Incorrect (allocates string even if debug disabled)
  log.Printf("Processing order %s", orderID)
  ```
- **Error Logging**: Always log the full error chain:
  ```go
  slog.Error("failed to process order",
      "error", err,
      "order_id", orderID)
  ```

### 7.4 Security & Compliance
- **PII Protection**: NEVER log personally identifiable information (names, emails, SSNs, credit cards).
- **Credential Protection**: NEVER log passwords, API keys, tokens, or secrets.
- **Data Masking**: Mask sensitive data if logging is necessary (e.g., `****-****-****-1234`).
- **Compliance**: Ensure logging practices comply with GDPR, HIPAA, or other applicable regulations.

### 7.5 Monitoring & Observability
- **Metrics**: Implement application metrics (response times, error rates, throughput) using Prometheus client (`prometheus/client_golang`).
- **Health Checks**: Expose `/healthz` (liveness) and `/readyz` (readiness) endpoints.
- **Alerting**: Configure alerts for critical errors, performance degradation, and SLA violations.
- **Dashboards**: Create operational dashboards (Grafana) for real-time system visibility.
- **Distributed Tracing**: Implement distributed tracing using OpenTelemetry (`go.opentelemetry.io/otel`) for service-to-service calls.

---

## 8. Performance Standards

### 8.1 Performance Requirements
- **API Response Times**:
  - p50 < 200ms
  - p95 < 500ms
  - p99 < 1000ms
- **Database Queries**: Individual queries < 100ms under normal load.
- **Batch Processing**: Design for horizontal scalability.

### 8.2 Optimization Practices
- **Database**: Use proper indexing, query optimization, connection pooling (`pgxpool`).
- **Caching**: Implement caching layers (Redis via `go-redis`, in-process via `sync.Map` or `groupcache`) for frequently accessed data.
- **Lazy Loading**: Avoid loading unnecessary data. Use targeted SQL queries over full-table scans.
- **Goroutines**: Use goroutines and channels for concurrent I/O-bound operations. Use `errgroup` for coordinated parallel work.
- **Profiling**: Regularly profile applications using `pprof` to identify CPU, memory, and goroutine leaks.
- **Connection Pools**: Configure `pgxpool` with appropriate `MaxConns`, `MinConns`, and `MaxConnIdleTime`.

---

## 9. Code Quality & Static Analysis

### 9.1 Static Analysis Tools (Mandatory)
- **`go vet`**: Must pass with zero findings before merge.
- **`staticcheck`**: Enforce correctness and style checks.
- **`golangci-lint`**: Meta-linter aggregating multiple linters (revive, errcheck, gosec, ineffassign, etc.).
- **`gosec`**: Security-focused static analysis.
- **`govulncheck`**: Scan for known vulnerabilities in dependencies.

### 9.2 Quality Gates
- **Code Coverage**: Minimum 80% line coverage (enforced via `go test -cover`).
- **Lint Findings**: Zero critical, < 10 warnings per 1000 lines.
- **Duplications**: < 3% duplicate code.
- **Error Handling**: All errors must be checked. `errcheck` linter must pass.
- **Security Hotspots**: All `gosec` findings reviewed and resolved.

---

## 10. Compliance & Enforcement

### 10.1 Automated Enforcement
- **CI/CD Integration**: Enforce all rules through automated pipeline checks (`golangci-lint`, `go test`, `govulncheck`).
- **Pre-commit Hooks**: Install hooks to catch violations before commit.
- **Quality Gates**: Block merges that violate standards.

### 10.2 Continuous Improvement
- **Regular Reviews**: Review and update standards quarterly.
- **Feedback Loop**: Encourage team feedback on standards effectiveness.
- **Training**: Provide regular training on standards and best practices.

### 10.3 Exceptions
- **Request Process**: Exceptions to standards require written justification and architect approval.
- **Documentation**: Document all approved exceptions with rationale and expiration date.
- **Technical Debt**: Track exceptions as technical debt for future remediation.

---

## 11. References & Resources

- **Go Style Guide**: [Effective Go](https://go.dev/doc/effective_go)
- **Go Code Review Comments**: [Go Wiki - Code Review Comments](https://go.dev/wiki/CodeReviewComments)
- **Go Proverbs**: [Rob Pike's Go Proverbs](https://go-proverbs.github.io/)
- **Security Standards**: [OWASP Top 10](https://owasp.org/www-project-top-ten/)
- **Clean Code**: Robert C. Martin - "Clean Code: A Handbook of Agile Software Craftsmanship"
- **Database Migrations**: [golang-migrate Documentation](https://github.com/golang-migrate/migrate)

---


### 2.4 Specific Coding Rules
- **Error Handling**: Always check and handle errors. Never use `_` to discard errors unless explicitly justified with a comment. Use `fmt.Errorf("context: %w", err)` to wrap errors with context.
- **Structured Logging**: Use `log/slog` (or `zerolog`/`zap`) for logging instead of `fmt.Println` or `log.Printf`.
- **Input Validation**: Validate all inputs at the beginning of a function. Check for empty strings, nil pointers, zero values, and invalid ranges.
- **Configurable Values**: Do not hardcode configuration values (URLs, model names, timeouts). Use environment variables loaded via `os.Getenv` or a config struct.
- **Table-Driven Tests**: Use table-driven test patterns for testing multiple scenarios in a clean, DRY manner.
- **Descriptive Error Messages**: Provide clear and descriptive error messages. Include the operation that failed and relevant context: `fmt.Errorf("GetCaseType(%s): %w", caseTypeID, err)`.
- **Context Propagation**: Always pass `context.Context` as the first parameter to functions that perform I/O or may be cancelled.
- **Defer for Cleanup**: Use `defer` for resource cleanup (`tx.Rollback`, `rows.Close`, `file.Close`). Place `defer` immediately after resource acquisition.

---

**Document Control:**
- **Version**: 2.0
- **Last Updated**: 2026-02-15
- **Review Cycle**: Quarterly
- **Owner**: Engineering Leadership
- **Approval Required For Changes**: Architecture Review Board
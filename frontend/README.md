# Loan Origination Frontend (Phase 1)

Production-grade React + TypeScript + Tailwind workbench for the Go workflow engine.

## Included in this phase

- AppShell: collapsible sidebar, topbar, breadcrumbs, theme toggle
- Case List View: quick/advanced filters, virtualized table, bulk actions, pagination
- Case Detail Workbench:
  - Overview
  - Tasks
  - Documents
  - Approvals
  - Timeline / History
  - Communications
- Task Workbench modal: dynamic form generation, Zod validation, draft + complete actions
- Workbasket queue view with claim flow

## Tech

- React 18
- Vite + TypeScript
- Tailwind CSS 3
- TanStack Query
- TanStack Table + TanStack Virtual
- React Hook Form + Zod
- Zustand
- Lucide icons
- Sonner toasts
- Framer Motion

## Environment

Create `.env.local`:

```bash
VITE_API_BASE_URL=http://localhost:8080
```

## Run

```bash
npm install
npm run dev
```

## Test

```bash
npm run test
```

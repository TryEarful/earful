# Surveys are owned by Workspaces, not users

Every survey belongs to a Workspace; users are workspace members. Signup auto-creates a single-member personal workspace, so the MVP feels single-user while the schema is already multi-user. The workspace is the billable entity and the audit log's scope.

## Considered Options

- User-owned surveys with a per-survey collaborators table: lighter now, painful later (billing, member removal, ownership transfer).
- Workspace from day 1 (chosen): member invites become a UI feature, not a data migration.

## Consequences

- Member invites, roles and permissions can ship post-MVP with zero schema change; MVP hard-codes "sole member = full access".
- Billing, quotas and GDPR data-controller boundaries attach to the workspace.

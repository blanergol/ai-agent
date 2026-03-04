-- Idempotent Langfuse bootstrap for local self-hosted setup.
-- Variables are injected via psql -v:
--   ORG_ID, ORG_NAME, ORG_ROLE, PROJECT_ID, PROJECT_NAME, PROJECT_ROLE

INSERT INTO organizations (id, name, created_at, updated_at)
VALUES (:'ORG_ID', :'ORG_NAME', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT (id)
DO UPDATE SET
  name = EXCLUDED.name,
  updated_at = CURRENT_TIMESTAMP;

INSERT INTO projects (id, created_at, name, updated_at, org_id, has_traces)
VALUES (:'PROJECT_ID', CURRENT_TIMESTAMP, :'PROJECT_NAME', CURRENT_TIMESTAMP, :'ORG_ID', false)
ON CONFLICT (id)
DO UPDATE SET
  name = EXCLUDED.name,
  org_id = EXCLUDED.org_id,
  updated_at = CURRENT_TIMESTAMP;

-- Ensure every existing user is attached to the default org.
INSERT INTO organization_memberships (id, org_id, user_id, role, created_at, updated_at)
SELECT
  'om_' || substr(md5(:'ORG_ID' || ':' || u.id), 1, 24),
  :'ORG_ID',
  u.id,
  CAST(:'ORG_ROLE' AS "Role"),
  CURRENT_TIMESTAMP,
  CURRENT_TIMESTAMP
FROM users u
ON CONFLICT (org_id, user_id)
DO UPDATE SET
  role = EXCLUDED.role,
  updated_at = CURRENT_TIMESTAMP;

-- Ensure every org member is attached to the default project.
INSERT INTO project_memberships (project_id, user_id, org_membership_id, role, created_at, updated_at)
SELECT
  :'PROJECT_ID',
  om.user_id,
  om.id,
  CAST(:'PROJECT_ROLE' AS "Role"),
  CURRENT_TIMESTAMP,
  CURRENT_TIMESTAMP
FROM organization_memberships om
WHERE om.org_id = :'ORG_ID'
ON CONFLICT (project_id, user_id)
DO UPDATE SET
  org_membership_id = EXCLUDED.org_membership_id,
  role = EXCLUDED.role,
  updated_at = CURRENT_TIMESTAMP;

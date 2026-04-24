CREATE TABLE IF NOT EXISTS admin_audit_log (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_address  TEXT        NOT NULL,
    method         TEXT        NOT NULL,
    path           TEXT        NOT NULL,
    status         SMALLINT    NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_admin_audit_log_admin_created
    ON admin_audit_log (admin_address, created_at DESC);

package hermem

import "context"

// AdminClient handles administrative operations.
type AdminClient struct {
	c *Client
}

// MigrateStatus returns migration status for all embedded migrations.
func (a *AdminClient) MigrateStatus(ctx context.Context) ([]MigStatus, error) {
	var result []MigStatus
	err := a.c.doGet(ctx, "/db/migrate", &result)
	return result, err
}

// Schema returns the stored vs current schema fingerprint.
func (a *AdminClient) Schema(ctx context.Context) (*SchemaReport, error) {
	var result SchemaReport
	err := a.c.doGet(ctx, "/db/schema", &result)
	return &result, err
}

// VerifyDB runs checksum integrity check across all migrations.
func (a *AdminClient) VerifyDB(ctx context.Context) error {
	return a.c.doGet(ctx, "/db/verify", nil)
}

// Rollback rolls back the most recently applied migration.
func (a *AdminClient) Rollback(ctx context.Context) error {
	return a.c.doNoContent(ctx, "POST", "/db/rollback", nil)
}

// Health returns the health status.
func (a *AdminClient) Health(ctx context.Context) (*HealthResponse, error) {
	var result HealthResponse
	err := a.c.doGet(ctx, "/health", &result)
	return &result, err
}

// Ready returns the readiness status with dependency checks.
func (a *AdminClient) Ready(ctx context.Context) (*ReadyResponse, error) {
	var result ReadyResponse
	err := a.c.doGet(ctx, "/health/ready", &result)
	return &result, err
}

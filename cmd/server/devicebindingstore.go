package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// DeviceBinding links a physical device (identified operationally by its
// hostname) to its permanent identities in GLPI (glpi_computer_id) and Tactical
// RMM (tactical_agent_id / tactical_client_id / tactical_site_id). See
// docs/tasks/T-002 and docs/ARCHITECTURE.md.
//
// Isolation follows the existing SaaS multitenancy: owner_id/tenant_id = company
// (NOT secretaria — see D-009). Uniqueness and lookup use hostname_normalized;
// the original hostname is kept verbatim for display/audit.
type DeviceBinding struct {
	ID                 string `json:"id"`
	OwnerID            string `json:"ownerId,omitempty"`
	TenantID           string `json:"tenantId,omitempty"`
	Hostname           string `json:"hostname"`
	HostnameNormalized string `json:"hostnameNormalized"`
	TacticalAgentID    string `json:"tacticalAgentId,omitempty"`
	GLPIComputerID     string `json:"glpiComputerId,omitempty"`
	TacticalClientID   string `json:"tacticalClientId,omitempty"`
	TacticalSiteID     string `json:"tacticalSiteId,omitempty"`
	SectorCode         string `json:"sectorCode,omitempty"`
	Patrimonio         string `json:"patrimonio,omitempty"`
	MatchStatus        string `json:"matchStatus"`
	LastVerifiedAt     int64  `json:"lastVerifiedAt"`
	CreatedAt          int64  `json:"createdAt"`
	UpdatedAt          int64  `json:"updatedAt"`
}

type deviceBindingStore struct{ db *sql.DB }

// Distinguishable errors for the failure modes the store can report.
var (
	// ErrDeviceBindingNotFound is returned by Get when no row matches the id.
	ErrDeviceBindingNotFound = errors.New("device binding not found")
	// ErrInvalidHostname is returned when a hostname is blank after trimming.
	ErrInvalidHostname = errors.New("invalid hostname")
	// ErrMissingTenant is returned by tenant-scoped operations when tenantID
	// is blank; the support domain requires tenant isolation.
	ErrMissingTenant = errors.New("tenant id required")
	// ErrDeviceBindingConflict is returned when a write violates a uniqueness
	// constraint that the idempotent upsert cannot resolve (e.g. a supplied id
	// already bound to a different hostname/tenant).
	ErrDeviceBindingConflict = errors.New("device binding conflict")
	// ErrInvalidMatchStatus is returned when match_status is not one of the
	// documented states (docs/ARCHITECTURE.md, docs/tasks/T-002).
	ErrInvalidMatchStatus = errors.New("invalid match status")
)

const deviceBindingColumns = `id, owner_id, tenant_id, hostname, hostname_normalized,
	tactical_agent_id, glpi_computer_id, tactical_client_id, tactical_site_id,
	sector_code, patrimonio, match_status, last_verified_at, created_at, updated_at`

// matchStatusValues is the closed set of device match states
// (docs/ARCHITECTURE.md §match_status; docs/tasks/T-002-*.md).
var matchStatusValues = map[string]bool{
	"pending":          true,
	"matched":          true,
	"conflict":         true,
	"missing_glpi":     true,
	"missing_tactical": true,
	"disabled":         true,
}

func newDeviceBindingStore(ctx context.Context, db *sql.DB) (*deviceBindingStore, error) {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS device_bindings (
		id                  TEXT PRIMARY KEY,
		owner_id            TEXT NOT NULL DEFAULT '',
		tenant_id           TEXT NOT NULL DEFAULT '',
		hostname            TEXT NOT NULL,
		hostname_normalized TEXT NOT NULL,
		tactical_agent_id   TEXT NOT NULL DEFAULT '',
		glpi_computer_id    TEXT NOT NULL DEFAULT '',
		tactical_client_id  TEXT NOT NULL DEFAULT '',
		tactical_site_id    TEXT NOT NULL DEFAULT '',
		sector_code         TEXT NOT NULL DEFAULT '',
		patrimonio          TEXT NOT NULL DEFAULT '',
		match_status        TEXT NOT NULL DEFAULT 'pending',
		last_verified_at    INTEGER NOT NULL DEFAULT 0,
		created_at          INTEGER NOT NULL,
		updated_at          INTEGER NOT NULL
	)`); err != nil {
		return nil, err
	}
	// One binding per (tenant, hostname); enables the idempotent upsert and
	// serves as the left-prefix index for tenant-scoped hostname search.
	if _, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_device_bindings_tenant_host
		ON device_bindings (tenant_id, hostname_normalized)`); err != nil {
		return nil, err
	}
	return &deviceBindingStore{db: db}, nil
}

// Upsert inserts or idempotently updates the binding for (tenant_id,
// hostname_normalized). New rows get a generated id and created_at; on conflict
// the id and created_at are preserved. Permanent-id fields are enriched (a
// non-empty incoming value wins; a blank one never clears a stored value), so
// re-running sync is safe and GLPI/Tactical ids can be filled in over time.
func (s *deviceBindingStore) Upsert(ctx context.Context, in DeviceBinding) (DeviceBinding, error) {
	in.Hostname = strings.TrimSpace(in.Hostname)
	if in.Hostname == "" {
		return DeviceBinding{}, ErrInvalidHostname
	}
	if strings.TrimSpace(in.TenantID) == "" {
		return DeviceBinding{}, ErrMissingTenant
	}
	in.HostnameNormalized = normalizeHostname(in.Hostname)
	if in.HostnameNormalized == "" {
		return DeviceBinding{}, ErrInvalidHostname
	}
	// Remember whether the caller supplied a status BEFORE applying the insert
	// default, so an update can tell "leave as-is" (blank) apart from an
	// explicit "pending". Validate/normalize against the documented state set.
	status := strings.ToLower(strings.TrimSpace(in.MatchStatus))
	if status != "" && !matchStatusValues[status] {
		return DeviceBinding{}, ErrInvalidMatchStatus
	}
	statusProvided := status != ""
	insertStatus := status
	if insertStatus == "" {
		insertStatus = "pending"
	}
	if in.ID == "" {
		in.ID = newID()
	}
	now := time.Now().Unix()
	if in.CreatedAt == 0 {
		in.CreatedAt = now
	}
	in.UpdatedAt = now

	statusFlag := 0
	if statusProvided {
		statusFlag = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO device_bindings
		(id, owner_id, tenant_id, hostname, hostname_normalized,
		 tactical_agent_id, glpi_computer_id, tactical_client_id, tactical_site_id,
		 sector_code, patrimonio, match_status, last_verified_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(tenant_id, hostname_normalized) DO UPDATE SET
			owner_id           = CASE WHEN excluded.owner_id <> '' THEN excluded.owner_id ELSE device_bindings.owner_id END,
			hostname           = CASE WHEN excluded.hostname <> '' THEN excluded.hostname ELSE device_bindings.hostname END,
			tactical_agent_id  = CASE WHEN excluded.tactical_agent_id <> '' THEN excluded.tactical_agent_id ELSE device_bindings.tactical_agent_id END,
			glpi_computer_id   = CASE WHEN excluded.glpi_computer_id <> '' THEN excluded.glpi_computer_id ELSE device_bindings.glpi_computer_id END,
			tactical_client_id = CASE WHEN excluded.tactical_client_id <> '' THEN excluded.tactical_client_id ELSE device_bindings.tactical_client_id END,
			tactical_site_id   = CASE WHEN excluded.tactical_site_id <> '' THEN excluded.tactical_site_id ELSE device_bindings.tactical_site_id END,
			sector_code        = CASE WHEN excluded.sector_code <> '' THEN excluded.sector_code ELSE device_bindings.sector_code END,
			patrimonio         = CASE WHEN excluded.patrimonio <> '' THEN excluded.patrimonio ELSE device_bindings.patrimonio END,
			match_status       = CASE WHEN ? = 1 THEN excluded.match_status ELSE device_bindings.match_status END,
			last_verified_at   = CASE WHEN excluded.last_verified_at > 0 THEN excluded.last_verified_at ELSE device_bindings.last_verified_at END,
			updated_at         = excluded.updated_at`,
		in.ID, in.OwnerID, in.TenantID, in.Hostname, in.HostnameNormalized,
		in.TacticalAgentID, in.GLPIComputerID, in.TacticalClientID, in.TacticalSiteID,
		in.SectorCode, in.Patrimonio, insertStatus, in.LastVerifiedAt, in.CreatedAt, in.UpdatedAt,
		statusFlag)
	if err != nil {
		if isUniqueViolation(err) {
			return DeviceBinding{}, ErrDeviceBindingConflict
		}
		return DeviceBinding{}, err
	}
	return s.getByTenantHost(ctx, in.TenantID, in.HostnameNormalized)
}

// Get returns a binding by its primary id (globally unique).
func (s *deviceBindingStore) Get(ctx context.Context, id string) (DeviceBinding, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+deviceBindingColumns+`
		FROM device_bindings WHERE id = ?`, id)
	b, err := scanDeviceBinding(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DeviceBinding{}, ErrDeviceBindingNotFound
		}
		return DeviceBinding{}, err
	}
	return b, nil
}

// GetForTenant returns a binding by its primary id within the specified tenant.
// If the binding does not exist or belongs to another tenant, it returns ErrDeviceBindingNotFound.
func (s *deviceBindingStore) GetForTenant(ctx context.Context, tenantID, id string) (DeviceBinding, error) {
	if strings.TrimSpace(tenantID) == "" {
		return DeviceBinding{}, ErrMissingTenant
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+deviceBindingColumns+`
		FROM device_bindings WHERE id = ? AND tenant_id = ?`, id, tenantID)
	b, err := scanDeviceBinding(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DeviceBinding{}, ErrDeviceBindingNotFound
		}
		return DeviceBinding{}, err
	}
	return b, nil
}

// FindByHostname resolves a binding within a tenant by exact normalized
// hostname. The bool reports presence; a valid-but-absent hostname returns
// (zero, false, nil).
func (s *deviceBindingStore) FindByHostname(ctx context.Context, tenantID, hostname string) (DeviceBinding, bool, error) {
	if strings.TrimSpace(tenantID) == "" {
		return DeviceBinding{}, false, ErrMissingTenant
	}
	if strings.TrimSpace(hostname) == "" {
		return DeviceBinding{}, false, ErrInvalidHostname
	}
	b, err := s.getByTenantHost(ctx, tenantID, normalizeHostname(hostname))
	if err != nil {
		if errors.Is(err, ErrDeviceBindingNotFound) {
			return DeviceBinding{}, false, nil
		}
		return DeviceBinding{}, false, err
	}
	return b, true, nil
}

// Search returns the tenant's bindings whose normalized hostname starts with
// the normalized query, ordered by hostname. An empty query lists all bindings
// for the tenant.
func (s *deviceBindingStore) Search(ctx context.Context, tenantID, query string) ([]DeviceBinding, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, ErrMissingTenant
	}
	pattern := escapeLikePrefix(normalizeHostname(query)) + "%"
	rows, err := s.db.QueryContext(ctx, `SELECT `+deviceBindingColumns+`
		FROM device_bindings
		WHERE tenant_id = ? AND hostname_normalized LIKE ? ESCAPE '\'
		ORDER BY hostname_normalized ASC`, tenantID, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeviceBinding{}
	for rows.Next() {
		b, err := scanDeviceBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *deviceBindingStore) getByTenantHost(ctx context.Context, tenantID, normalized string) (DeviceBinding, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+deviceBindingColumns+`
		FROM device_bindings WHERE tenant_id = ? AND hostname_normalized = ?`, tenantID, normalized)
	b, err := scanDeviceBinding(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return DeviceBinding{}, ErrDeviceBindingNotFound
		}
		return DeviceBinding{}, err
	}
	return b, nil
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanDeviceBinding(sc scanner) (DeviceBinding, error) {
	var b DeviceBinding
	err := sc.Scan(&b.ID, &b.OwnerID, &b.TenantID, &b.Hostname, &b.HostnameNormalized,
		&b.TacticalAgentID, &b.GLPIComputerID, &b.TacticalClientID, &b.TacticalSiteID,
		&b.SectorCode, &b.Patrimonio, &b.MatchStatus, &b.LastVerifiedAt, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return DeviceBinding{}, err
	}
	return b, nil
}

// escapeLikePrefix escapes LIKE wildcards so a search term is matched
// literally (paired with ESCAPE '\'). Only used to build a prefix pattern.
func escapeLikePrefix(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// isUniqueViolation reports whether err is a UNIQUE/duplicate-key constraint
// error from SQLite (modernc) or MariaDB. Kept text-based because the drivers
// do not expose a portable typed error.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "constraint failed") ||
		strings.Contains(msg, "duplicate entry")
}

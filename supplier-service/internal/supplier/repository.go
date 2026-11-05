package supplier

import "context"

// Repository is the persistence boundary for suppliers. The Postgres
// implementation lives in internal/repository; the service layer only
// depends on this interface so it can be unit-tested without a database.
type Repository interface {
	Create(ctx context.Context, s *Supplier) error
	GetByID(ctx context.Context, id string) (*Supplier, error)
	List(ctx context.Context, filter ListFilter) ([]*Supplier, error)
	Update(ctx context.Context, s *Supplier) error
	SoftDelete(ctx context.Context, id string) error
	ExistsDuplicate(ctx context.Context, name, building, locationDescription, excludeID string) (bool, error)
	Count(ctx context.Context) (int, error)

	// The audited writes save the supplier change and its audit row in one
	// transaction, so either both are saved or neither is (D-108).
	CreateAudited(ctx context.Context, s *Supplier, e *AuditEntry) error
	UpdateAudited(ctx context.Context, s *Supplier, e *AuditEntry) error
	SoftDeleteAudited(ctx context.Context, id string, e *AuditEntry) error

	// RecordAudit writes one audit row on its own. It is for an attempt that
	// did not commit, so there was no transaction to share (D-109).
	RecordAudit(ctx context.Context, e *AuditEntry) error
	ListAudit(ctx context.Context, f AuditFilter) (*AuditPage, error)
	// PurgeAudit deletes the audit rows older than 6 months and returns how
	// many it removed (D-115).
	PurgeAudit(ctx context.Context) (int64, error)
}

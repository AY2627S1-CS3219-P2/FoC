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
}

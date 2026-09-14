package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"foc/supplier-service/internal/supplier"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const supplierColumns = `id, name, type, building, floor, location_description,
	latitude, longitude, opening_time, closing_time, image_url, description,
	is_available, created_at, updated_at, deleted_at`

func scanSupplier(row pgx.Row) (*supplier.Supplier, error) {
	var s supplier.Supplier
	err := row.Scan(
		&s.ID, &s.Name, &s.Type, &s.Building, &s.Floor, &s.LocationDescription,
		&s.Latitude, &s.Longitude, &s.OpeningTime, &s.ClosingTime, &s.ImageURL, &s.Description,
		&s.IsAvailable, &s.CreatedAt, &s.UpdatedAt, &s.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *PostgresRepository) Create(ctx context.Context, s *supplier.Supplier) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO suppliers (id, name, type, building, floor, location_description,
			latitude, longitude, opening_time, closing_time, image_url, description,
			is_available, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		s.ID, s.Name, s.Type, s.Building, s.Floor, s.LocationDescription,
		s.Latitude, s.Longitude, s.OpeningTime, s.ClosingTime, s.ImageURL, s.Description,
		s.IsAvailable, s.CreatedAt, s.UpdatedAt,
	)
	return err
}

func (r *PostgresRepository) GetByID(ctx context.Context, id string) (*supplier.Supplier, error) {
	row := r.pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT %s FROM suppliers WHERE id = $1 AND deleted_at IS NULL`, supplierColumns), id)
	s, err := scanSupplier(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *PostgresRepository) List(ctx context.Context, filter supplier.ListFilter) ([]*supplier.Supplier, error) {
	query := fmt.Sprintf(`SELECT %s FROM suppliers WHERE deleted_at IS NULL`, supplierColumns)
	var args []any
	if filter.Category != "" {
		args = append(args, filter.Category)
		query += fmt.Sprintf(" AND type ILIKE $%d", len(args))
	}
	if filter.Search != "" {
		args = append(args, "%"+strings.ToLower(filter.Search)+"%")
		query += fmt.Sprintf(" AND lower(name) LIKE $%d", len(args))
	}
	query += " ORDER BY name ASC"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*supplier.Supplier
	for rows.Next() {
		s, err := scanSupplier(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) Update(ctx context.Context, s *supplier.Supplier) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE suppliers SET
			name = $1, type = $2, building = $3, floor = $4, location_description = $5,
			latitude = $6, longitude = $7, opening_time = $8, closing_time = $9,
			image_url = $10, description = $11, is_available = $12, updated_at = $13
		WHERE id = $14 AND deleted_at IS NULL`,
		s.Name, s.Type, s.Building, s.Floor, s.LocationDescription,
		s.Latitude, s.Longitude, s.OpeningTime, s.ClosingTime,
		s.ImageURL, s.Description, s.IsAvailable, s.UpdatedAt, s.ID,
	)
	return err
}

func (r *PostgresRepository) SoftDelete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE suppliers SET deleted_at = now(), is_available = false, updated_at = now()
		WHERE id = $1 AND deleted_at IS NULL`, id)
	return err
}

func (r *PostgresRepository) ExistsDuplicate(ctx context.Context, name, building, locationDescription, excludeID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM suppliers
			WHERE deleted_at IS NULL
				AND lower(name) = lower($1)
				AND lower(building) = lower($2)
				AND lower(location_description) = lower($3)
				AND id::text != $4
		)`, name, building, locationDescription, excludeID).Scan(&exists)
	return exists, err
}

func (r *PostgresRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM suppliers`).Scan(&count)
	return count, err
}

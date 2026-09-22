// Package seed loads the campus suppliers shipped in the template
// repository's data/csv/supplier-seed-data.csv into the Supplier
// Service's own database (FR F2.3), the first time the service starts
// against an empty table.
package seed

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"

	"foc/supplier-service/internal/supplier"
)

// csv columns: Name,Type,Building,Floor,Location Description,Latitude,Longitude,StartingTime,ClosingTime,ImageURL
const expectedColumns = 10

// FromCSV parses the seed CSV into domain suppliers. The file is encoded
// as Windows-1252 (it contains a curly apostrophe outside the ASCII/UTF-8
// range), so it's decoded through charmap before hitting the CSV reader.
func FromCSV(path string) ([]*supplier.Supplier, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open seed csv: %w", err)
	}
	defer f.Close()

	reader := csv.NewReader(transform.NewReader(f, charmap.Windows1252.NewDecoder()))
	reader.FieldsPerRecord = -1

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read seed csv header: %w", err)
	}
	if len(header) < expectedColumns {
		return nil, fmt.Errorf("seed csv has %d columns, expected at least %d", len(header), expectedColumns)
	}

	var out []*supplier.Supplier
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read seed csv row: %w", err)
		}
		if len(strings.TrimSpace(strings.Join(record, ""))) == 0 {
			continue
		}

		s, err := rowToSupplier(record)
		if err != nil {
			return nil, fmt.Errorf("parse seed csv row %v: %w", record, err)
		}
		out = append(out, s)
	}
	return out, nil
}

func rowToSupplier(record []string) (*supplier.Supplier, error) {
	get := func(i int) string {
		if i < len(record) {
			return strings.TrimSpace(record[i])
		}
		return ""
	}

	lat, err := strconv.ParseFloat(get(5), 64)
	if err != nil {
		return nil, fmt.Errorf("latitude: %w", err)
	}
	lng, err := strconv.ParseFloat(get(6), 64)
	if err != nil {
		return nil, fmt.Errorf("longitude: %w", err)
	}

	return &supplier.Supplier{
		Name:                get(0),
		Type:                get(1),
		Building:            get(2),
		Floor:               get(3),
		LocationDescription: get(4),
		Latitude:            lat,
		Longitude:           lng,
		OpeningTime:         normalizeHours(get(7)),
		ClosingTime:         normalizeHours(get(8)),
		ImageURL:            get(9),
		IsAvailable:         true,
	}, nil
}

// normalizeHours converts the CSV's "0900hrs" style values into "HH:MM".
func normalizeHours(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.TrimSuffix(v, "hrs")
	if len(v) != 4 {
		return ""
	}
	return v[:2] + ":" + v[2:]
}

// EnsureSeeded loads the CSV and inserts every row whose (name, building,
// location_description) doesn't already exist, so it's safe to call on
// every startup without duplicating rows across restarts.
func EnsureSeeded(ctx context.Context, repo supplier.Repository, csvPath string) error {
	count, err := repo.Count(ctx)
	if err != nil {
		return fmt.Errorf("count existing suppliers: %w", err)
	}
	if count > 0 {
		return nil
	}

	suppliers, err := FromCSV(csvPath)
	if err != nil {
		return err
	}

	svc := supplier.NewService(repo)
	for _, s := range suppliers {
		if _, err := svc.Create(ctx, s); err != nil {
			return fmt.Errorf("seed %q: %w", s.Name, err)
		}
	}
	return nil
}

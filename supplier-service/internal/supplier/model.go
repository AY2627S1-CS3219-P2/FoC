package supplier

import "time"

// Supplier is a campus store, facility, or landmark that can be selected
// as the pickup location of an errand request (FR F2.1.1).
type Supplier struct {
	ID                  string
	Name                string
	Type                string
	Building            string
	Floor               string
	LocationDescription string
	Latitude            float64
	Longitude           float64
	OpeningTime         string // "HH:MM", 24h
	ClosingTime         string // "HH:MM", 24h
	ImageURL            string
	Description         string
	IsAvailable         bool
	CreatedAt           time.Time
	UpdatedAt           time.Time
	DeletedAt           *time.Time
}

// ListFilter narrows a supplier listing by category and/or a name search
// term (FR F2.1.3).
type ListFilter struct {
	Category string
	Search   string
}

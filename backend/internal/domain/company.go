package domain

import "time"

// CompanyRecord is the durable company registry row used by storage and
// services. A company groups multiple git-repo projects (e.g. an org with
// several product repos); ID is a slug, mirroring ProjectRecord.ID.
type CompanyRecord struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

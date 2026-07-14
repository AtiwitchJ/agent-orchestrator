package company

import "time"

// Company is the read-model returned by the /api/v1/companies surface — both
// GET (list) and POST (create) use this one shape, mirroring the durable
// CompanyRecord with no derived fields.
type Company struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// CreateInput is the body shape for POST /api/v1/companies. ID is derived from
// Name (slugified, with a numeric suffix on collision) — callers do not choose it.
type CreateInput struct {
	Name string `json:"name"`
}

// AssignProjectInput is the body shape for PUT /api/v1/projects/{id}/company.
// CompanyID must name an existing company; "" unassigns the project's company.
type AssignProjectInput struct {
	CompanyID string `json:"companyId"`
}

// DeleteResult is the body shape for DELETE /api/v1/companies/{id}.
type DeleteResult struct {
	Deleted bool `json:"deleted"`
}

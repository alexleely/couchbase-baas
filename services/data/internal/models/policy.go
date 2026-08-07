package models

type Policy struct {
	ID         string `json:"id"`
	Scope      string `json:"scope"`
	Collection string `json:"collection"`
	Action     string `json:"action"`     // "SELECT", "UPDATE", "DELETE"
	Expression string `json:"expression"` // e.g. "owner_id = $uid" or "role = $role"
}

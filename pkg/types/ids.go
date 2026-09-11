package types

// Typed string IDs. Postgres generates the underlying UUIDs
// (gen_random_uuid()); these types exist only so a caller can't pass an
// OrgID where an AppID is expected and have the compiler stay silent.
type (
	OrgID    string
	UserID   string
	AppID    string
	APIKeyID string
)

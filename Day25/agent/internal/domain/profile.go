package domain

// UserProfile stores the explicit user identity and stable preferences
// that the agent injects into every request.
type UserProfile struct {
	Name        string            `json:"name,omitempty"`
	Preferences map[string]string `json:"preferences"`
	UpdatedAt   string            `json:"updated_at,omitempty"`
}

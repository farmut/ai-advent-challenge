package usecase

import (
	"fmt"
	"strings"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// ProfileSystemBlock formats the user profile as a system-message prefix block.
// Returns an empty string when the profile has neither a name nor any preferences.
func ProfileSystemBlock(p domain.UserProfile) string {
	if p.Name == "" && len(p.Preferences) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[User Profile]\n")
	if p.Name != "" {
		fmt.Fprintf(&sb, "name: %s\n", p.Name)
	}
	if len(p.Preferences) > 0 {
		sb.WriteString("preferences:\n")
		for _, k := range sortedKeys(p.Preferences) {
			fmt.Fprintf(&sb, "  %s: %s\n", k, p.Preferences[k])
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ProfileUseCase manages explicit user profile operations.
type ProfileUseCase struct {
	repo port.UserProfileRepository
}

// NewProfileUseCase creates a ProfileUseCase backed by the given repository.
func NewProfileUseCase(repo port.UserProfileRepository) *ProfileUseCase {
	return &ProfileUseCase{repo: repo}
}

// SetName updates the user name in the profile.
func (uc *ProfileUseCase) SetName(name string) error {
	p, err := uc.repo.Load()
	if err != nil {
		return err
	}
	p.Name = name
	return uc.repo.Save(p)
}

// Set adds or updates preference key with value.
func (uc *ProfileUseCase) Set(key, value string) error {
	p, err := uc.repo.Load()
	if err != nil {
		return err
	}
	p.Preferences[key] = value
	return uc.repo.Save(p)
}

// Delete removes a preference by key. Silently succeeds if the key does not exist.
func (uc *ProfileUseCase) Delete(key string) error {
	p, err := uc.repo.Load()
	if err != nil {
		return err
	}
	delete(p.Preferences, key)
	return uc.repo.Save(p)
}

// Get returns the value for key and whether it was found.
func (uc *ProfileUseCase) Get(key string) (string, bool, error) {
	p, err := uc.repo.Load()
	if err != nil {
		return "", false, err
	}
	v, ok := p.Preferences[key]
	return v, ok, nil
}

// Profile returns the full current profile.
func (uc *ProfileUseCase) Profile() (domain.UserProfile, error) {
	return uc.repo.Load()
}

package usecase_test

import (
	"strings"
	"testing"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/usecase"
)

// ---- ProfileSystemBlock ----

func TestProfileSystemBlock_Empty(t *testing.T) {
	p := domain.UserProfile{Preferences: map[string]string{}}
	if block := usecase.ProfileSystemBlock(p); block != "" {
		t.Errorf("empty profile should produce empty block, got %q", block)
	}
}

func TestProfileSystemBlock_NameOnly(t *testing.T) {
	p := domain.UserProfile{Name: "Alice", Preferences: map[string]string{}}
	block := usecase.ProfileSystemBlock(p)
	if !strings.Contains(block, "[User Profile]") {
		t.Error("block should contain [User Profile] header")
	}
	if !strings.Contains(block, "name: Alice") {
		t.Error("block should contain name")
	}
	if strings.Contains(block, "preferences:") {
		t.Error("block should not contain preferences section when empty")
	}
}

func TestProfileSystemBlock_PreferencesOnly(t *testing.T) {
	p := domain.UserProfile{Preferences: map[string]string{
		"style":  "concise",
		"format": "markdown",
	}}
	block := usecase.ProfileSystemBlock(p)
	if !strings.Contains(block, "[User Profile]") {
		t.Error("block should contain [User Profile] header")
	}
	if strings.Contains(block, "name:") {
		t.Error("block should not contain name line when name is empty")
	}
	if !strings.Contains(block, "preferences:") {
		t.Error("block should contain preferences section")
	}
	if !strings.Contains(block, "style: concise") {
		t.Error("block should contain style preference")
	}
	if !strings.Contains(block, "format: markdown") {
		t.Error("block should contain format preference")
	}
}

func TestProfileSystemBlock_NameAndPreferences(t *testing.T) {
	p := domain.UserProfile{
		Name: "Bob",
		Preferences: map[string]string{
			"language": "russian",
		},
	}
	block := usecase.ProfileSystemBlock(p)
	if !strings.Contains(block, "name: Bob") {
		t.Error("block should contain name")
	}
	if !strings.Contains(block, "language: russian") {
		t.Error("block should contain language preference")
	}
}

func TestProfileSystemBlock_PreferencesSorted(t *testing.T) {
	p := domain.UserProfile{Preferences: map[string]string{
		"zebra":  "z",
		"alpha":  "a",
		"medium": "m",
	}}
	block := usecase.ProfileSystemBlock(p)
	lines := strings.Split(block, "\n")
	// Expected: [User Profile], preferences:, alpha, medium, zebra
	var prefLines []string
	inPrefs := false
	for _, l := range lines {
		if strings.TrimSpace(l) == "preferences:" {
			inPrefs = true
			continue
		}
		if inPrefs {
			prefLines = append(prefLines, strings.TrimSpace(l))
		}
	}
	if len(prefLines) < 3 {
		t.Fatalf("expected 3 preference lines, got %d: %q", len(prefLines), block)
	}
	if !strings.HasPrefix(prefLines[0], "alpha:") {
		t.Errorf("first pref should be alpha, got: %q", prefLines[0])
	}
	if !strings.HasPrefix(prefLines[1], "medium:") {
		t.Errorf("second pref should be medium, got: %q", prefLines[1])
	}
	if !strings.HasPrefix(prefLines[2], "zebra:") {
		t.Errorf("third pref should be zebra, got: %q", prefLines[2])
	}
}

// ---- ProfileUseCase ----

// inMemoryProfileRepo is a simple in-memory implementation of port.UserProfileRepository for tests.
type inMemoryProfileRepo struct {
	profile domain.UserProfile
}

func newInMemoryProfileRepo() *inMemoryProfileRepo {
	return &inMemoryProfileRepo{
		profile: domain.UserProfile{Preferences: make(map[string]string)},
	}
}

func (r *inMemoryProfileRepo) Load() (domain.UserProfile, error) {
	return r.profile, nil
}

func (r *inMemoryProfileRepo) Save(p domain.UserProfile) error {
	r.profile = p
	return nil
}

func TestProfileUseCase_SetName(t *testing.T) {
	uc := usecase.NewProfileUseCase(newInMemoryProfileRepo())
	if err := uc.SetName("Alice"); err != nil {
		t.Fatalf("SetName failed: %v", err)
	}
	p, err := uc.Profile()
	if err != nil {
		t.Fatalf("Profile failed: %v", err)
	}
	if p.Name != "Alice" {
		t.Errorf("Name = %q, want Alice", p.Name)
	}
}

func TestProfileUseCase_Set(t *testing.T) {
	uc := usecase.NewProfileUseCase(newInMemoryProfileRepo())
	if err := uc.Set("style", "concise"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	v, ok, err := uc.Get("style")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !ok {
		t.Error("expected key to exist after Set")
	}
	if v != "concise" {
		t.Errorf("value = %q, want concise", v)
	}
}

func TestProfileUseCase_Set_Overwrite(t *testing.T) {
	uc := usecase.NewProfileUseCase(newInMemoryProfileRepo())
	_ = uc.Set("style", "verbose")
	if err := uc.Set("style", "concise"); err != nil {
		t.Fatalf("Set overwrite failed: %v", err)
	}
	v, _, _ := uc.Get("style")
	if v != "concise" {
		t.Errorf("overwrite: value = %q, want concise", v)
	}
}

func TestProfileUseCase_Delete(t *testing.T) {
	uc := usecase.NewProfileUseCase(newInMemoryProfileRepo())
	_ = uc.Set("style", "concise")
	if err := uc.Delete("style"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, ok, _ := uc.Get("style")
	if ok {
		t.Error("expected key to be absent after Delete")
	}
}

func TestProfileUseCase_Delete_MissingKey(t *testing.T) {
	uc := usecase.NewProfileUseCase(newInMemoryProfileRepo())
	if err := uc.Delete("nonexistent"); err != nil {
		t.Errorf("Delete of missing key should not error, got: %v", err)
	}
}

func TestProfileUseCase_Get_Missing(t *testing.T) {
	uc := usecase.NewProfileUseCase(newInMemoryProfileRepo())
	_, ok, err := uc.Get("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Error("expected ok=false for missing key")
	}
}

func TestProfileUseCase_MultiplePreferences(t *testing.T) {
	uc := usecase.NewProfileUseCase(newInMemoryProfileRepo())
	_ = uc.Set("style", "concise")
	_ = uc.Set("format", "markdown")
	_ = uc.Set("language", "russian")
	p, err := uc.Profile()
	if err != nil {
		t.Fatalf("Profile failed: %v", err)
	}
	if len(p.Preferences) != 3 {
		t.Errorf("expected 3 preferences, got %d", len(p.Preferences))
	}
}

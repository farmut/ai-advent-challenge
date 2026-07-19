package app

import (
	"ai-adv-agent/internal/adapter/storage"
	"ai-adv-agent/internal/config"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase"
)

// MemoryFactory builds memory-backed ChatUseCases from the config's memory
// section. It resolves every layer's file path once and honours the per-layer
// enable toggles: a disabled layer is wired to an empty ""-path file repo, which
// loads empty and saves nothing, so its system block is never injected.
//
// Two flavours are produced:
//   - Shared: file-backed memory for the orchestrator's own dialogue (STM, WM,
//     LTM, facts, profile, task memory persist across runs).
//   - Ephemeral: a sub-agent's scratch memory — in-memory STM+WM seeded with the
//     orchestrator-supplied context, plus read-only access to the shared LTM and
//     profile. Nothing a sub-agent does touches disk except via the orchestrator.
type MemoryFactory struct {
	mc   config.MemoryConfig
	base string

	// Shared, file-backed repositories (constructed once). Disabled layers use a
	// ""-path repo that reads empty and never writes.
	history  port.HistoryRepository
	stats    port.StatsRepository
	summary  port.SummaryRepository
	facts    port.FactsRepository
	wm       port.WorkingMemoryRepository
	ltm      port.LongTermMemoryRepository
	profile  port.UserProfileRepository
	taskMem  port.TaskMemoryRepository
	profileP string
}

// NewMemoryFactory resolves all memory paths from the config and constructs the
// shared repositories.
func NewMemoryFactory(mc config.MemoryConfig) *MemoryFactory {
	base := mc.Dir
	if mc.STM.History != "" {
		base = mc.STM.History
	}

	profilePath := ""
	if mc.Profile.Enabled {
		profilePath = mc.Profile.Path
		if profilePath == "" {
			profilePath = storage.ProfilePath(base)
		}
	}

	return &MemoryFactory{
		mc:       mc,
		base:     base,
		history:  storage.NewHistoryFile(cond(mc.STM.Enabled, base)),
		stats:    storage.NewStatsFile(cond(mc.STM.Enabled, storage.StatsPath(base))),
		summary:  storage.NewSummaryFile(cond(mc.STM.Enabled, storage.SummaryPath(base))),
		facts:    storage.NewFactsFile(cond(mc.Facts.Enabled, storage.FactsPath(base))),
		wm:       storage.NewWorkingMemoryFile(cond(mc.WM.Enabled, storage.WMPath(base))),
		ltm:      storage.NewLongTermMemoryFile(cond(mc.LTM.Enabled, storage.LTMPath(base))),
		profile:  storage.NewProfileFile(profilePath),
		taskMem:  storage.NewTaskMemoryFile(cond(mc.TaskMemory.Enabled, storage.TaskMemoryPath(base))),
		profileP: profilePath,
	}
}

// cond returns path when enabled, else "" (an empty path = no-op repo).
func cond(enabled bool, path string) string {
	if enabled {
		return path
	}
	return ""
}

// Accessors to the shared repositories, used by the orchestrator to read memory
// blocks and persist its own dialogue turns.
func (f *MemoryFactory) History() port.HistoryRepository       { return f.history }
func (f *MemoryFactory) WM() port.WorkingMemoryRepository      { return f.wm }
func (f *MemoryFactory) LTM() port.LongTermMemoryRepository    { return f.ltm }
func (f *MemoryFactory) Profile() port.UserProfileRepository   { return f.profile }
func (f *MemoryFactory) TaskMemory() port.TaskMemoryRepository { return f.taskMem }
func (f *MemoryFactory) Config() config.MemoryConfig           { return f.mc }

// SharedChat builds the orchestrator's file-backed ChatUseCase over the shared
// repositories.
func (f *MemoryFactory) SharedChat(llm port.LLMClient) *usecase.ChatUseCase {
	return usecase.NewChatUseCase(llm, f.history, f.stats, f.summary, f.facts, f.wm, f.ltm, f.profile)
}

// EphemeralChat builds a sub-agent's scratch ChatUseCase: in-memory STM seeded
// with seed, in-memory WM, read-only shared LTM, and the shared profile.
// wmEnabled reflects the role's WM toggle ANDed with the global switch; it is
// kept for API clarity — both paths use an in-memory WM that never persists.
func (f *MemoryFactory) EphemeralChat(llm port.LLMClient, seed []domain.Message, wmEnabled bool) *usecase.ChatUseCase {
	ltm := port.LongTermMemoryRepository(storage.NewMemLongTermMemory())
	if f.mc.LTM.Enabled {
		ltm = storage.NewReadOnlyLTM(f.ltm)
	}

	profile := port.UserProfileRepository(storage.NewProfileFile(""))
	if f.mc.Profile.Enabled {
		profile = storage.NewProfileFile(f.profileP)
	}

	_ = wmEnabled

	return usecase.NewChatUseCase(
		llm,
		storage.NewMemHistory(seed),
		storage.NewMemStats(),
		storage.NewMemSummary(),
		storage.NewMemFacts(),
		storage.NewMemWorkingMemory(),
		ltm,
		profile,
	)
}

// WMEnabledFor reports whether a sub-agent role should keep working memory,
// given the role toggle and the global memory.wm switch.
func (f *MemoryFactory) WMEnabledFor(roleWM bool) bool {
	return roleWM && f.mc.WM.Enabled
}

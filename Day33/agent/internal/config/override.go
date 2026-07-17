package config

// Overrides carries values from explicitly-set CLI flags. Every field is a
// pointer: nil means "flag not set, keep the config/default value", non-nil means
// "the user passed this flag, it wins". main builds this from flag.Visit so only
// flags the user actually typed override the YAML — giving flags top priority
// without silently clobbering config with flag zero-defaults.
type Overrides struct {
	// LLM
	Model         *string
	MaxTokens     *int
	Temperature   *float64
	ContextWindow *int
	CACert        *string

	// Memory
	Dir               *string
	STMEnabled        *bool
	STMLimit          *int
	STMSummary        *bool
	STMStrategy       *string
	STMWindowSize     *int
	WMEnabled         *bool
	LTMEnabled        *bool
	FactsEnabled      *bool
	ProfileEnabled    *bool
	ProfilePath       *string
	TaskMemoryEnabled *bool
	AutoUpdate        *bool

	// Invariants
	InvariantsEnabled *bool
	InvariantsPath    *string

	// RAG
	RAGEnabled     *bool
	RAGDB          *string
	RAGEmbedURL    *string
	RAGEmbedModel  *string
	RAGEmbedKey    *string
	RAGTopK        *int
	RAGThreshold   *float64
	RAGTopKFinal   *int
	RerankEnabled  *bool
	RerankModel    *string
	RerankMode     *string
	RerankProvider *string
	RerankURL      *string
	RerankKey      *string

	// MCP
	MCPEnabled *bool
	MCPFile    *string

	// Orchestrator
	OrchestratorEnabled *bool
}

// Apply mutates cfg in place, overwriting only the fields whose override pointer
// is non-nil.
func (o Overrides) Apply(cfg *Config) {
	setStr := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	setInt := func(dst *int, v *int) {
		if v != nil {
			*dst = *v
		}
	}
	setBool := func(dst *bool, v *bool) {
		if v != nil {
			*dst = *v
		}
	}
	setFloat := func(dst *float64, v *float64) {
		if v != nil {
			*dst = *v
		}
	}

	// LLM
	setStr(&cfg.LLM.Model, o.Model)
	setInt(&cfg.LLM.MaxTokens, o.MaxTokens)
	if o.Temperature != nil {
		t := *o.Temperature
		cfg.LLM.Temperature = &t
	}
	setInt(&cfg.LLM.ContextWindow, o.ContextWindow)
	setStr(&cfg.LLM.CACert, o.CACert)

	// Memory
	setStr(&cfg.Memory.Dir, o.Dir)
	setBool(&cfg.Memory.STM.Enabled, o.STMEnabled)
	setInt(&cfg.Memory.STM.Limit, o.STMLimit)
	setBool(&cfg.Memory.STM.Summary, o.STMSummary)
	setStr(&cfg.Memory.STM.Strategy, o.STMStrategy)
	setInt(&cfg.Memory.STM.WindowSize, o.STMWindowSize)
	setBool(&cfg.Memory.WM.Enabled, o.WMEnabled)
	setBool(&cfg.Memory.LTM.Enabled, o.LTMEnabled)
	setBool(&cfg.Memory.Facts.Enabled, o.FactsEnabled)
	setBool(&cfg.Memory.Profile.Enabled, o.ProfileEnabled)
	setStr(&cfg.Memory.Profile.Path, o.ProfilePath)
	setBool(&cfg.Memory.TaskMemory.Enabled, o.TaskMemoryEnabled)
	setBool(&cfg.Memory.AutoUpdate, o.AutoUpdate)

	// Invariants
	setBool(&cfg.Invariants.Enabled, o.InvariantsEnabled)
	setStr(&cfg.Invariants.Path, o.InvariantsPath)

	// RAG
	setBool(&cfg.RAG.Enabled, o.RAGEnabled)
	setStr(&cfg.RAG.DB, o.RAGDB)
	setStr(&cfg.RAG.EmbedURL, o.RAGEmbedURL)
	setStr(&cfg.RAG.EmbedModel, o.RAGEmbedModel)
	setStr(&cfg.RAG.EmbedKey, o.RAGEmbedKey)
	setInt(&cfg.RAG.TopK, o.RAGTopK)
	setFloat(&cfg.RAG.Threshold, o.RAGThreshold)
	setInt(&cfg.RAG.TopKFinal, o.RAGTopKFinal)
	setBool(&cfg.RAG.Rerank.Enabled, o.RerankEnabled)
	setStr(&cfg.RAG.Rerank.Model, o.RerankModel)
	setStr(&cfg.RAG.Rerank.Mode, o.RerankMode)
	setStr(&cfg.RAG.Rerank.Provider, o.RerankProvider)
	setStr(&cfg.RAG.Rerank.URL, o.RerankURL)
	setStr(&cfg.RAG.Rerank.Key, o.RerankKey)

	// MCP
	setBool(&cfg.MCP.Enabled, o.MCPEnabled)
	setStr(&cfg.MCP.File, o.MCPFile)

	// Orchestrator
	setBool(&cfg.Orchestrator.Enabled, o.OrchestratorEnabled)
}

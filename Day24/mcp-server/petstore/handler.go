package petstore

// Handler bundles a petstore HTTP Client with a background Collector.
// One Handler is created per MCP server process and shared across all tool calls,
// which is what allows the Collector goroutine to survive between calls.
type Handler struct {
	client    *Client
	collector *Collector
}

// NewHandler creates a Handler ready to use.
func NewHandler(c *Client) *Handler {
	return &Handler{client: c, collector: NewCollector()}
}

// Client returns the underlying HTTP client (used by tests).
func (h *Handler) Client() *Client { return h.client }

// CallTool dispatches a tool call.  Collector-managed tools are handled here;
// all other tools delegate to the package-level CallTool function.
func (h *Handler) CallTool(name string, args map[string]interface{}) (string, error) {
	switch name {

	case "report_start_collection":
		reportFile, err := strArg(args, "report_file")
		if err != nil {
			return "", err
		}
		intervalSec, err := intArg(args, "interval_seconds")
		if err != nil {
			return "", err
		}
		return h.collector.Start(h.client, reportFile, int(intervalSec))

	case "report_stop_collection":
		return h.collector.Stop()

	case "report_collection_status":
		return h.collector.Status(), nil

	default:
		return CallTool(h.client, name, args)
	}
}

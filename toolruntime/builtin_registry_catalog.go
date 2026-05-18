package toolruntime

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/toolruntime/archive"
	"github.com/fulcrus/hopclaw/toolruntime/calendar"
	"github.com/fulcrus/hopclaw/toolruntime/crypto"
	"github.com/fulcrus/hopclaw/toolruntime/db"
	"github.com/fulcrus/hopclaw/toolruntime/document"
	execpkg "github.com/fulcrus/hopclaw/toolruntime/exec"
	mediagentools "github.com/fulcrus/hopclaw/toolruntime/mediagen"
	netpkg "github.com/fulcrus/hopclaw/toolruntime/net"
	"github.com/fulcrus/hopclaw/toolruntime/pdf"
	"github.com/fulcrus/hopclaw/toolruntime/presentation"
	procpkg "github.com/fulcrus/hopclaw/toolruntime/proc"
	spreadsheetpkg "github.com/fulcrus/hopclaw/toolruntime/spreadsheet"
	"github.com/fulcrus/hopclaw/toolruntime/text"
	"github.com/fulcrus/hopclaw/toolruntime/vision"
)

func builtinCategory(name, description string, order int, load func(BuiltinsConfig) []builtinToolDef) builtinCategoryDescriptor {
	return builtinCategoryDescriptor{
		Name:        name,
		Description: description,
		Order:       order,
		Load:        load,
	}
}

func registerBuiltinCategories() error {
	specs := []builtinCategoryDescriptor{
		builtinCategory("core", "Workspace and filesystem primitives.", builtinCategoryOrderCore, coreToolDefs),
		builtinCategory("exec", "Command execution and shell tools.", builtinCategoryOrderCore, execToolDefsFromSubpkg),
		builtinCategory("a2ui", "Agent-to-UI canvas interaction and app surface tools.", builtinCategoryOrderDefault, a2uiToolDefs),
		builtinCategory("agent", "Sub-agent lifecycle and delegation tools.", builtinCategoryOrderDefault, agentToolDefs),
		builtinCategory("archive", "Archive inspection and extraction tools.", builtinCategoryOrderDefault, archiveToolDefsFromSubpkg),
		builtinCategory("automation", "Structured automation and workflow execution tools.", builtinCategoryOrderDefault, automationToolDefs),
		builtinCategory("browser", "Remote browser navigation and page interaction tools.", builtinCategoryOrderDefault, browserToolDefs),
		builtinCategory("calendar_ics", "Calendar import and ICS processing tools.", builtinCategoryOrderDefault, calendarToolDefsFromSubpkg),
		builtinCategory("canvas", "Canvas document and board manipulation tools.", builtinCategoryOrderDefault, canvasToolDefs),
		builtinCategory("channel", "Channel inventory and channel management tools.", builtinCategoryOrderDefault, channelToolDefs),
		builtinCategory("cron", "Cron schedule management and inspection tools.", builtinCategoryOrderDefault, cronToolDefs),
		builtinCategory("crypto", "Hashing and cryptographic utility tools.", builtinCategoryOrderDefault, cryptoToolDefsFromSubpkg),
		builtinCategory("db", "Database inspection and query tools.", builtinCategoryOrderDefault, dbToolDefsFromSubpkg),
		builtinCategory("destination", "Destination inventory and account routing tools.", builtinCategoryOrderDefault, destinationToolDefs),
		builtinCategory("document", "Document creation and editing tools.", builtinCategoryOrderDefault, documentToolDefsFromSubpkg),
		builtinCategory("env", "Environment probing and runtime refresh tools.", builtinCategoryOrderDefault, envToolDefs),
		builtinCategory("exec_extra", "Supplemental shell and process discovery tools.", builtinCategoryOrderDefault, execExtraToolDefsFromSubpkg),
		builtinCategory("firecrawl", "Firecrawl web capture and extraction tools.", builtinCategoryOrderDefault, firecrawlToolDefs),
		builtinCategory("fs_extra", "Advanced filesystem diff and change tracking tools.", builtinCategoryOrderDefault, fsExtraToolDefs),
		builtinCategory("knowledge", "Knowledge source and retrieval tools.", builtinCategoryOrderDefault, knowledgeToolDefs),
		builtinCategory("media_generation", "Image, video, and music generation tools.", builtinCategoryOrderDefault, mediaGenerationToolDefsFromSubpkg),
		builtinCategory("net", "HTTP, fetch, and network utility tools.", builtinCategoryOrderDefault, netToolDefsFromSubpkg),
		builtinCategory("nodes", "Node graph inspection and execution tools.", builtinCategoryOrderDefault, nodesToolDefs),
		builtinCategory("pdf", "PDF reading and extraction tools.", builtinCategoryOrderDefault, pdfToolDefsFromSubpkg),
		builtinCategory("presentation", "Presentation creation and slide editing tools.", builtinCategoryOrderDefault, presentationToolDefsFromSubpkg),
		builtinCategory("proc", "System process inspection and control tools.", builtinCategoryOrderDefault, procToolDefsFromSubpkg),
		builtinCategory("semantic", "Semantic search and embedding-oriented tools.", builtinCategoryOrderDefault, semanticToolDefs),
		builtinCategory("spreadsheet", "Spreadsheet creation and editing tools.", builtinCategoryOrderDefault, spreadsheetToolDefsFromSubpkg),
		builtinCategory("spreadsheet_xlsx", "Excel XLSX interoperability tools.", builtinCategoryOrderDefault, xlsxToolDefsFromSubpkg),
		builtinCategory("text", "Text transformation and content processing tools.", builtinCategoryOrderDefault, textToolDefsFromSubpkg),
		builtinCategory("vision", "Image and vision analysis tools.", builtinCategoryOrderDefault, visionToolDefsFromSubpkg),
		builtinCategory("watch", "File and directory watch management tools.", builtinCategoryOrderDefault, watchToolDefs),
		builtinCategory("wakeup", "Wakeup scheduling and trigger tools.", builtinCategoryOrderDefault, wakeupToolDefs),
	}

	var errs []error
	for _, spec := range specs {
		if err := registerBuiltinCategory(spec); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ---------------------------------------------------------------------------
// Sub-package adapters: convert domain-specific ToolDef → root BuiltinToolDef.
// Each adapter wraps the sub-package handler (which takes a narrow Runtime
// interface) into the root builtinHandler (which takes *Builtins).
// ---------------------------------------------------------------------------

func archiveToolDefsFromSubpkg(_ BuiltinsConfig) []BuiltinToolDef {
	defs := archive.ToolDefs()
	out := make([]BuiltinToolDef, len(defs))
	for i, d := range defs {
		h := d.Handler
		out[i] = BuiltinToolDef{
			Manifest: d.Manifest,
			Handler: func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
				return h(ctx, b, call)
			},
		}
	}
	return out
}

func dbToolDefsFromSubpkg(_ BuiltinsConfig) []BuiltinToolDef {
	defs := db.ToolDefs()
	out := make([]BuiltinToolDef, len(defs))
	for i, d := range defs {
		h := d.Handler
		out[i] = BuiltinToolDef{
			Manifest: d.Manifest,
			Handler: func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
				return h(ctx, b, call)
			},
		}
	}
	return out
}

func calendarToolDefsFromSubpkg(_ BuiltinsConfig) []BuiltinToolDef {
	defs := calendar.ToolDefs()
	out := make([]BuiltinToolDef, len(defs))
	for i, d := range defs {
		h := d.Handler // capture for closure
		out[i] = BuiltinToolDef{
			Manifest: d.Manifest,
			Handler: func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
				return h(ctx, b, call)
			},
		}
	}
	return out
}

func cryptoToolDefsFromSubpkg(_ BuiltinsConfig) []BuiltinToolDef {
	defs := crypto.ToolDefs()
	out := make([]BuiltinToolDef, len(defs))
	for i, d := range defs {
		h := d.Handler // capture for closure
		out[i] = BuiltinToolDef{
			Manifest: d.Manifest,
			Handler: func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
				return h(ctx, b, call)
			},
		}
	}
	return out
}

func visionToolDefsFromSubpkg(_ BuiltinsConfig) []BuiltinToolDef {
	defs := vision.ToolDefs()
	out := make([]BuiltinToolDef, len(defs))
	for i, d := range defs {
		h := d.Handler // capture for closure
		out[i] = BuiltinToolDef{
			Manifest: d.Manifest,
			Handler: func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
				return h(ctx, b, call)
			},
		}
	}
	return out
}

func pdfToolDefsFromSubpkg(_ BuiltinsConfig) []BuiltinToolDef {
	defs := pdf.ToolDefs()
	out := make([]BuiltinToolDef, len(defs))
	for i, d := range defs {
		h := d.Handler // capture for closure
		out[i] = BuiltinToolDef{
			Manifest: d.Manifest,
			Handler: func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
				return h(ctx, b, call)
			},
		}
	}
	return out
}

func textToolDefsFromSubpkg(_ BuiltinsConfig) []BuiltinToolDef {
	defs := text.ToolDefs()
	out := make([]BuiltinToolDef, len(defs))
	for i, d := range defs {
		h := d.Handler // capture for closure
		out[i] = BuiltinToolDef{
			Manifest: d.Manifest,
			Handler: func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
				return h(ctx, b, call)
			},
		}
	}
	return out
}

func execToolDefsFromSubpkg(cfg BuiltinsConfig) []BuiltinToolDef {
	defs := execpkg.ToolDefs(cfg.DefaultExecTimeout)
	out := make([]BuiltinToolDef, len(defs))
	for i, d := range defs {
		h := d.Handler
		out[i] = BuiltinToolDef{
			Manifest: d.Manifest,
			Handler: func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
				return h(ctx, b, call)
			},
		}
	}
	return out
}

func execExtraToolDefsFromSubpkg(cfg BuiltinsConfig) []BuiltinToolDef {
	defs := execpkg.ExtraToolDefs(cfg.DefaultExecTimeout)
	out := make([]BuiltinToolDef, len(defs))
	for i, d := range defs {
		h := d.Handler
		out[i] = BuiltinToolDef{
			Manifest: d.Manifest,
			Handler: func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
				return h(ctx, b, call)
			},
		}
	}
	return out
}

func procToolDefsFromSubpkg(_ BuiltinsConfig) []BuiltinToolDef {
	defs := procpkg.ToolDefs()
	out := make([]BuiltinToolDef, len(defs))
	for i, d := range defs {
		h := d.Handler
		out[i] = BuiltinToolDef{
			Manifest: d.Manifest,
			Handler: func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
				return h(ctx, b, call)
			},
		}
	}
	return out
}

func netToolDefsFromSubpkg(_ BuiltinsConfig) []BuiltinToolDef {
	defs := netpkg.ToolDefs()
	out := make([]BuiltinToolDef, len(defs))
	for i, d := range defs {
		h := d.Handler
		out[i] = BuiltinToolDef{
			Manifest: d.Manifest,
			Handler: func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
				return h(ctx, &netRuntimeAdapter{b}, call)
			},
		}
	}
	return out
}

func mediaGenerationToolDefsFromSubpkg(_ BuiltinsConfig) []BuiltinToolDef {
	defs := mediagentools.ToolDefs()
	out := make([]BuiltinToolDef, len(defs))
	for i, d := range defs {
		h := d.Handler
		out[i] = BuiltinToolDef{
			Manifest: d.Manifest,
			Handler: func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
				return h(ctx, b, call)
			},
		}
	}
	return out
}

// netRuntimeAdapter wraps *Builtins to satisfy net.Runtime.
// The FetchReadableURL method converts the root-package FetchedWebContent
// to the net sub-package's FetchedWebContent to avoid import cycles.
type netRuntimeAdapter struct{ b *Builtins }

func (a *netRuntimeAdapter) JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error) {
	return a.b.JSONResult(call, payload)
}
func (a *netRuntimeAdapter) ResolvePath(input string) (string, error) { return a.b.ResolvePath(input) }
func (a *netRuntimeAdapter) DisplayPath(absPath string) string        { return a.b.DisplayPath(absPath) }
func (a *netRuntimeAdapter) RootAbs() string                          { return a.b.RootAbs() }
func (a *netRuntimeAdapter) MaxReadBytes() int                        { return a.b.MaxReadBytes() }
func (a *netRuntimeAdapter) CheckURLSSRF(rawURL string) error         { return a.b.CheckURLSSRF(rawURL) }
func (a *netRuntimeAdapter) CheckHostSSRF(host string) error          { return a.b.CheckHostSSRF(host) }
func (a *netRuntimeAdapter) HostMatchesList(host string, list []string) bool {
	return a.b.HostMatchesList(host, list)
}
func (a *netRuntimeAdapter) NewSSRFProtectedHTTPClient() *http.Client {
	return a.b.NewSSRFProtectedHTTPClient()
}
func (a *netRuntimeAdapter) AllowHosts() []string { return a.b.AllowHosts() }
func (a *netRuntimeAdapter) DenyHosts() []string  { return a.b.DenyHosts() }
func (a *netRuntimeAdapter) AllowLocal() *bool    { return a.b.AllowLocal() }
func (a *netRuntimeAdapter) MaxDownload() int64   { return a.b.MaxDownload() }

func (a *netRuntimeAdapter) FetchReadableURL(ctx context.Context, rawURL string, timeout time.Duration, maxBytes, maxChars int) (netpkg.FetchedWebContent, error) {
	result, err := a.b.FetchReadableURLRaw(ctx, rawURL, timeout, maxBytes, maxChars)
	if err != nil {
		return netpkg.FetchedWebContent{}, err
	}
	return netpkg.FetchedWebContent{
		URL:         result.URL,
		FinalURL:    result.FinalURL,
		Domain:      result.Domain,
		Title:       result.Title,
		ContentType: result.ContentType,
		Content:     result.Content,
		StatusCode:  result.StatusCode,
		Truncated:   result.Truncated,
		Bytes:       result.Bytes,
	}, nil
}

func spreadsheetToolDefsFromSubpkg(_ BuiltinsConfig) []BuiltinToolDef {
	defs := spreadsheetpkg.ToolDefs()
	out := make([]BuiltinToolDef, len(defs))
	for i, d := range defs {
		h := d.Handler // capture for closure
		out[i] = BuiltinToolDef{
			Manifest: d.Manifest,
			Handler: func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
				return h(ctx, b, call)
			},
		}
	}
	return out
}

func xlsxToolDefsFromSubpkg(_ BuiltinsConfig) []BuiltinToolDef {
	defs := spreadsheetpkg.XLSXToolDefs()
	out := make([]BuiltinToolDef, len(defs))
	for i, d := range defs {
		h := d.Handler // capture for closure
		out[i] = BuiltinToolDef{
			Manifest: d.Manifest,
			Handler: func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
				return h(ctx, b, call)
			},
		}
	}
	return out
}

func presentationToolDefsFromSubpkg(_ BuiltinsConfig) []BuiltinToolDef {
	defs := presentation.ToolDefs()
	out := make([]BuiltinToolDef, len(defs))
	for i, d := range defs {
		h := d.Handler // capture for closure
		out[i] = BuiltinToolDef{
			Manifest: d.Manifest,
			Handler: func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
				return h(ctx, b, call)
			},
		}
	}
	return out
}

func documentToolDefsFromSubpkg(_ BuiltinsConfig) []BuiltinToolDef {
	defs := document.ToolDefs()
	out := make([]BuiltinToolDef, len(defs))
	for i, d := range defs {
		h := d.Handler // capture for closure
		out[i] = BuiltinToolDef{
			Manifest: d.Manifest,
			Handler: func(ctx context.Context, b *Builtins, call agent.ToolCall) (contextengine.ToolResult, error) {
				return h(ctx, b, call)
			},
		}
	}
	return out
}

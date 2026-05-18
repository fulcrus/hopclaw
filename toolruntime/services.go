package toolruntime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

// ServicesConfig configures external service integrations for Layer 2 tools.
// Users enable tools by providing credentials/endpoints here — no code needed.
type ServicesConfig struct {
	Search   SearchServiceConfig
	Email    EmailServiceConfig
	Speech   SpeechServiceConfig
	Calendar CalendarServiceConfig
}

// SearchServiceConfig configures web search tools (search.web, search.news).
// Supported providers: serpapi, tavily, bing, google, or generic (custom endpoint).
type SearchServiceConfig struct {
	Provider string // serpapi | tavily | bing | google | generic
	APIKey   string
	BaseURL  string // custom endpoint (required for generic)
}

// EmailServiceConfig configures email.send and IMAP-backed inbox tools.
type EmailServiceConfig struct {
	SMTPHost string
	SMTPPort int // default: 587
	Username string
	Password string
	From     string
	IMAPHost string
	IMAPPort int // default: 993
}

// SpeechServiceConfig configures speech tools (speech.tts, speech.stt).
// Uses OpenAI-compatible API (works with OpenAI, Azure, local Whisper server, etc.).
type SpeechServiceConfig struct {
	BaseURL string // e.g. https://api.openai.com/v1
	APIKey  string
	Model   string // e.g. tts-1, whisper-1
}

// IsConfigured returns true if the search service has minimal config.
func (c SearchServiceConfig) IsConfigured() bool {
	return c.APIKey != "" || c.BaseURL != ""
}

// HasSMTP returns true if SMTP delivery is configured.
func (c EmailServiceConfig) HasSMTP() bool {
	return strings.TrimSpace(c.SMTPHost) != ""
}

// HasIMAP returns true if IMAP inbox access is configured.
func (c EmailServiceConfig) HasIMAP() bool {
	return strings.TrimSpace(c.IMAPHost) != ""
}

// IsConfigured returns true if either SMTP or IMAP email access is configured.
func (c EmailServiceConfig) IsConfigured() bool {
	return c.HasSMTP() || c.HasIMAP()
}

// IsConfigured returns true if the speech service has API config.
func (c SpeechServiceConfig) IsConfigured() bool {
	return c.BaseURL != "" && c.APIKey != ""
}

// CalendarServiceConfig configures CalDAV calendar access.
type CalendarServiceConfig struct {
	CalDAVURL string
	Username  string
	Password  string
}

// IsConfigured returns true if CalDAV calendar access is configured.
func (c CalendarServiceConfig) IsConfigured() bool {
	return strings.TrimSpace(c.CalDAVURL) != ""
}

// serviceHTTPClient is used for all service API calls with a sensible timeout.
var serviceHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
}

// notConfiguredResult returns a structured "not_configured" response
// that tells the user which YAML config section to fill in.
func notConfiguredResult(call agent.ToolCall, configPath string) (contextengine.ToolResult, error) {
	body, _ := json.MarshalIndent(map[string]any{
		"status":  "not_configured",
		"message": fmt.Sprintf("%s requires configuration at %s in your YAML config", call.Name, configPath),
		"hint":    fmt.Sprintf("Set %s fields to enable this tool", configPath),
	}, "", "  ")
	return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(body)}, nil
}

// ==========================================================================
// Search exec (replaces searchStubExec)
// ==========================================================================

func searchExec(ctx context.Context, _ *ws, config BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	svc := config.Services.Search
	if !svc.IsConfigured() {
		return notConfiguredResult(call, "tools.services.search")
	}

	params, err := parseSearchQueryParams(call.Input)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	payload, err := executeSearchTool(ctx, svc, call.Name, params)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("%s: %w", call.Name, err)
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(body)}, nil
}

// ==========================================================================
// Email exec (replaces emailStubExec)
// ==========================================================================

func emailExec(ctx context.Context, w *ws, config BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	svc := config.Services.Email
	switch call.Name {
	case "email.send":
		if !svc.HasSMTP() {
			return notConfiguredResult(call, "tools.services.email")
		}
		return emailSendReal(ctx, svc, call, w)
	case "email.list":
		if !svc.HasIMAP() {
			return emailIMAPNotConfigured(call), nil
		}
		return emailListReal(ctx, svc, call)
	case "email.read":
		if !svc.HasIMAP() {
			return emailIMAPNotConfigured(call), nil
		}
		return emailReadReal(ctx, svc, call)
	case "email.search":
		if !svc.HasIMAP() {
			return emailIMAPNotConfigured(call), nil
		}
		return emailSearchReal(ctx, svc, call)
	case "email.download_attachment":
		if !svc.HasIMAP() {
			return emailIMAPNotConfigured(call), nil
		}
		return emailDownloadAttachmentReal(ctx, svc, call, w)
	default:
		return notConfiguredResult(call, "tools.services.email")
	}
}

const emailMIMEBoundary = "HopClaw-MIME-Boundary-7f2e4a9b3c1d"

func emailSendReal(_ context.Context, svc EmailServiceConfig, call agent.ToolCall, w *ws) (contextengine.ToolResult, error) {
	to, err := requiredString(call.Input, "to")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	subject, err := requiredString(call.Input, "subject")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	body, err := requiredString(call.Input, "body")
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	attachmentPaths, _ := stringSliceFrom(call.Input["attachments"])
	isHTML, _ := boolFromDefault(call.Input["html"], false)

	port := svc.SMTPPort
	if port <= 0 {
		port = 587
	}
	addr := fmt.Sprintf("%s:%d", svc.SMTPHost, port)

	var auth smtp.Auth
	if svc.Username != "" {
		auth = smtp.PlainAuth("", svc.Username, svc.Password, svc.SMTPHost)
	}

	from := svc.From
	if from == "" {
		from = svc.Username
	}

	var msg []byte
	if len(attachmentPaths) > 0 {
		built, buildErr := buildMultipartEmail(from, to, subject, body, isHTML, attachmentPaths, w)
		if buildErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("email.send: %w", buildErr)
		}
		msg = built
	} else {
		contentType := "text/plain; charset=UTF-8"
		if isHTML {
			contentType = "text/html; charset=UTF-8"
		}
		msg = []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: %s\r\n\r\n%s",
			from, to, subject, contentType, body))
	}

	sendErr := smtp.SendMail(addr, auth, from, []string{to}, msg)
	if sendErr != nil {
		errResult := map[string]any{
			"success": false,
			"error":   sendErr.Error(),
		}
		out, _ := json.MarshalIndent(errResult, "", "  ")
		return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(out)}, nil
	}

	result := map[string]any{
		"success":          true,
		"to":               to,
		"subject":          subject,
		"attachment_count": len(attachmentPaths),
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(out)}, nil
}

// buildMultipartEmail constructs a MIME multipart/mixed message with attachments.
func buildMultipartEmail(from, to, subject, body string, isHTML bool, attachmentPaths []string, w *ws) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString(fmt.Sprintf("From: %s\r\n", from))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", to))
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%q\r\n", emailMIMEBoundary))
	buf.WriteString("\r\n")

	buf.WriteString(fmt.Sprintf("--%s\r\n", emailMIMEBoundary))
	if isHTML {
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	} else {
		buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	}
	buf.WriteString("\r\n")
	buf.WriteString(body)
	buf.WriteString("\r\n")

	for _, attachPath := range attachmentPaths {
		resolved := attachPath
		if w != nil {
			var resolveErr error
			resolved, resolveErr = w.resolvePath(attachPath)
			if resolveErr != nil {
				return nil, fmt.Errorf("resolve attachment %q: %w", attachPath, resolveErr)
			}
		}

		data, readErr := os.ReadFile(resolved)
		if readErr != nil {
			return nil, fmt.Errorf("read attachment %q: %w", attachPath, readErr)
		}

		filename := filepath.Base(resolved)
		contentType := mime.TypeByExtension(filepath.Ext(filename))
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		buf.WriteString(fmt.Sprintf("--%s\r\n", emailMIMEBoundary))
		buf.WriteString(fmt.Sprintf("Content-Type: %s\r\n", contentType))
		buf.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=%q\r\n", filename))
		buf.WriteString("Content-Transfer-Encoding: base64\r\n")
		buf.WriteString("\r\n")

		encoded := base64.StdEncoding.EncodeToString(data)
		const base64LineLen = 76
		for i := 0; i < len(encoded); i += base64LineLen {
			end := i + base64LineLen
			if end > len(encoded) {
				end = len(encoded)
			}
			buf.WriteString(encoded[i:end])
			buf.WriteString("\r\n")
		}
	}

	buf.WriteString(fmt.Sprintf("--%s--\r\n", emailMIMEBoundary))
	return buf.Bytes(), nil
}

// emailDownloadAttachmentReal downloads an email attachment from IMAP and saves it to workspace.
func emailDownloadAttachmentReal(ctx context.Context, svc EmailServiceConfig, call agent.ToolCall, w *ws) (contextengine.ToolResult, error) {
	uid, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	output, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	folder := strings.TrimSpace(optionalString(call.Input, "folder"))
	if folder == "" {
		folder = "INBOX"
	}
	attachIdx := 0
	if call.Input["attachment_index"] != nil {
		attachIdx, err = intFrom(call.Input["attachment_index"], 0)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("email.download_attachment: invalid attachment_index: %w", err)
		}
	}

	client, openErr := openIMAP(ctx, svc)
	if openErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("email.download_attachment: %w", openErr)
	}
	defer client.close()
	if loginErr := client.login(svc.Username, svc.Password); loginErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("email.download_attachment: %w", loginErr)
	}
	if selErr := client.selectMailbox(folder); selErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("email.download_attachment: %w", selErr)
	}

	attachments, bsErr := client.fetchBodyStructure(uid)
	if bsErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("email.download_attachment: %w", bsErr)
	}
	if attachIdx >= len(attachments) {
		return contextengine.ToolResult{}, fmt.Errorf("email.download_attachment: attachment_index %d out of range (message has %d attachments)", attachIdx, len(attachments))
	}

	att := attachments[attachIdx]

	// For multipart/mixed, attachment parts are numbered starting at 2 (1 is the body).
	partNum := fmt.Sprintf("%d", attachIdx+2)
	partData, fetchErr := client.fetchAttachmentPart(uid, partNum)
	if fetchErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("email.download_attachment: fetch part: %w", fetchErr)
	}

	// Try base64 decode; fall back to raw data.
	decoded, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(partData)))
	if decErr != nil {
		decoded = partData
	}

	resolvedOutput, err := w.resolvePath(output)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("email.download_attachment: %w", err)
	}

	if mkErr := os.MkdirAll(filepath.Dir(resolvedOutput), 0o755); mkErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("email.download_attachment: mkdir: %w", mkErr)
	}
	if writeErr := os.WriteFile(resolvedOutput, decoded, 0o644); writeErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("email.download_attachment: write: %w", writeErr)
	}

	result := map[string]any{
		"path":         w.displayPath(resolvedOutput),
		"filename":     att.Filename,
		"bytes":        len(decoded),
		"content_type": att.ContentType,
	}
	body, _ := json.MarshalIndent(result, "", "  ")
	return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(body)}, nil
}

// ==========================================================================
// Speech exec (replaces speechStubExec)
// ==========================================================================

func speechExec(ctx context.Context, w *ws, config BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	svc := config.Services.Speech
	if !svc.IsConfigured() {
		return notConfiguredResult(call, "tools.services.speech")
	}

	switch call.Name {
	case "speech.tts":
		return speechTTSReal(ctx, w, svc, call)
	case "speech.stt":
		return speechSTTReal(ctx, w, svc, call)
	default:
		return notConfiguredResult(call, "tools.services.speech")
	}
}

func speechTTSReal(ctx context.Context, w *ws, svc SpeechServiceConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	text, err := requiredString(call.Input, "text")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	output, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	mdl := svc.Model
	if mdl == "" {
		mdl = "tts-1"
	}

	reqBody, _ := json.Marshal(map[string]any{
		"input":           text,
		"model":           mdl,
		"voice":           "alloy",
		"response_format": "mp3",
	})

	u := strings.TrimRight(svc.BaseURL, "/") + "/audio/speech"
	req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(reqBody))
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("speech.tts: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+svc.APIKey)

	resp, err := serviceHTTPClient.Do(req)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("speech.tts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		result := map[string]any{
			"success":     false,
			"status_code": resp.StatusCode,
			"error":       string(errBody),
		}
		out, _ := json.MarshalIndent(result, "", "  ")
		return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(out)}, nil
	}

	resolvedOutput, err := w.resolvePath(output)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("speech.tts output: %w", err)
	}

	f, err := os.Create(resolvedOutput)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("speech.tts create: %w", err)
	}
	defer f.Close()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("speech.tts write: %w", err)
	}

	result := map[string]any{
		"success": true,
		"output":  w.displayPath(resolvedOutput),
		"bytes":   n,
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(out)}, nil
}

func speechSTTReal(ctx context.Context, w *ws, svc SpeechServiceConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	input, err := requiredString(call.Input, "input")
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	resolvedInput, err := w.resolvePath(input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("speech.stt input: %w", err)
	}

	f, err := os.Open(resolvedInput)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("speech.stt open: %w", err)
	}
	defer f.Close()

	mdl := svc.Model
	if mdl == "" {
		mdl = "whisper-1"
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", filepath.Base(resolvedInput))
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("speech.stt multipart: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("speech.stt copy: %w", err)
	}
	if err := writer.WriteField("model", mdl); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("speech.stt field: %w", err)
	}
	writer.Close()

	u := strings.TrimRight(svc.BaseURL, "/") + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, "POST", u, &buf)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("speech.stt: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+svc.APIKey)

	resp, err := serviceHTTPClient.Do(req)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("speech.stt: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("speech.stt read: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		result := map[string]any{
			"success":     false,
			"status_code": resp.StatusCode,
			"error":       string(respBody),
		}
		out, _ := json.MarshalIndent(result, "", "  ")
		return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(out)}, nil
	}

	var data any
	if jsonErr := json.Unmarshal(respBody, &data); jsonErr != nil {
		data = map[string]any{"text": string(respBody)}
	}

	result := map[string]any{
		"success": true,
		"input":   w.displayPath(resolvedInput),
		"result":  data,
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(out)}, nil
}

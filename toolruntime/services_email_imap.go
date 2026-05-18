package toolruntime

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/imaputil"
	"github.com/fulcrus/hopclaw/internal/support/ints"
)

const (
	defaultIMAPPort      = 993
	emailIMAPTimeout     = 20 * time.Second
	emailBodyPreviewMax  = 64 << 10
	emailHeaderFetchSpec = "BODY.PEEK[HEADER.FIELDS (SUBJECT FROM TO DATE)]"
)

type emailMessage struct {
	ID          string            `json:"id"`
	Subject     string            `json:"subject,omitempty"`
	From        string            `json:"from,omitempty"`
	To          string            `json:"to,omitempty"`
	Date        string            `json:"date,omitempty"`
	Size        int               `json:"size,omitempty"`
	Preview     string            `json:"preview,omitempty"`
	Body        string            `json:"body,omitempty"`
	Attachments []emailAttachment `json:"attachments,omitempty"`
}

type emailAttachment struct {
	Index       int    `json:"index"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
}

type imapClient struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
	tag  int
}

func emailIMAPNotConfigured(call agent.ToolCall) contextengine.ToolResult {
	body, _ := json.MarshalIndent(map[string]any{
		"status":  "not_configured",
		"message": fmt.Sprintf("%s requires IMAP configuration (imap_host, username, password) in tools.services.email", call.Name),
	}, "", "  ")
	return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(body)}
}

func emailListReal(ctx context.Context, svc EmailServiceConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	folder, limit, err := parseEmailBrowseInputs(call.Input)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	client, err := openIMAP(ctx, svc)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	defer client.close()
	if err := client.login(svc.Username, svc.Password); err != nil {
		return contextengine.ToolResult{}, err
	}
	if err := client.selectMailbox(folder); err != nil {
		return contextengine.ToolResult{}, err
	}
	ids, err := client.searchUIDs("ALL")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	items, err := client.fetchRecentMetadata(ids, limit)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return emailJSONResult(call, map[string]any{
		"folder": folder,
		"count":  len(items),
		"items":  items,
	})
}

func emailReadReal(ctx context.Context, svc EmailServiceConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	id, err := requiredString(call.Input, "id")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	folder := strings.TrimSpace(optionalString(call.Input, "folder"))
	if folder == "" {
		folder = "INBOX"
	}
	client, err := openIMAP(ctx, svc)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	defer client.close()
	if err := client.login(svc.Username, svc.Password); err != nil {
		return contextengine.ToolResult{}, err
	}
	if err := client.selectMailbox(folder); err != nil {
		return contextengine.ToolResult{}, err
	}
	meta, err := client.fetchMetadata(id)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	body, err := client.fetchBody(id, emailBodyPreviewMax)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	meta.Body = body
	meta.Preview = compactEmailText(body, 280)
	return emailJSONResult(call, map[string]any{
		"folder": folder,
		"item":   meta,
	})
}

func emailSearchReal(ctx context.Context, svc EmailServiceConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	query, err := requiredString(call.Input, "query")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	folder, limit, err := parseEmailBrowseInputs(call.Input)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	client, err := openIMAP(ctx, svc)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	defer client.close()
	if err := client.login(svc.Username, svc.Password); err != nil {
		return contextengine.ToolResult{}, err
	}
	if err := client.selectMailbox(folder); err != nil {
		return contextengine.ToolResult{}, err
	}
	ids, err := client.searchUIDs(`TEXT ` + imaputil.Quote(query))
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	items, err := client.fetchRecentMetadata(ids, limit)
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return emailJSONResult(call, map[string]any{
		"folder": folder,
		"query":  query,
		"count":  len(items),
		"items":  items,
	})
}

func emailJSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error) {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(body)}, nil
}

func parseEmailBrowseInputs(input map[string]any) (folder string, limit int, err error) {
	folder = strings.TrimSpace(optionalString(input, "folder"))
	if folder == "" {
		folder = "INBOX"
	}
	limit = 20
	if input["limit"] != nil {
		limit, err = intFrom(input["limit"], 20)
		if err != nil {
			return "", 0, fmt.Errorf("invalid limit: %w", err)
		}
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return folder, limit, nil
}

func openIMAP(ctx context.Context, svc EmailServiceConfig) (*imapClient, error) {
	host := strings.TrimSpace(svc.IMAPHost)
	if host == "" {
		return nil, fmt.Errorf("email IMAP host is not configured")
	}
	port := svc.IMAPPort
	if port <= 0 {
		port = defaultIMAPPort
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: emailIMAPTimeout}
	insecure := false
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		insecure = true
	}
	if strings.EqualFold(host, "localhost") {
		insecure = true
	}
	conn, err := tls.DialWithDialer(dialer, "tcp", address, &tls.Config{
		ServerName:         host,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecure,
	})
	if err != nil {
		return nil, fmt.Errorf("imap dial: %w", err)
	}
	client := &imapClient{
		conn: conn,
		r:    bufio.NewReader(conn),
		w:    bufio.NewWriter(conn),
	}
	conn.SetDeadline(time.Now().Add(emailIMAPTimeout))
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if _, err := client.readLine(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("imap greeting: %w", err)
	}
	return client, nil
}

func (c *imapClient) close() {
	if c == nil || c.conn == nil {
		return
	}
	_, _ = c.run("LOGOUT")
	_ = c.conn.Close()
}

func (c *imapClient) login(username string, password string) error {
	if strings.TrimSpace(username) == "" || strings.TrimSpace(password) == "" {
		return fmt.Errorf("email IMAP requires username and password")
	}
	_, err := c.run("LOGIN " + imaputil.Quote(username) + " " + imaputil.Quote(password))
	return err
}

func (c *imapClient) selectMailbox(folder string) error {
	if folder == "" {
		folder = "INBOX"
	}
	_, err := c.run("SELECT " + imaputil.Quote(folder))
	return err
}

func (c *imapClient) searchUIDs(criteria string) ([]string, error) {
	lines, err := c.run("UID SEARCH " + criteria)
	if err != nil {
		return nil, err
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "* SEARCH") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) <= 2 {
			return nil, nil
		}
		return fields[2:], nil
	}
	return nil, nil
}

func (c *imapClient) fetchRecentMetadata(ids []string, limit int) ([]emailMessage, error) {
	if len(ids) == 0 {
		return []emailMessage{}, nil
	}
	items := make([]emailMessage, 0, ints.Min(limit, len(ids)))
	for i := len(ids) - 1; i >= 0 && len(items) < limit; i-- {
		msg, err := c.fetchMetadata(ids[i])
		if err != nil {
			return nil, err
		}
		items = append(items, msg)
	}
	return items, nil
}

func (c *imapClient) fetchMetadata(uid string) (emailMessage, error) {
	lines, literals, err := c.runFetch("UID FETCH " + uid + " (UID RFC822.SIZE INTERNALDATE " + emailHeaderFetchSpec + ")")
	if err != nil {
		return emailMessage{}, err
	}
	msg := emailMessage{ID: uid}
	if len(literals) > 0 {
		applyEmailHeaders(&msg, literals[0])
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "* ") && strings.Contains(line, "RFC822.SIZE ") {
			if size, ok := extractIMAPInt(line, "RFC822.SIZE "); ok {
				msg.Size = size
			}
			if date, ok := extractIMAPQuoted(line, "INTERNALDATE "); ok {
				msg.Date = date
			}
			break
		}
	}
	return msg, nil
}

func (c *imapClient) fetchBody(uid string, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		maxBytes = emailBodyPreviewMax
	}
	_, literals, err := c.runFetch(fmt.Sprintf("UID FETCH %s (BODY.PEEK[TEXT]<0.%d>)", uid, maxBytes))
	if err != nil {
		return "", err
	}
	if len(literals) == 0 {
		return "", nil
	}
	return compactEmailText(string(literals[0]), maxBytes), nil
}

// fetchBodyStructure fetches the BODYSTRUCTURE of a message and extracts
// attachment information. It returns a list of attachments with their part
// numbers, filenames, content types, and sizes.
func (c *imapClient) fetchBodyStructure(uid string) ([]emailAttachment, error) {
	lines, _, err := c.runFetch(fmt.Sprintf("UID FETCH %s (BODYSTRUCTURE)", uid))
	if err != nil {
		return nil, err
	}
	// Join all lines to get the full BODYSTRUCTURE response.
	full := strings.Join(lines, " ")
	return parseBodyStructureAttachments(full), nil
}

// fetchAttachmentPart fetches a specific MIME body part by part number (e.g. "2")
// and returns the raw (base64-encoded) content.
func (c *imapClient) fetchAttachmentPart(uid string, partNum string) ([]byte, error) {
	_, literals, err := c.runFetch(fmt.Sprintf("UID FETCH %s (BODY.PEEK[%s])", uid, partNum))
	if err != nil {
		return nil, err
	}
	if len(literals) == 0 {
		return nil, fmt.Errorf("no data returned for part %s", partNum)
	}
	return literals[0], nil
}

// parseBodyStructureAttachments extracts attachment info from BODYSTRUCTURE.
// This is a simplified parser that looks for attachment disposition entries.
func parseBodyStructureAttachments(bs string) []emailAttachment {
	var attachments []emailAttachment
	upper := strings.ToUpper(bs)

	// Look for "ATTACHMENT" disposition indicators in the BODYSTRUCTURE.
	idx := 0
	partIndex := 0
	for {
		pos := strings.Index(upper[idx:], "ATTACHMENT")
		if pos < 0 {
			break
		}
		absPos := idx + pos
		idx = absPos + len("ATTACHMENT")

		// Try to extract filename from nearby "FILENAME" parameter.
		filename := extractBSFilename(bs, absPos)
		if filename == "" {
			filename = fmt.Sprintf("attachment_%d", partIndex)
		}

		// Try to extract content type by looking backwards for type/subtype.
		contentType := extractBSContentType(bs, absPos)

		// Try to extract size.
		size := extractBSSize(bs, absPos)

		attachments = append(attachments, emailAttachment{
			Index:       partIndex,
			Filename:    filename,
			ContentType: contentType,
			Size:        size,
		})
		partIndex++
	}
	return attachments
}

// extractBSFilename looks for a "FILENAME" parameter near the given position.
func extractBSFilename(bs string, nearPos int) string {
	// Search within a window around the position.
	windowStart := nearPos - 200
	if windowStart < 0 {
		windowStart = 0
	}
	windowEnd := nearPos + 200
	if windowEnd > len(bs) {
		windowEnd = len(bs)
	}
	window := bs[windowStart:windowEnd]
	upper := strings.ToUpper(window)

	for _, key := range []string{"\"FILENAME\" ", "\"NAME\" "} {
		fnIdx := strings.Index(upper, key)
		if fnIdx < 0 {
			continue
		}
		rest := window[fnIdx+len(key):]
		rest = strings.TrimSpace(rest)
		if strings.HasPrefix(rest, "\"") {
			end := strings.Index(rest[1:], "\"")
			if end >= 0 {
				return rest[1 : end+1]
			}
		}
	}
	return ""
}

// extractBSContentType looks backwards from the position to find type/subtype.
func extractBSContentType(bs string, nearPos int) string {
	windowStart := nearPos - 100
	if windowStart < 0 {
		windowStart = 0
	}
	window := bs[windowStart:nearPos]
	upper := strings.ToUpper(window)

	// Common MIME types to look for.
	for _, mtype := range []string{"APPLICATION", "IMAGE", "AUDIO", "VIDEO", "TEXT"} {
		idx := strings.LastIndex(upper, "\""+mtype+"\"")
		if idx < 0 {
			continue
		}
		// Find the subtype after it.
		rest := window[idx:]
		parts := strings.SplitN(rest, "\"", 5)
		if len(parts) >= 4 {
			return strings.ToLower(parts[1]) + "/" + strings.ToLower(parts[3])
		}
	}
	return "application/octet-stream"
}

// extractBSSize tries to find a size number near the given position.
func extractBSSize(bs string, nearPos int) int {
	windowEnd := nearPos + 200
	if windowEnd > len(bs) {
		windowEnd = len(bs)
	}
	window := bs[nearPos:windowEnd]

	// Look for a standalone number that looks like a size.
	fields := strings.Fields(window)
	for _, f := range fields {
		f = strings.Trim(f, "()\",")
		if n, err := strconv.Atoi(f); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func (c *imapClient) run(command string) ([]string, error) {
	tag := c.nextTag()
	if _, err := c.w.WriteString(tag + " " + command + "\r\n"); err != nil {
		return nil, err
	}
	if err := c.w.Flush(); err != nil {
		return nil, err
	}
	lines := make([]string, 0, 8)
	for {
		line, err := c.readLine()
		if err != nil {
			return nil, err
		}
		lines = append(lines, line)
		if strings.HasPrefix(line, tag+" ") {
			if !strings.Contains(line, " OK") && !strings.HasSuffix(line, " OK") {
				return nil, fmt.Errorf("imap command failed: %s", line)
			}
			return lines, nil
		}
	}
}

func (c *imapClient) runFetch(command string) ([]string, [][]byte, error) {
	tag := c.nextTag()
	if _, err := c.w.WriteString(tag + " " + command + "\r\n"); err != nil {
		return nil, nil, err
	}
	if err := c.w.Flush(); err != nil {
		return nil, nil, err
	}
	lines := make([]string, 0, 8)
	literals := make([][]byte, 0, 2)
	for {
		line, err := c.readLine()
		if err != nil {
			return nil, nil, err
		}
		lines = append(lines, line)
		if literalSize, ok := imaputil.ParseLiteralSize(line); ok {
			buf := make([]byte, literalSize)
			if _, err := io.ReadFull(c.r, buf); err != nil {
				return nil, nil, err
			}
			literals = append(literals, buf)
			if _, err := c.readLine(); err != nil {
				return nil, nil, err
			}
			continue
		}
		if strings.HasPrefix(line, tag+" ") {
			if !strings.Contains(line, " OK") && !strings.HasSuffix(line, " OK") {
				return nil, nil, fmt.Errorf("imap command failed: %s", line)
			}
			return lines, literals, nil
		}
	}
}

func (c *imapClient) nextTag() string {
	c.tag++
	return fmt.Sprintf("A%04d", c.tag)
}

func (c *imapClient) readLine() (string, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func parseIMAPLiteralSize(line string) (int, bool) {
	if !strings.HasSuffix(line, "}") {
		return 0, false
	}
	start := strings.LastIndex(line, "{")
	if start < 0 || start >= len(line)-2 {
		return 0, false
	}
	n, err := strconv.Atoi(line[start+1 : len(line)-1])
	if err != nil {
		return 0, false
	}
	return n, true
}

func extractIMAPInt(line string, prefix string) (int, bool) {
	idx := strings.Index(line, prefix)
	if idx < 0 {
		return 0, false
	}
	rest := line[idx+len(prefix):]
	end := strings.IndexByte(rest, ' ')
	if end < 0 {
		end = len(rest)
	}
	n, err := strconv.Atoi(strings.TrimSpace(rest[:end]))
	if err != nil {
		return 0, false
	}
	return n, true
}

func extractIMAPQuoted(line string, prefix string) (string, bool) {
	idx := strings.Index(line, prefix)
	if idx < 0 {
		return "", false
	}
	rest := strings.TrimSpace(line[idx+len(prefix):])
	if !strings.HasPrefix(rest, `"`) {
		return "", false
	}
	rest = rest[1:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

func applyEmailHeaders(msg *emailMessage, headerBytes []byte) {
	parsed, err := mail.ReadMessage(strings.NewReader(string(headerBytes)))
	if err != nil {
		return
	}
	msg.Subject = decodeHeaderValue(parsed.Header.Get("Subject"))
	msg.From = decodeHeaderValue(parsed.Header.Get("From"))
	msg.To = decodeHeaderValue(parsed.Header.Get("To"))
	if msg.Date == "" {
		msg.Date = decodeHeaderValue(parsed.Header.Get("Date"))
	}
}

func decodeHeaderValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	decoded, err := (&mime.WordDecoder{}).DecodeHeader(value)
	if err == nil {
		return strings.TrimSpace(decoded)
	}
	return value
}

func compactEmailText(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\x00", "")
	text = strings.Join(strings.Fields(text), " ")
	if maxLen > 0 && len(text) > maxLen {
		return strings.TrimSpace(text[:maxLen]) + "..."
	}
	return text
}

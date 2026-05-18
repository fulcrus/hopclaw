package watch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	browserclient "github.com/fulcrus/hopclaw/browserapi/client"
	browsertypes "github.com/fulcrus/hopclaw/browserapi/types"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/imaputil"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
	ical "github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	"github.com/emersion/go-webdav/caldav"
)

type driver interface {
	Validate(Source) error
	Probe(context.Context, Source) (Observation, error)
	Describe(Source) string
}

func validateDriver(kind string) (driver, error) {
	switch strings.TrimSpace(kind) {
	case SourceKindHTTP:
		return httpDriver{}, nil
	case SourceKindFile:
		return fileDriver{}, nil
	case SourceKindFeed:
		return feedDriver{}, nil
	case SourceKindMailbox:
		return mailboxValidateDriver{}, nil
	case SourceKindBrowserSnapshot:
		return browserSnapshotValidateDriver{}, nil
	case SourceKindCalendar:
		return calendarValidateDriver{}, nil
	case SourceKindWebhook:
		return webhookInboxValidateDriver{}, nil
	case SourceKindStructuredInbox:
		return structuredInboxValidateDriver{}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported source kind %q", ErrInvalidSource, kind)
	}
}

func describeDriver(kind string) (driver, error) {
	return validateDriver(kind)
}

func (s *Service) getDriver(kind string) (driver, error) {
	switch strings.TrimSpace(kind) {
	case SourceKindHTTP:
		return httpDriver{}, nil
	case SourceKindFile:
		return fileDriver{}, nil
	case SourceKindFeed:
		return feedDriver{}, nil
	case SourceKindMailbox:
		return mailboxDriver{config: s.email}, nil
	case SourceKindBrowserSnapshot:
		return browserSnapshotDriver{client: s.browserClient}, nil
	case SourceKindCalendar:
		return calendarDriver{config: s.calendar}, nil
	case SourceKindWebhook:
		return webhookInboxDriver{reader: s.sessionReader}, nil
	case SourceKindStructuredInbox:
		return structuredInboxDriver{reader: s.sessionReader}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported source kind %q", ErrInvalidSource, kind)
	}
}

type httpDriver struct{}

func (httpDriver) Validate(source Source) error {
	if source.HTTP == nil || strings.TrimSpace(source.HTTP.URL) == "" {
		return fmt.Errorf("%w: http.url is required", ErrInvalidSource)
	}
	if _, err := url.ParseRequestURI(strings.TrimSpace(source.HTTP.URL)); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSource, err)
	}
	return nil
}

func (httpDriver) Probe(ctx context.Context, source Source) (Observation, error) {
	return fetchObservation(ctx, strings.TrimSpace(source.HTTP.URL))
}

func (httpDriver) Describe(source Source) string {
	if source.HTTP == nil {
		return ""
	}
	return strings.TrimSpace(source.HTTP.URL)
}

type fileDriver struct{}

func (fileDriver) Validate(source Source) error {
	if source.File == nil || strings.TrimSpace(source.File.Path) == "" {
		return fmt.Errorf("%w: file.path is required", ErrInvalidSource)
	}
	return nil
}

func (fileDriver) Probe(_ context.Context, source Source) (Observation, error) {
	if source.File == nil {
		return Observation{}, fmt.Errorf("%w: file source is required", ErrInvalidSource)
	}
	data, err := os.ReadFile(strings.TrimSpace(source.File.Path))
	if err != nil {
		return Observation{}, err
	}
	if len(data) > httpProbeBodySize {
		data = data[:httpProbeBodySize]
	}
	return observationFromBytes(data), nil
}

func (fileDriver) Describe(source Source) string {
	if source.File == nil {
		return ""
	}
	return strings.TrimSpace(source.File.Path)
}

type feedDriver struct{}

func (feedDriver) Validate(source Source) error {
	if source.Feed == nil || strings.TrimSpace(source.Feed.URL) == "" {
		return fmt.Errorf("%w: feed.url is required", ErrInvalidSource)
	}
	if _, err := url.ParseRequestURI(strings.TrimSpace(source.Feed.URL)); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSource, err)
	}
	return nil
}

func (feedDriver) Probe(ctx context.Context, source Source) (Observation, error) {
	observation, err := fetchObservation(ctx, strings.TrimSpace(source.Feed.URL))
	if err != nil {
		return Observation{}, err
	}
	items := parseFeedItems(observation.Body)
	if len(items) == 0 {
		return observation, nil
	}
	fingerprint := hashStrings(items...)
	return Observation{
		Summary:     compactSummary(strings.Join(items, "\n")),
		Body:        strings.Join(items, "\n"),
		Fingerprint: fingerprint,
	}, nil
}

func (feedDriver) Describe(source Source) string {
	if source.Feed == nil {
		return ""
	}
	return strings.TrimSpace(source.Feed.URL)
}

type mailboxValidateDriver struct{}

func (mailboxValidateDriver) Validate(source Source) error {
	if source.Mailbox == nil {
		return fmt.Errorf("%w: mailbox source is required", ErrInvalidSource)
	}
	return nil
}

func (mailboxValidateDriver) Probe(context.Context, Source) (Observation, error) {
	return Observation{}, fmt.Errorf("mailbox probe requires service configuration")
}

func (mailboxValidateDriver) Describe(source Source) string {
	if source.Mailbox == nil {
		return ""
	}
	folder := strings.TrimSpace(source.Mailbox.Folder)
	if folder == "" {
		folder = "INBOX"
	}
	if query := strings.TrimSpace(source.Mailbox.Query); query != "" {
		return folder + " query=" + query
	}
	return folder
}

type mailboxDriver struct {
	config EmailConfig
}

func (d mailboxDriver) Validate(source Source) error {
	return mailboxValidateDriver{}.Validate(source)
}

func (d mailboxDriver) Probe(ctx context.Context, source Source) (Observation, error) {
	if strings.TrimSpace(d.config.IMAPHost) == "" || strings.TrimSpace(d.config.Username) == "" || strings.TrimSpace(d.config.Password) == "" {
		return Observation{}, fmt.Errorf("%w: mailbox driver requires IMAP configuration", ErrInvalidSource)
	}
	client, err := openMailboxIMAP(ctx, d.config)
	if err != nil {
		return Observation{}, err
	}
	defer client.close()
	if err := client.login(d.config.Username, d.config.Password); err != nil {
		return Observation{}, err
	}
	folder := "INBOX"
	if source.Mailbox != nil && strings.TrimSpace(source.Mailbox.Folder) != "" {
		folder = strings.TrimSpace(source.Mailbox.Folder)
	}
	if err := client.selectMailbox(folder); err != nil {
		return Observation{}, err
	}
	query := "ALL"
	if source.Mailbox != nil && strings.TrimSpace(source.Mailbox.Query) != "" {
		query = `TEXT ` + imaputil.Quote(strings.TrimSpace(source.Mailbox.Query))
	}
	ids, err := client.searchUIDs(query)
	if err != nil {
		return Observation{}, err
	}
	limit := 20
	if source.Mailbox != nil && source.Mailbox.Limit > 0 {
		limit = source.Mailbox.Limit
	}
	items, err := client.fetchRecentMetadata(ids, limit)
	if err != nil {
		return Observation{}, err
	}
	lines := make([]string, 0, len(items))
	for _, item := range items {
		line := strings.TrimSpace(item.Date + " | " + item.From + " | " + item.Subject)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return Observation{
		Summary:     compactSummary(strings.Join(lines, "\n")),
		Body:        strings.Join(lines, "\n"),
		Fingerprint: hashStrings(lines...),
	}, nil
}

func (d mailboxDriver) Describe(source Source) string {
	return mailboxValidateDriver{}.Describe(source)
}

type browserSnapshotValidateDriver struct{}

func (browserSnapshotValidateDriver) Validate(source Source) error {
	if source.BrowserSnapshot == nil || strings.TrimSpace(source.BrowserSnapshot.URL) == "" {
		return fmt.Errorf("%w: browser_snapshot.url is required", ErrInvalidSource)
	}
	if _, err := url.ParseRequestURI(strings.TrimSpace(source.BrowserSnapshot.URL)); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSource, err)
	}
	return nil
}

func (browserSnapshotValidateDriver) Probe(context.Context, Source) (Observation, error) {
	return Observation{}, fmt.Errorf("browser snapshot probe requires browser client")
}

func (browserSnapshotValidateDriver) Describe(source Source) string {
	if source.BrowserSnapshot == nil {
		return ""
	}
	return strings.TrimSpace(source.BrowserSnapshot.URL)
}

type calendarValidateDriver struct{}

func (calendarValidateDriver) Validate(source Source) error {
	if source.Calendar == nil {
		return fmt.Errorf("%w: calendar source is required", ErrInvalidSource)
	}
	if source.Calendar.Limit < 0 {
		return fmt.Errorf("%w: calendar.limit must be >= 0", ErrInvalidSource)
	}
	return nil
}

func (calendarValidateDriver) Probe(context.Context, Source) (Observation, error) {
	return Observation{}, fmt.Errorf("calendar probe requires CalDAV configuration")
}

func (calendarValidateDriver) Describe(source Source) string {
	if source.Calendar == nil {
		return "calendar"
	}
	if query := strings.TrimSpace(source.Calendar.Query); query != "" {
		return "calendar query=" + query
	}
	return "calendar"
}

type calendarDriver struct {
	config CalendarConfig
}

func (d calendarDriver) Validate(source Source) error {
	return calendarValidateDriver{}.Validate(source)
}

func (d calendarDriver) Probe(ctx context.Context, source Source) (Observation, error) {
	if strings.TrimSpace(d.config.CalDAVURL) == "" {
		return Observation{}, fmt.Errorf("%w: calendar driver requires CalDAV configuration", ErrInvalidSource)
	}
	client, err := newWatchCalDAVClient(d.config)
	if err != nil {
		return Observation{}, err
	}
	calPath, err := findWatchCalendarPath(ctx, client)
	if err != nil {
		return Observation{}, err
	}
	start := time.Now().Add(-24 * time.Hour)
	end := time.Now().Add(7 * 24 * time.Hour)
	query := &caldav.CalendarQuery{
		CompRequest: caldav.CalendarCompRequest{
			Name:  ical.CompCalendar,
			Props: []string{ical.PropVersion},
			Comps: []caldav.CalendarCompRequest{{
				Name:     ical.CompEvent,
				AllProps: true,
			}},
		},
		CompFilter: caldav.CompFilter{
			Name: ical.CompCalendar,
			Comps: []caldav.CompFilter{{
				Name:  ical.CompEvent,
				Start: start,
				End:   end,
			}},
		},
	}
	objects, err := client.QueryCalendar(ctx, calPath, query)
	if err != nil {
		return Observation{}, err
	}
	filter := ""
	limit := 50
	if source.Calendar != nil {
		filter = strings.ToLower(strings.TrimSpace(source.Calendar.Query))
		if source.Calendar.Limit > 0 {
			limit = source.Calendar.Limit
		}
	}
	lines := make([]string, 0, limit)
	for _, obj := range objects {
		if obj.Data == nil {
			continue
		}
		for _, ev := range obj.Data.Events() {
			line := calendarEventLine(&ev)
			if line == "" {
				continue
			}
			if filter != "" && !strings.Contains(strings.ToLower(line), filter) {
				continue
			}
			lines = append(lines, line)
			if len(lines) >= limit {
				break
			}
		}
		if len(lines) >= limit {
			break
		}
	}
	return Observation{
		Summary:     compactSummary(strings.Join(lines, "\n")),
		Body:        strings.Join(lines, "\n"),
		Fingerprint: hashStrings(lines...),
	}, nil
}

func (d calendarDriver) Describe(source Source) string {
	return calendarValidateDriver{}.Describe(source)
}

type webhookInboxValidateDriver struct{}

func (webhookInboxValidateDriver) Validate(source Source) error {
	if source.Webhook == nil {
		return fmt.Errorf("%w: webhook source is required", ErrInvalidSource)
	}
	if source.Webhook.Limit < 0 {
		return fmt.Errorf("%w: webhook.limit must be >= 0", ErrInvalidSource)
	}
	if strings.TrimSpace(source.Webhook.SessionKey) != "" {
		return nil
	}
	if strings.TrimSpace(source.Webhook.WebhookID) == "" || strings.TrimSpace(source.Webhook.SenderID) == "" {
		return fmt.Errorf("%w: webhook requires session_key or webhook_id + sender_id", ErrInvalidSource)
	}
	return nil
}

func (webhookInboxValidateDriver) Probe(context.Context, Source) (Observation, error) {
	return Observation{}, fmt.Errorf("webhook inbox probe requires session access")
}

func (webhookInboxValidateDriver) Describe(source Source) string {
	if source.Webhook == nil {
		return "webhook inbox"
	}
	if sessionKey := strings.TrimSpace(source.Webhook.SessionKey); sessionKey != "" {
		return sessionKey
	}
	if webhookID := strings.TrimSpace(source.Webhook.WebhookID); webhookID != "" {
		if senderID := strings.TrimSpace(source.Webhook.SenderID); senderID != "" {
			return "webhook:" + webhookID + ":" + senderID
		}
		return "webhook:" + webhookID
	}
	return "webhook inbox"
}

type webhookInboxDriver struct {
	reader SessionInboxReader
}

func (d webhookInboxDriver) Validate(source Source) error {
	return webhookInboxValidateDriver{}.Validate(source)
}

func (d webhookInboxDriver) Probe(ctx context.Context, source Source) (Observation, error) {
	if source.Webhook == nil {
		return Observation{}, fmt.Errorf("%w: webhook source is required", ErrInvalidSource)
	}
	sessionKey := strings.TrimSpace(source.Webhook.SessionKey)
	if sessionKey == "" {
		sessionKey = fmt.Sprintf("webhook:%s:%s", strings.TrimSpace(source.Webhook.WebhookID), strings.TrimSpace(source.Webhook.SenderID))
	}
	limit := source.Webhook.Limit
	if limit <= 0 {
		limit = 20
	}
	return probeSessionInbox(ctx, d.reader, sessionKey, limit)
}

func (d webhookInboxDriver) Describe(source Source) string {
	return webhookInboxValidateDriver{}.Describe(source)
}

type structuredInboxValidateDriver struct{}

func (structuredInboxValidateDriver) Validate(source Source) error {
	if source.StructuredInbox == nil {
		return fmt.Errorf("%w: structured_app_inbox source is required", ErrInvalidSource)
	}
	if strings.TrimSpace(source.StructuredInbox.SessionKey) == "" {
		return fmt.Errorf("%w: structured_app_inbox.session_key is required", ErrInvalidSource)
	}
	if source.StructuredInbox.Limit < 0 {
		return fmt.Errorf("%w: structured_app_inbox.limit must be >= 0", ErrInvalidSource)
	}
	return nil
}

func (structuredInboxValidateDriver) Probe(context.Context, Source) (Observation, error) {
	return Observation{}, fmt.Errorf("structured app inbox probe requires session access")
}

func (structuredInboxValidateDriver) Describe(source Source) string {
	if source.StructuredInbox == nil {
		return "structured app inbox"
	}
	return strings.TrimSpace(source.StructuredInbox.SessionKey)
}

type structuredInboxDriver struct {
	reader SessionInboxReader
}

func (d structuredInboxDriver) Validate(source Source) error {
	return structuredInboxValidateDriver{}.Validate(source)
}

func (d structuredInboxDriver) Probe(ctx context.Context, source Source) (Observation, error) {
	if source.StructuredInbox == nil {
		return Observation{}, fmt.Errorf("%w: structured_app_inbox source is required", ErrInvalidSource)
	}
	limit := source.StructuredInbox.Limit
	if limit <= 0 {
		limit = 20
	}
	return probeSessionInbox(ctx, d.reader, strings.TrimSpace(source.StructuredInbox.SessionKey), limit)
}

func (d structuredInboxDriver) Describe(source Source) string {
	return structuredInboxValidateDriver{}.Describe(source)
}

func probeSessionInbox(ctx context.Context, reader SessionInboxReader, sessionKey string, limit int) (Observation, error) {
	if reader == nil {
		return Observation{}, fmt.Errorf("%w: session-backed inbox driver requires session access", ErrInvalidSource)
	}
	if strings.TrimSpace(sessionKey) == "" {
		return Observation{}, fmt.Errorf("%w: session key is required", ErrInvalidSource)
	}
	session, err := reader.GetByKey(ctx, sessionKey)
	if err != nil {
		if looksLikeSessionNotFound(err) {
			return Observation{
				Summary:     "",
				Body:        "",
				Fingerprint: hashStrings(),
			}, nil
		}
		return Observation{}, err
	}
	if session == nil {
		return Observation{
			Summary:     "",
			Body:        "",
			Fingerprint: hashStrings(),
		}, nil
	}
	lines := sessionInboxLines(session, limit)
	body := strings.Join(lines, "\n")
	return Observation{
		Summary:     compactSummary(body),
		Body:        body,
		Fingerprint: hashStrings(lines...),
	}, nil
}

func sessionInboxLines(session *agent.Session, limit int) []string {
	if session == nil {
		return nil
	}
	if limit <= 0 {
		limit = 20
	}
	lines := make([]string, 0, limit)
	for _, msg := range session.Messages {
		if msg.Role != contextengine.RoleUser {
			continue
		}
		text := strings.TrimSpace(msg.TextContent())
		if text == "" {
			continue
		}
		prefix := msg.CreatedAt.UTC().Format(time.RFC3339)
		lines = append(lines, prefix+" | "+text)
	}
	if len(lines) == 0 {
		return nil
	}
	if len(lines) > limit {
		lines = append([]string(nil), lines[len(lines)-limit:]...)
	}
	sort.Strings(lines)
	return lines
}

func looksLikeSessionNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

func newWatchCalDAVClient(cfg CalendarConfig) (*caldav.Client, error) {
	httpClient := webdav.HTTPClientWithBasicAuth(nil, cfg.Username, cfg.Password)
	return caldav.NewClient(httpClient, cfg.CalDAVURL)
}

func findWatchCalendarPath(ctx context.Context, client *caldav.Client) (string, error) {
	principal, err := client.FindCurrentUserPrincipal(ctx)
	if err != nil {
		return "", fmt.Errorf("find principal: %w", err)
	}
	homeSet, err := client.FindCalendarHomeSet(ctx, principal)
	if err != nil {
		return "", fmt.Errorf("find calendar home set: %w", err)
	}
	calendars, err := client.FindCalendars(ctx, homeSet)
	if err != nil {
		return "", fmt.Errorf("find calendars: %w", err)
	}
	if len(calendars) == 0 {
		return "", fmt.Errorf("no calendars found")
	}
	return calendars[0].Path, nil
}

func calendarEventLine(ev *ical.Event) string {
	if ev == nil {
		return ""
	}
	parts := make([]string, 0, 4)
	if start, err := ev.DateTimeStart(nil); err == nil {
		parts = append(parts, start.Format(time.RFC3339))
	}
	if summary, err := ev.Props.Text(ical.PropSummary); err == nil && strings.TrimSpace(summary) != "" {
		parts = append(parts, strings.TrimSpace(summary))
	}
	if loc, err := ev.Props.Text(ical.PropLocation); err == nil && strings.TrimSpace(loc) != "" {
		parts = append(parts, strings.TrimSpace(loc))
	}
	if org, err := ev.Props.Text(ical.PropOrganizer); err == nil && strings.TrimSpace(org) != "" {
		parts = append(parts, strings.TrimSpace(org))
	}
	return strings.TrimSpace(strings.Join(parts, " | "))
}

type browserSnapshotDriver struct {
	client *browserclient.Client
}

func (d browserSnapshotDriver) Validate(source Source) error {
	return browserSnapshotValidateDriver{}.Validate(source)
}

func (d browserSnapshotDriver) Probe(ctx context.Context, source Source) (Observation, error) {
	if d.client == nil {
		return Observation{}, fmt.Errorf("%w: browser client not configured", ErrInvalidSource)
	}
	resp, err := d.client.Do(ctx, browsertypes.Request{
		Action: browsertypes.ActionCreateSession,
		Params: map[string]any{"url": strings.TrimSpace(source.BrowserSnapshot.URL)},
	})
	if err != nil {
		return Observation{}, err
	}
	sessionID := strings.TrimSpace(resp.SessionID)
	if sessionID == "" && resp.Data != nil {
		if id, _ := resp.Data["session_id"].(string); strings.TrimSpace(id) != "" {
			sessionID = strings.TrimSpace(id)
		}
	}
	if sessionID == "" {
		return Observation{}, fmt.Errorf("browser snapshot: empty session id")
	}
	defer func() {
		_, _ = d.client.Do(context.WithoutCancel(ctx), browsertypes.Request{
			Action:    browsertypes.ActionCloseSession,
			SessionID: sessionID,
		})
	}()
	snap, err := d.client.Do(ctx, browsertypes.Request{
		Action:    browsertypes.ActionSnapshot,
		SessionID: sessionID,
	})
	if err != nil {
		return Observation{}, err
	}
	if !snap.OK && snap.Error != "" {
		return Observation{}, fmt.Errorf("browser: %s", snap.Error)
	}
	html, _ := snap.Data["html"].(string)
	return observationFromBytes([]byte(html)), nil
}

func (d browserSnapshotDriver) Describe(source Source) string {
	return browserSnapshotValidateDriver{}.Describe(source)
}

func fetchObservation(ctx context.Context, rawURL string) (Observation, error) {
	reqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), httpProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Observation{}, err
	}
	req.Header.Set("User-Agent", "HopClaw/watch")
	resp, err := (&http.Client{Timeout: httpProbeTimeout}).Do(req)
	if err != nil {
		return Observation{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, httpProbeBodySize))
	if err != nil {
		return Observation{}, err
	}
	return observationFromBytes(body), nil
}

func observationFromBytes(body []byte) Observation {
	fingerprintBytes := sha256.Sum256(body)
	text := strings.TrimSpace(string(body))
	return Observation{
		Summary:     compactSummary(text),
		Body:        text,
		Fingerprint: hex.EncodeToString(fingerprintBytes[:]),
	}
}

func hashStrings(items ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(items, "\n")))
	return hex.EncodeToString(sum[:])
}

func parseFeedItems(raw string) []string {
	type item struct {
		Title string `xml:"title"`
		Link  string `xml:"link"`
		GUID  string `xml:"guid"`
		Date  string `xml:"pubDate"`
	}
	type entry struct {
		Title   string `xml:"title"`
		ID      string `xml:"id"`
		Updated string `xml:"updated"`
		Link    struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
	}
	type rss struct {
		Channel struct {
			Items []item `xml:"item"`
		} `xml:"channel"`
	}
	type atom struct {
		Entries []entry `xml:"entry"`
	}
	lines := make([]string, 0, 16)
	var rssDoc rss
	if err := xml.Unmarshal([]byte(raw), &rssDoc); err == nil && len(rssDoc.Channel.Items) > 0 {
		for _, it := range rssDoc.Channel.Items {
			lines = append(lines, strings.TrimSpace(it.Date+" | "+normalize.FirstNonEmpty(it.GUID, it.Link)+" | "+it.Title))
			if len(lines) == 20 {
				return lines
			}
		}
	}
	var atomDoc atom
	if err := xml.Unmarshal([]byte(raw), &atomDoc); err == nil && len(atomDoc.Entries) > 0 {
		for _, it := range atomDoc.Entries {
			lines = append(lines, strings.TrimSpace(it.Updated+" | "+normalize.FirstNonEmpty(it.ID, it.Link.Href)+" | "+it.Title))
			if len(lines) == 20 {
				return lines
			}
		}
	}
	return lines
}

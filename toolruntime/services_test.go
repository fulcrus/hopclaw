package toolruntime

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
)

func TestSearchNotConfigured(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	reg := NewLayer2Registry(Layer2Config{Root: root})
	ctx := context.Background()

	results, err := reg.ExecuteBatch(ctx, &agent.Run{ID: "r"}, &agent.Session{ID: "s"}, []agent.ToolCall{{
		ID: "c1", Name: "search.web", Input: map[string]any{"query": "test"},
	}})
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	json.Unmarshal([]byte(results[0].Content), &out)
	if out["status"] != "not_configured" {
		t.Fatalf("expected not_configured, got %v", out["status"])
	}
}

func TestSearchWithConfig(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"title": "Result for " + q, "url": "https://example.com"},
			},
		})
	}))
	defer ts.Close()

	root := t.TempDir()
	reg := NewLayer2Registry(Layer2Config{
		Root: root,
		Services: ServicesConfig{
			Search: SearchServiceConfig{
				Provider: "serpapi",
				BaseURL:  ts.URL,
				APIKey:   "test-key",
			},
		},
	})
	ctx := context.Background()

	results, err := reg.ExecuteBatch(ctx, &agent.Run{ID: "r"}, &agent.Session{ID: "s"}, []agent.ToolCall{{
		ID: "c1", Name: "search.web", Input: map[string]any{"query": "hello world"},
	}})
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	json.Unmarshal([]byte(results[0].Content), &out)

	if out["provider"] != "serpapi" {
		t.Fatalf("expected provider 'serpapi', got %v", out["provider"])
	}
	if out["query"] != "hello world" {
		t.Fatalf("expected query 'hello world', got %v", out["query"])
	}
	if out["status_code"].(float64) != 200 {
		t.Fatalf("expected status_code 200, got %v", out["status_code"])
	}
}

func TestSearchGenericProvider(t *testing.T) {
	t.Parallel()

	var receivedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer ts.Close()

	root := t.TempDir()
	reg := NewLayer2Registry(Layer2Config{
		Root: root,
		Services: ServicesConfig{
			Search: SearchServiceConfig{
				Provider: "generic",
				BaseURL:  ts.URL,
			},
		},
	})
	ctx := context.Background()

	results, err := reg.ExecuteBatch(ctx, &agent.Run{ID: "r"}, &agent.Session{ID: "s"}, []agent.ToolCall{{
		ID: "c1", Name: "search.news", Input: map[string]any{"query": "breaking"},
	}})
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	json.Unmarshal([]byte(results[0].Content), &out)

	if out["status_code"].(float64) != 200 {
		t.Fatalf("expected 200, got %v", out["status_code"])
	}
	if receivedBody["query"] != "breaking" {
		t.Fatalf("expected query 'breaking' in POST body, got %v", receivedBody["query"])
	}
}

func TestSearchTavilyProvider(t *testing.T) {
	t.Parallel()

	var receivedBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer ts.Close()

	root := t.TempDir()
	reg := NewLayer2Registry(Layer2Config{
		Root: root,
		Services: ServicesConfig{
			Search: SearchServiceConfig{
				Provider: "tavily",
				BaseURL:  ts.URL,
				APIKey:   "tvly-key",
			},
		},
	})
	ctx := context.Background()

	_, err := reg.ExecuteBatch(ctx, &agent.Run{ID: "r"}, &agent.Session{ID: "s"}, []agent.ToolCall{{
		ID: "c1", Name: "search.web", Input: map[string]any{"query": "test"},
	}})
	if err != nil {
		t.Fatal(err)
	}

	// Verify Tavily sends api_key in POST body.
	if receivedBody["api_key"] != "tvly-key" {
		t.Fatalf("expected api_key in body, got %v", receivedBody["api_key"])
	}
	if receivedBody["query"] != "test" {
		t.Fatalf("expected query in body, got %v", receivedBody["query"])
	}
}

func TestEmailNotConfigured(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	reg := NewLayer2Registry(Layer2Config{Root: root})
	ctx := context.Background()

	results, err := reg.ExecuteBatch(ctx, &agent.Run{ID: "r"}, &agent.Session{ID: "s"}, []agent.ToolCall{{
		ID: "c1", Name: "email.send", Input: map[string]any{
			"to": "a@b.com", "subject": "hi", "body": "hello",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	json.Unmarshal([]byte(results[0].Content), &out)
	if out["status"] != "not_configured" {
		t.Fatalf("expected not_configured, got %v", out["status"])
	}
}

func TestEmailIMAPListReadSearch(t *testing.T) {
	t.Parallel()

	server := newFakeIMAPServer(t, []fakeIMAPMessage{
		{UID: "101", Subject: "Welcome", From: "ops@example.com", To: "user@example.com", Date: "16-Mar-2026 09:00:00 +0000", Body: "hello from ops"},
		{UID: "102", Subject: "Invoice due", From: "billing@example.com", To: "user@example.com", Date: "16-Mar-2026 10:00:00 +0000", Body: "invoice is attached"},
	})
	defer server.Close()

	root := t.TempDir()
	reg := NewLayer2Registry(Layer2Config{
		Root: root,
		Services: ServicesConfig{
			Email: EmailServiceConfig{
				IMAPHost: server.Host,
				IMAPPort: server.Port,
				Username: "user@example.com",
				Password: "secret",
			},
		},
	})
	ctx := context.Background()

	listResults, err := reg.ExecuteBatch(ctx, &agent.Run{ID: "r"}, &agent.Session{ID: "s"}, []agent.ToolCall{{
		ID: "c1", Name: "email.list", Input: map[string]any{"limit": 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var listOut struct {
		Count int `json:"count"`
		Items []struct {
			ID      string `json:"id"`
			Subject string `json:"subject"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(listResults[0].Content), &listOut); err != nil {
		t.Fatalf("email.list unmarshal: %v", err)
	}
	if listOut.Count != 2 {
		t.Fatalf("email.list count = %d", listOut.Count)
	}
	if len(listOut.Items) != 2 || listOut.Items[0].ID != "102" {
		t.Fatalf("email.list items = %+v", listOut.Items)
	}

	searchResults, err := reg.ExecuteBatch(ctx, &agent.Run{ID: "r"}, &agent.Session{ID: "s"}, []agent.ToolCall{{
		ID: "c2", Name: "email.search", Input: map[string]any{"query": "invoice"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var searchOut struct {
		Count int `json:"count"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(searchResults[0].Content), &searchOut); err != nil {
		t.Fatalf("email.search unmarshal: %v", err)
	}
	if searchOut.Count != 1 || len(searchOut.Items) != 1 || searchOut.Items[0].ID != "102" {
		t.Fatalf("email.search result = %+v", searchOut)
	}

	readResults, err := reg.ExecuteBatch(ctx, &agent.Run{ID: "r"}, &agent.Session{ID: "s"}, []agent.ToolCall{{
		ID: "c3", Name: "email.read", Input: map[string]any{"id": "102"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var readOut struct {
		Item struct {
			ID      string `json:"id"`
			Subject string `json:"subject"`
			Body    string `json:"body"`
		} `json:"item"`
	}
	if err := json.Unmarshal([]byte(readResults[0].Content), &readOut); err != nil {
		t.Fatalf("email.read unmarshal: %v", err)
	}
	if readOut.Item.ID != "102" || readOut.Item.Subject != "Invoice due" {
		t.Fatalf("email.read item = %+v", readOut.Item)
	}
	if !strings.Contains(readOut.Item.Body, "invoice is attached") {
		t.Fatalf("email.read body = %q", readOut.Item.Body)
	}
}

type fakeIMAPMessage struct {
	UID     string
	Subject string
	From    string
	To      string
	Date    string
	Body    string
}

type fakeIMAPServer struct {
	Host string
	Port int
	ln   net.Listener
}

func (s *fakeIMAPServer) Close() {
	if s != nil && s.ln != nil {
		_ = s.ln.Close()
	}
}

func newFakeIMAPServer(t *testing.T, messages []fakeIMAPMessage) *fakeIMAPServer {
	t.Helper()
	cert := mustSelfSignedCert(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	})
	if err != nil {
		t.Fatalf("tls.Listen() error = %v", err)
	}
	server := &fakeIMAPServer{ln: ln}
	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	server.Host = host
	server.Port, _ = strconv.Atoi(portStr)

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go serveFakeIMAPConn(conn, messages)
		}
	}()
	return server
}

func serveFakeIMAPConn(conn net.Conn, messages []fakeIMAPMessage) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	write := func(format string, args ...any) {
		_, _ = fmt.Fprintf(w, format, args...)
		_ = w.Flush()
	}
	write("* OK fake imap ready\r\n")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 2 {
			continue
		}
		tag := parts[0]
		cmd := strings.ToUpper(parts[1])
		arg := ""
		if len(parts) > 2 {
			arg = parts[2]
		}
		switch cmd {
		case "LOGIN":
			write("%s OK LOGIN completed\r\n", tag)
		case "SELECT":
			write("* %d EXISTS\r\n%s OK [READ-WRITE] SELECT completed\r\n", len(messages), tag)
		case "UID":
			handleFakeIMAPUID(write, tag, arg, messages)
		case "LOGOUT":
			write("* BYE logging out\r\n%s OK LOGOUT completed\r\n", tag)
			return
		default:
			write("%s OK %s completed\r\n", tag, cmd)
		}
	}
}

func handleFakeIMAPUID(write func(string, ...any), tag string, arg string, messages []fakeIMAPMessage) {
	argUpper := strings.ToUpper(arg)
	switch {
	case strings.HasPrefix(argUpper, "SEARCH "):
		query := ""
		if idx := strings.Index(argUpper, "TEXT "); idx >= 0 {
			query = strings.Trim(strings.TrimSpace(arg[idx+5:]), `"`)
		}
		ids := make([]string, 0, len(messages))
		for _, msg := range messages {
			if query == "" || strings.Contains(strings.ToLower(msg.Subject+" "+msg.From+" "+msg.Body), strings.ToLower(query)) {
				ids = append(ids, msg.UID)
			}
		}
		write("* SEARCH %s\r\n%s OK SEARCH completed\r\n", strings.Join(ids, " "), tag)
	case strings.HasPrefix(argUpper, "FETCH "):
		fields := strings.Fields(arg)
		if len(fields) < 2 {
			write("%s BAD invalid FETCH\r\n", tag)
			return
		}
		uid := fields[1]
		msg, ok := fakeIMAPFind(messages, uid)
		if !ok {
			write("%s NO no such message\r\n", tag)
			return
		}
		if strings.Contains(argUpper, "HEADER.FIELDS") {
			headers := fmt.Sprintf("Subject: %s\r\nFrom: %s\r\nTo: %s\r\nDate: %s\r\n\r\n", msg.Subject, msg.From, msg.To, msg.Date)
			write("* 1 FETCH (UID %s RFC822.SIZE %d INTERNALDATE %q BODY[HEADER.FIELDS (SUBJECT FROM TO DATE)] {%d}\r\n%s\r\n)\r\n%s OK FETCH completed\r\n",
				msg.UID, len(msg.Body), msg.Date, len(headers), headers, tag)
			return
		}
		body := msg.Body
		write("* 1 FETCH (UID %s BODY[TEXT] {%d}\r\n%s\r\n)\r\n%s OK FETCH completed\r\n", msg.UID, len(body), body, tag)
	default:
		write("%s BAD unsupported UID command\r\n", tag)
	}
}

func fakeIMAPFind(messages []fakeIMAPMessage, uid string) (fakeIMAPMessage, bool) {
	for _, msg := range messages {
		if msg.UID == uid {
			return msg, true
		}
	}
	return fakeIMAPMessage{}, false
}

func mustSelfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair() error = %v", err)
	}
	return cert
}

func TestSpeechNotConfigured(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	reg := NewLayer2Registry(Layer2Config{Root: root})
	ctx := context.Background()

	results, err := reg.ExecuteBatch(ctx, &agent.Run{ID: "r"}, &agent.Session{ID: "s"}, []agent.ToolCall{{
		ID: "c1", Name: "speech.tts", Input: map[string]any{
			"text": "hello", "output": "out.wav",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	json.Unmarshal([]byte(results[0].Content), &out)
	if out["status"] != "not_configured" {
		t.Fatalf("expected not_configured, got %v", out["status"])
	}
}

func TestSpeechTTSWithConfig(t *testing.T) {
	t.Parallel()

	audioData := []byte("fake-audio-data-mp3")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/speech" {
			t.Errorf("expected path /audio/speech, got %s", r.URL.Path)
		}
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-speech-key" {
			t.Errorf("expected bearer token, got %q", auth)
		}
		w.Write(audioData)
	}))
	defer ts.Close()

	root := t.TempDir()
	reg := NewLayer2Registry(Layer2Config{
		Root: root,
		Services: ServicesConfig{
			Speech: SpeechServiceConfig{
				BaseURL: ts.URL,
				APIKey:  "test-speech-key",
				Model:   "tts-1",
			},
		},
	})
	ctx := context.Background()

	results, err := reg.ExecuteBatch(ctx, &agent.Run{ID: "r"}, &agent.Session{ID: "s"}, []agent.ToolCall{{
		ID: "c1", Name: "speech.tts", Input: map[string]any{
			"text":   "hello world",
			"output": "output.mp3",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	json.Unmarshal([]byte(results[0].Content), &out)

	if out["success"] != true {
		t.Fatalf("expected success=true, got %v. full: %s", out["success"], results[0].Content)
	}
	if out["bytes"].(float64) != float64(len(audioData)) {
		t.Fatalf("expected %d bytes, got %v", len(audioData), out["bytes"])
	}

	// Verify the file was written.
	data, err := os.ReadFile(filepath.Join(root, "output.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(audioData) {
		t.Fatalf("file content mismatch")
	}
}

func TestSpeechSTTWithConfig(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" {
			t.Errorf("expected path /audio/transcriptions, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"text": "hello world transcribed",
		})
	}))
	defer ts.Close()

	root := t.TempDir()
	// Create a fake audio file.
	audioPath := filepath.Join(root, "input.wav")
	os.WriteFile(audioPath, []byte("fake-audio"), 0o644)

	reg := NewLayer2Registry(Layer2Config{
		Root: root,
		Services: ServicesConfig{
			Speech: SpeechServiceConfig{
				BaseURL: ts.URL,
				APIKey:  "test-stt-key",
			},
		},
	})
	ctx := context.Background()

	results, err := reg.ExecuteBatch(ctx, &agent.Run{ID: "r"}, &agent.Session{ID: "s"}, []agent.ToolCall{{
		ID: "c1", Name: "speech.stt", Input: map[string]any{
			"input": "input.wav",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	json.Unmarshal([]byte(results[0].Content), &out)

	if out["success"] != true {
		t.Fatalf("expected success=true, got %v. full: %s", out["success"], results[0].Content)
	}

	result := out["result"].(map[string]any)
	if result["text"] != "hello world transcribed" {
		t.Fatalf("expected transcription text, got %v", result["text"])
	}
}

func TestServicesConfigIsConfigured(t *testing.T) {
	t.Parallel()

	// Search.
	if (SearchServiceConfig{}).IsConfigured() {
		t.Fatal("empty search should not be configured")
	}
	if !(SearchServiceConfig{APIKey: "k"}).IsConfigured() {
		t.Fatal("search with APIKey should be configured")
	}
	if !(SearchServiceConfig{BaseURL: "http://example.com"}).IsConfigured() {
		t.Fatal("search with BaseURL should be configured")
	}

	// Email.
	if (EmailServiceConfig{}).IsConfigured() {
		t.Fatal("empty email should not be configured")
	}
	if !(EmailServiceConfig{SMTPHost: "smtp.example.com"}).IsConfigured() {
		t.Fatal("email with SMTPHost should be configured")
	}

	// Speech.
	if (SpeechServiceConfig{}).IsConfigured() {
		t.Fatal("empty speech should not be configured")
	}
	if (SpeechServiceConfig{BaseURL: "http://example.com"}).IsConfigured() {
		t.Fatal("speech with only BaseURL should not be configured (needs APIKey)")
	}
	if !(SpeechServiceConfig{BaseURL: "http://example.com", APIKey: "k"}).IsConfigured() {
		t.Fatal("speech with BaseURL+APIKey should be configured")
	}
}

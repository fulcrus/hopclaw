package watch

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/internal/imaputil"
)

const (
	defaultIMAPPort     = 993
	imapTimeout         = 20 * time.Second
	imapHeaderFetchSpec = "BODY.PEEK[HEADER.FIELDS (SUBJECT FROM DATE)]"
)

type mailboxMessage struct {
	ID      string
	Subject string
	From    string
	Date    string
}

type mailboxIMAPClient struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
	tag  int
}

func openMailboxIMAP(ctx context.Context, cfg EmailConfig) (*mailboxIMAPClient, error) {
	host := strings.TrimSpace(cfg.IMAPHost)
	if host == "" {
		return nil, fmt.Errorf("email IMAP host is not configured")
	}
	port := cfg.IMAPPort
	if port <= 0 {
		port = defaultIMAPPort
	}
	address := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: imapTimeout}
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
	client := &mailboxIMAPClient{
		conn: conn,
		r:    bufio.NewReader(conn),
		w:    bufio.NewWriter(conn),
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(imapTimeout))
	}
	if _, err := client.readLine(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("imap greeting: %w", err)
	}
	return client, nil
}

func (c *mailboxIMAPClient) close() {
	if c == nil || c.conn == nil {
		return
	}
	_, _ = c.run("LOGOUT")
	_ = c.conn.Close()
}

func (c *mailboxIMAPClient) login(username string, password string) error {
	_, err := c.run("LOGIN " + imaputil.Quote(username) + " " + imaputil.Quote(password))
	return err
}

func (c *mailboxIMAPClient) selectMailbox(folder string) error {
	_, err := c.run("SELECT " + imaputil.Quote(folder))
	return err
}

func (c *mailboxIMAPClient) searchUIDs(criteria string) ([]string, error) {
	lines, err := c.run("UID SEARCH " + criteria)
	if err != nil {
		return nil, err
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "* SEARCH ") {
			continue
		}
		ids := strings.Fields(strings.TrimPrefix(line, "* SEARCH "))
		return ids, nil
	}
	return nil, nil
}

func (c *mailboxIMAPClient) fetchRecentMetadata(ids []string, limit int) ([]mailboxMessage, error) {
	if len(ids) == 0 || limit <= 0 {
		return nil, nil
	}
	if limit > len(ids) {
		limit = len(ids)
	}
	selected := ids[len(ids)-limit:]
	out := make([]mailboxMessage, 0, len(selected))
	for i := len(selected) - 1; i >= 0; i-- {
		msg, err := c.fetchMetadata(selected[i])
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

func (c *mailboxIMAPClient) fetchMetadata(uid string) (mailboxMessage, error) {
	lines, literals, err := c.runFetch("UID FETCH " + uid + " (" + imapHeaderFetchSpec + ")")
	if err != nil {
		return mailboxMessage{}, err
	}
	msg := mailboxMessage{ID: uid}
	if len(literals) > 0 {
		headers := imaputil.ParseHeaderBlock(string(literals[0]))
		msg.Subject = headers["subject"]
		msg.From = headers["from"]
		msg.Date = headers["date"]
	}
	if msg.Subject == "" && len(lines) > 0 {
		msg.Subject = strings.Join(lines, " ")
	}
	return msg, nil
}

func (c *mailboxIMAPClient) run(command string) ([]string, error) {
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
			if strings.Contains(line, " OK ") || strings.HasSuffix(line, " OK") {
				return lines, nil
			}
			return nil, fmt.Errorf("imap command failed: %s", line)
		}
	}
}

func (c *mailboxIMAPClient) runFetch(command string) ([]string, [][]byte, error) {
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
		if size, ok := imaputil.ParseLiteralSize(line); ok {
			buf := make([]byte, size)
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
			if strings.Contains(line, " OK ") || strings.HasSuffix(line, " OK") {
				return lines, literals, nil
			}
			return nil, nil, fmt.Errorf("imap command failed: %s", line)
		}
	}
}

func (c *mailboxIMAPClient) nextTag() string {
	c.tag++
	return fmt.Sprintf("A%04d", c.tag)
}

func (c *mailboxIMAPClient) readLine() (string, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

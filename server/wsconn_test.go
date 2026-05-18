package server

import (
	"context"
	"strings"
	"testing"
)

func TestWSConnCloseCancelsContext(t *testing.T) {
	t.Parallel()

	conn := newWSConn(context.TODO(), "conn-1", nil, nil)
	select {
	case <-conn.context().Done():
		t.Fatal("connection context should be active before close")
	default:
	}

	conn.Close()

	select {
	case <-conn.context().Done():
	default:
		t.Fatal("connection context should be cancelled after close")
	}
}

func TestWSConnCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	conn := newWSConn(context.TODO(), "conn-1", nil, nil)
	conn.Close()
	conn.Close()

	select {
	case <-conn.done:
	default:
		t.Fatal("done channel should stay closed after repeated Close()")
	}
}

func TestWSConnSendAfterCloseFailsWithoutBuffering(t *testing.T) {
	t.Parallel()

	conn := newWSConn(context.TODO(), "conn-1", nil, nil)
	conn.Close()

	err := conn.send([]byte("hello"))
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("send() error = %v, want closed error", err)
	}
	if conn.bufferedBytes != 0 {
		t.Fatalf("bufferedBytes = %d, want 0", conn.bufferedBytes)
	}
	if got := len(conn.sendCh); got != 0 {
		t.Fatalf("len(sendCh) = %d, want 0", got)
	}
}

func TestWSConnSendChannelFullRollsBackBufferAndCloses(t *testing.T) {
	t.Parallel()

	conn := newWSConn(context.TODO(), "conn-1", nil, nil)
	conn.sendCh = make(chan []byte)

	err := conn.send([]byte("hello"))
	if err == nil || !strings.Contains(err.Error(), "send channel full") {
		t.Fatalf("send() error = %v, want send channel full", err)
	}
	if conn.bufferedBytes != 0 {
		t.Fatalf("bufferedBytes = %d, want 0", conn.bufferedBytes)
	}
	select {
	case <-conn.context().Done():
	default:
		t.Fatal("connection context should be cancelled after channel-full close")
	}
}

func TestWSConnSendReservesBufferOnlyAfterQueueing(t *testing.T) {
	t.Parallel()

	conn := newWSConn(context.TODO(), "conn-1", nil, nil)
	conn.sendCh = make(chan []byte)

	if err := conn.send([]byte("hello")); err == nil || !strings.Contains(err.Error(), "send channel full") {
		t.Fatalf("send() error = %v, want send channel full", err)
	}
	if conn.bufferedBytes != 0 {
		t.Fatalf("bufferedBytes = %d, want 0", conn.bufferedBytes)
	}
}

func TestWSConnSendBufferOverflowClosesConnection(t *testing.T) {
	t.Parallel()

	conn := newWSConn(context.TODO(), "conn-1", nil, nil)
	conn.bufferedBytes = int64(wsMaxBufferedBytes) - 1

	err := conn.send([]byte("ab"))
	if err == nil || !strings.Contains(err.Error(), "send buffer overflow") {
		t.Fatalf("send() error = %v, want send buffer overflow", err)
	}
	if conn.bufferedBytes != int64(wsMaxBufferedBytes)-1 {
		t.Fatalf("bufferedBytes = %d, want %d", conn.bufferedBytes, int64(wsMaxBufferedBytes)-1)
	}
	select {
	case <-conn.context().Done():
	default:
		t.Fatal("connection context should be cancelled after overflow close")
	}
}

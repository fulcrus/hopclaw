package model

import (
	"context"
	"crypto/sha256"
	"sync"

	"github.com/fulcrus/hopclaw/agent"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// defaultEmbeddingCacheSize is the maximum number of cached embeddings.
	defaultEmbeddingCacheSize = 1024
)

// ---------------------------------------------------------------------------
// CachedEmbeddingClient
// ---------------------------------------------------------------------------

// CachedEmbeddingClient wraps an EmbeddingClient with a hash-based LRU cache
// to avoid redundant API calls for identical texts. Thread-safe.
type CachedEmbeddingClient struct {
	inner   agent.EmbeddingClient
	maxSize int

	mu      sync.Mutex
	entries map[[sha256.Size]byte]*cacheNode
	order   cacheList // doubly-linked list for LRU eviction
}

// NewCachedEmbeddingClient creates a cached wrapper around inner. maxSize
// controls the maximum number of cached vectors; use 0 for the default (1024).
func NewCachedEmbeddingClient(inner agent.EmbeddingClient, maxSize int) *CachedEmbeddingClient {
	if maxSize <= 0 {
		maxSize = defaultEmbeddingCacheSize
	}
	return &CachedEmbeddingClient{
		inner:   inner,
		maxSize: maxSize,
		entries: make(map[[sha256.Size]byte]*cacheNode, maxSize),
	}
}

// Embed returns cached embeddings for texts that have been seen before and
// delegates to the inner client only for cache misses. Results are assembled
// in the original input order.
func (c *CachedEmbeddingClient) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	results := make([][]float32, len(texts))
	hashes := make([][sha256.Size]byte, len(texts))

	// Phase 1: resolve cache hits.
	var missIndices []int
	var missTexts []string

	c.mu.Lock()
	for i, text := range texts {
		h := sha256.Sum256([]byte(text))
		hashes[i] = h
		if node, ok := c.entries[h]; ok {
			c.order.moveToFront(node)
			results[i] = node.vector
		} else {
			missIndices = append(missIndices, i)
			missTexts = append(missTexts, text)
		}
	}
	c.mu.Unlock()

	if len(missTexts) == 0 {
		return results, nil
	}

	// Phase 2: embed cache misses.
	vectors, err := c.inner.Embed(ctx, missTexts)
	if err != nil {
		return nil, err
	}

	// Phase 3: populate cache with new results.
	c.mu.Lock()
	for j, idx := range missIndices {
		vec := vectors[j]
		results[idx] = vec
		c.put(hashes[idx], vec)
	}
	c.mu.Unlock()

	return results, nil
}

// put inserts or updates a cache entry. Must be called with c.mu held.
func (c *CachedEmbeddingClient) put(hash [sha256.Size]byte, vec []float32) {
	if node, ok := c.entries[hash]; ok {
		node.vector = vec
		c.order.moveToFront(node)
		return
	}

	node := &cacheNode{hash: hash, vector: vec}
	c.entries[hash] = node
	c.order.pushFront(node)

	// Evict least-recently-used entries when over capacity.
	for len(c.entries) > c.maxSize {
		tail := c.order.back()
		if tail == nil {
			break
		}
		c.order.remove(tail)
		delete(c.entries, tail.hash)
	}
}

// Len returns the current number of cached embeddings.
func (c *CachedEmbeddingClient) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// ---------------------------------------------------------------------------
// Doubly-linked list for LRU ordering
// ---------------------------------------------------------------------------

type cacheNode struct {
	hash   [sha256.Size]byte
	vector []float32
	prev   *cacheNode
	next   *cacheNode
}

type cacheList struct {
	head *cacheNode
	tail *cacheNode
}

func (l *cacheList) pushFront(n *cacheNode) {
	n.prev = nil
	n.next = l.head
	if l.head != nil {
		l.head.prev = n
	}
	l.head = n
	if l.tail == nil {
		l.tail = n
	}
}

func (l *cacheList) moveToFront(n *cacheNode) {
	if l.head == n {
		return
	}
	l.remove(n)
	l.pushFront(n)
}

func (l *cacheList) remove(n *cacheNode) {
	if n.prev != nil {
		n.prev.next = n.next
	} else {
		l.head = n.next
	}
	if n.next != nil {
		n.next.prev = n.prev
	} else {
		l.tail = n.prev
	}
	n.prev = nil
	n.next = nil
}

func (l *cacheList) back() *cacheNode {
	return l.tail
}

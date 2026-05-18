package media

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
)

// ---------------------------------------------------------------------------
// Batch processing constants
// ---------------------------------------------------------------------------

const (
	// defaultBatchParallelism is the default number of concurrent analysis requests.
	defaultBatchParallelism = 4

	// maxBatchParallelism is the upper bound on concurrent analysis requests.
	maxBatchParallelism = 16

	// maxBatchSize is the maximum number of images in a single batch.
	maxBatchSize = 100
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

var (
	// ErrBatchEmpty is returned when an empty batch is submitted.
	ErrBatchEmpty = fmt.Errorf("batch is empty")

	// ErrBatchTooLarge is returned when the batch exceeds maxBatchSize.
	ErrBatchTooLarge = fmt.Errorf("batch exceeds maximum size of %d", maxBatchSize)
)

// ---------------------------------------------------------------------------
// Batch configuration and callbacks
// ---------------------------------------------------------------------------

// BatchConfig controls the behaviour of batch image processing.
type BatchConfig struct {
	// Parallelism is the maximum number of concurrent analysis requests.
	// Defaults to defaultBatchParallelism, capped at maxBatchParallelism.
	Parallelism int `json:"parallelism" yaml:"parallelism"`

	// Deduplicate enables skipping images with identical content hashes.
	Deduplicate bool `json:"deduplicate" yaml:"deduplicate"`

	// ProgressFn is called after each request completes (success or failure).
	// The arguments are (completed count, total count, latest result or nil, error or nil).
	// It is safe to set this to nil.
	ProgressFn func(completed, total int, result *ImageAnalysisResult, err error) `json:"-"`
}

// effectiveParallelism returns the parallelism clamped to valid bounds.
func (c BatchConfig) effectiveParallelism() int {
	if c.Parallelism <= 0 {
		return defaultBatchParallelism
	}
	if c.Parallelism > maxBatchParallelism {
		return maxBatchParallelism
	}
	return c.Parallelism
}

// ---------------------------------------------------------------------------
// Batch result types
// ---------------------------------------------------------------------------

// BatchResult holds the aggregated output from batch image processing.
type BatchResult struct {
	// Results contains the analysis result for each successfully processed image.
	// The index corresponds to the original request index.
	Results []BatchItem `json:"results"`

	// TotalRequested is the number of images submitted (before deduplication).
	TotalRequested int `json:"total_requested"`

	// TotalProcessed is the number of images actually sent for analysis.
	TotalProcessed int `json:"total_processed"`

	// TotalSucceeded is the number of images that completed successfully.
	TotalSucceeded int `json:"total_succeeded"`

	// TotalFailed is the number of images that failed.
	TotalFailed int `json:"total_failed"`

	// TotalSkipped is the number of images skipped due to deduplication.
	TotalSkipped int `json:"total_skipped"`
}

// BatchItem holds the result (or error) for a single image in the batch.
type BatchItem struct {
	// Index is the original position in the request slice.
	Index int `json:"index"`

	// Result holds the analysis output if successful.
	Result *ImageAnalysisResult `json:"result,omitempty"`

	// Error holds the error message if the analysis failed.
	Error string `json:"error,omitempty"`

	// Skipped is true if the image was deduplicated.
	Skipped bool `json:"skipped,omitempty"`

	// DuplicateOf is the index of the original image this one duplicates.
	DuplicateOf int `json:"duplicate_of,omitempty"`
}

// ---------------------------------------------------------------------------
// BatchAnalyzer
// ---------------------------------------------------------------------------

// BatchAnalyzer provides efficient batch image analysis with configurable
// parallelism, rate limiting, deduplication, and partial failure handling.
type BatchAnalyzer struct {
	analyzer *VisionAnalyzer
}

// NewBatchAnalyzer creates a BatchAnalyzer using the given VisionAnalyzer.
func NewBatchAnalyzer(analyzer *VisionAnalyzer) *BatchAnalyzer {
	return &BatchAnalyzer{analyzer: analyzer}
}

// ---------------------------------------------------------------------------
// BatchAnalyze
// ---------------------------------------------------------------------------

// BatchAnalyze processes multiple image analysis requests concurrently.
// It returns results for all items, including partial failures. The returned
// BatchResult always has one BatchItem per input request.
func (b *BatchAnalyzer) BatchAnalyze(ctx context.Context, requests []ImageAnalysisRequest, config BatchConfig) (*BatchResult, error) {
	if len(requests) == 0 {
		return nil, fmt.Errorf("media/vision: %w", ErrBatchEmpty)
	}
	if len(requests) > maxBatchSize {
		return nil, fmt.Errorf("media/vision: %w", ErrBatchTooLarge)
	}

	parallelism := config.effectiveParallelism()
	items := make([]BatchItem, len(requests))

	// Phase 1: Deduplication (optional).
	skipMap := make(map[int]int) // index -> duplicateOf index
	if config.Deduplicate {
		skipMap = b.findDuplicates(requests)
	}

	// Mark skipped items.
	for idx, dupOf := range skipMap {
		items[idx] = BatchItem{
			Index:       idx,
			Skipped:     true,
			DuplicateOf: dupOf,
		}
	}

	// Phase 2: Concurrent processing.
	sem := make(chan struct{}, parallelism)
	var (
		mu        sync.Mutex // guards items, completed, succeeded, failed
		wg        sync.WaitGroup
		completed int
		succeeded int
		failed    int
	)

	total := len(requests) - len(skipMap)

	for i, req := range requests {
		if _, skipped := skipMap[i]; skipped {
			continue
		}

		wg.Add(1)
		go func(idx int, r ImageAnalysisRequest) {
			defer wg.Done()

			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			result, err := b.analyzer.AnalyzeImage(ctx, r)

			mu.Lock()
			completed++
			currentCompleted := completed
			if err != nil {
				failed++
				items[idx] = BatchItem{
					Index: idx,
					Error: err.Error(),
				}
			} else {
				succeeded++
				items[idx] = BatchItem{
					Index:  idx,
					Result: result,
				}
			}
			progressFn := config.ProgressFn
			mu.Unlock()

			if progressFn != nil {
				progressFn(currentCompleted, total, result, err)
			}
		}(i, req)
	}

	wg.Wait()

	// Phase 3: Copy results for deduplicated items.
	if config.Deduplicate {
		for idx, dupOf := range skipMap {
			if items[dupOf].Result != nil {
				items[idx].Result = items[dupOf].Result
			}
		}
	}

	return &BatchResult{
		Results:        items,
		TotalRequested: len(requests),
		TotalProcessed: total,
		TotalSucceeded: succeeded,
		TotalFailed:    failed,
		TotalSkipped:   len(skipMap),
	}, nil
}

// ---------------------------------------------------------------------------
// Deduplication
// ---------------------------------------------------------------------------

// findDuplicates identifies images with identical content by SHA-256 hash.
// Returns a map of duplicate index -> original index.
func (b *BatchAnalyzer) findDuplicates(requests []ImageAnalysisRequest) map[int]int {
	seen := make(map[[sha256.Size]byte]int) // hash -> first-seen index
	duplicates := make(map[int]int)

	for i, req := range requests {
		if len(req.Data) == 0 {
			continue
		}
		h := sha256.Sum256(req.Data)
		if firstIdx, exists := seen[h]; exists {
			duplicates[i] = firstIdx
		} else {
			seen[h] = i
		}
	}

	return duplicates
}

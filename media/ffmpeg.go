package media

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// FFmpeg constants
// ---------------------------------------------------------------------------

const (
	ffmpegBinary  = "ffmpeg"
	ffprobeBinary = "ffprobe"

	// ffmpegDefaultTimeout is the maximum time allowed for a single ffmpeg command.
	ffmpegDefaultTimeout = 5 * time.Minute

	// ffmpegMaxFrames is the default maximum number of frames to extract.
	ffmpegMaxFrames = 30

	// ffmpegDefaultFrameInterval is the default interval between extracted frames.
	ffmpegDefaultFrameInterval = 2.0 // seconds

	// ffmpegDefaultAudioFormat is the default output format for audio extraction.
	ffmpegDefaultAudioFormat = "wav"

	// ffmpegDefaultWaveformWidth is the default waveform image width.
	ffmpegDefaultWaveformWidth = 800

	// ffmpegDefaultWaveformHeight is the default waveform image height.
	ffmpegDefaultWaveformHeight = 200

	// ffmpegMinDimension is the minimum dimension for waveform output.
	ffmpegMinDimension = 16

	// ffmpegMaxDimension is the maximum dimension for waveform output.
	ffmpegMaxDimension = 4096

	// ffmpegTempDirPrefix is the prefix for temporary directories.
	ffmpegTempDirPrefix = "hopclaw-ffmpeg-"
)

// ---------------------------------------------------------------------------
// FFmpeg availability check
// ---------------------------------------------------------------------------

var (
	ffmpegOnce      sync.Once
	ffmpegAvailable bool
)

// FFmpegAvailable returns true if ffmpeg is found on the system PATH.
func FFmpegAvailable() bool {
	ffmpegOnce.Do(func() {
		_, err := exec.LookPath(ffmpegBinary)
		ffmpegAvailable = err == nil
	})
	return ffmpegAvailable
}

// ffprobeAvailable returns true if ffprobe is found on the system PATH.
func ffprobeAvailable() bool {
	_, err := exec.LookPath(ffprobeBinary)
	return err == nil
}

// ---------------------------------------------------------------------------
// MediaInfo
// ---------------------------------------------------------------------------

// MediaInfo holds metadata about a media file extracted via ffprobe.
type MediaInfo struct {
	DurationSec float64 `json:"duration_sec"`
	Width       int     `json:"width,omitempty"`
	Height      int     `json:"height,omitempty"`
	Codec       string  `json:"codec,omitempty"`
	AudioCodec  string  `json:"audio_codec,omitempty"`
	BitRate     int64   `json:"bit_rate,omitempty"`
	Format      string  `json:"format,omitempty"`
	HasAudio    bool    `json:"has_audio"`
	HasVideo    bool    `json:"has_video"`
}

// ffprobeOutput represents the JSON output from ffprobe.
type ffprobeOutput struct {
	Format  ffprobeFormat   `json:"format"`
	Streams []ffprobeStream `json:"streams"`
}

// ffprobeFormat holds format-level metadata from ffprobe.
type ffprobeFormat struct {
	Duration string `json:"duration"`
	BitRate  string `json:"bit_rate"`
	Name     string `json:"format_name"`
}

// ffprobeStream holds stream-level metadata from ffprobe.
type ffprobeStream struct {
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
}

// ---------------------------------------------------------------------------
// GetMediaInfo
// ---------------------------------------------------------------------------

// GetMediaInfo extracts metadata from media data using ffprobe.
// Returns an error if ffprobe is not available.
func GetMediaInfo(ctx context.Context, data []byte) (*MediaInfo, error) {
	if !ffprobeAvailable() {
		return nil, fmt.Errorf("media/ffmpeg: ffprobe not found on PATH")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("media/ffmpeg: media data is required")
	}

	tmpDir, cleanup, err := createTempDir()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	inputPath := filepath.Join(tmpDir, "input")
	if err := os.WriteFile(inputPath, data, 0o600); err != nil {
		return nil, fmt.Errorf("media/ffmpeg: writing temp file: %w", err)
	}

	ctx, cancel := applyTimeout(ctx, ffmpegDefaultTimeout)
	defer cancel()

	args := []string{
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		inputPath,
	}

	cmd := exec.CommandContext(ctx, ffprobeBinary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("media/ffmpeg: ffprobe failed: %w: %s", err, stderr.String())
	}

	var probe ffprobeOutput
	if err := json.Unmarshal(stdout.Bytes(), &probe); err != nil {
		return nil, fmt.Errorf("media/ffmpeg: parsing ffprobe output: %w", err)
	}

	info := &MediaInfo{
		Format: probe.Format.Name,
	}

	if dur, err := strconv.ParseFloat(probe.Format.Duration, 64); err == nil {
		info.DurationSec = dur
	}
	if br, err := strconv.ParseInt(probe.Format.BitRate, 10, 64); err == nil {
		info.BitRate = br
	}

	for _, stream := range probe.Streams {
		switch stream.CodecType {
		case "video":
			info.HasVideo = true
			info.Codec = stream.CodecName
			info.Width = stream.Width
			info.Height = stream.Height
		case "audio":
			info.HasAudio = true
			info.AudioCodec = stream.CodecName
		}
	}

	return info, nil
}

// ---------------------------------------------------------------------------
// ConvertAudio
// ---------------------------------------------------------------------------

// ConvertAudio converts audio data from one format to another using ffmpeg.
// toFormat should be a format name like "wav", "mp3", "ogg", "flac".
func ConvertAudio(ctx context.Context, data []byte, fromMIME, toFormat string) ([]byte, error) {
	if !FFmpegAvailable() {
		return nil, fmt.Errorf("media/ffmpeg: ffmpeg not found on PATH")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("media/ffmpeg: audio data is required")
	}
	if toFormat == "" {
		toFormat = ffmpegDefaultAudioFormat
	}

	tmpDir, cleanup, err := createTempDir()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	inputPath := filepath.Join(tmpDir, "input")
	outputPath := filepath.Join(tmpDir, "output."+toFormat)

	if err := os.WriteFile(inputPath, data, 0o600); err != nil {
		return nil, fmt.Errorf("media/ffmpeg: writing input file: %w", err)
	}

	ctx, cancel := applyTimeout(ctx, ffmpegDefaultTimeout)
	defer cancel()

	args := []string{
		"-y",
		"-i", inputPath,
		"-vn", // no video
		outputPath,
	}

	if err := runFFmpeg(ctx, args); err != nil {
		return nil, fmt.Errorf("media/ffmpeg: audio conversion failed: %w", err)
	}

	result, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("media/ffmpeg: reading output file: %w", err)
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// ExtractAudioFromVideo
// ---------------------------------------------------------------------------

// ExtractAudioFromVideo extracts the audio track from video data as WAV.
// Useful for sending video audio to STT providers.
func ExtractAudioFromVideo(ctx context.Context, data []byte) ([]byte, error) {
	if !FFmpegAvailable() {
		return nil, fmt.Errorf("media/ffmpeg: ffmpeg not found on PATH")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("media/ffmpeg: video data is required")
	}

	tmpDir, cleanup, err := createTempDir()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	inputPath := filepath.Join(tmpDir, "input")
	outputPath := filepath.Join(tmpDir, "audio.wav")

	if err := os.WriteFile(inputPath, data, 0o600); err != nil {
		return nil, fmt.Errorf("media/ffmpeg: writing input file: %w", err)
	}

	ctx, cancel := applyTimeout(ctx, ffmpegDefaultTimeout)
	defer cancel()

	args := []string{
		"-y",
		"-i", inputPath,
		"-vn",
		"-acodec", "pcm_s16le",
		"-ar", "16000",
		"-ac", "1",
		outputPath,
	}

	if err := runFFmpeg(ctx, args); err != nil {
		return nil, fmt.Errorf("media/ffmpeg: audio extraction failed: %w", err)
	}

	result, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("media/ffmpeg: reading output file: %w", err)
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// ExtractVideoFrames
// ---------------------------------------------------------------------------

// ExtractVideoFrames extracts keyframes from video data at the given interval.
// Returns a slice of JPEG-encoded frame images. maxFrames limits the total
// number of frames extracted (0 uses ffmpegMaxFrames).
func ExtractVideoFrames(ctx context.Context, data []byte, intervalSec float64, maxFrames int) ([][]byte, error) {
	if !FFmpegAvailable() {
		return nil, fmt.Errorf("media/ffmpeg: ffmpeg not found on PATH")
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("media/ffmpeg: video data is required")
	}
	if intervalSec <= 0 {
		intervalSec = ffmpegDefaultFrameInterval
	}
	if maxFrames <= 0 {
		maxFrames = ffmpegMaxFrames
	}

	tmpDir, cleanup, err := createTempDir()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	inputPath := filepath.Join(tmpDir, "input")
	outputPattern := filepath.Join(tmpDir, "frame_%04d.jpg")

	if err := os.WriteFile(inputPath, data, 0o600); err != nil {
		return nil, fmt.Errorf("media/ffmpeg: writing input file: %w", err)
	}

	ctx, cancel := applyTimeout(ctx, ffmpegDefaultTimeout)
	defer cancel()

	fpsFilter := fmt.Sprintf("fps=1/%s", strconv.FormatFloat(intervalSec, 'f', 2, 64))

	args := []string{
		"-y",
		"-i", inputPath,
		"-vf", fpsFilter,
		"-frames:v", strconv.Itoa(maxFrames),
		"-q:v", "2", // high quality JPEG
		outputPattern,
	}

	if err := runFFmpeg(ctx, args); err != nil {
		return nil, fmt.Errorf("media/ffmpeg: frame extraction failed: %w", err)
	}

	// Read extracted frames.
	var frames [][]byte
	for i := 1; i <= maxFrames; i++ {
		framePath := filepath.Join(tmpDir, fmt.Sprintf("frame_%04d.jpg", i))
		frameData, err := os.ReadFile(framePath)
		if err != nil {
			// No more frames.
			break
		}
		frames = append(frames, frameData)
	}

	if len(frames) == 0 {
		return nil, fmt.Errorf("media/ffmpeg: no frames extracted")
	}

	return frames, nil
}

// ---------------------------------------------------------------------------
// GenerateWaveform
// ---------------------------------------------------------------------------

// GenerateWaveform creates a waveform visualization PNG from audio data.
// Width and height control the output image dimensions.
func GenerateWaveform(ctx context.Context, audioData []byte, width, height int) ([]byte, error) {
	if !FFmpegAvailable() {
		return nil, fmt.Errorf("media/ffmpeg: ffmpeg not found on PATH")
	}
	if len(audioData) == 0 {
		return nil, fmt.Errorf("media/ffmpeg: audio data is required")
	}
	if width <= 0 {
		width = ffmpegDefaultWaveformWidth
	}
	if height <= 0 {
		height = ffmpegDefaultWaveformHeight
	}
	if width < ffmpegMinDimension || width > ffmpegMaxDimension {
		return nil, fmt.Errorf("media/ffmpeg: width must be between %d and %d", ffmpegMinDimension, ffmpegMaxDimension)
	}
	if height < ffmpegMinDimension || height > ffmpegMaxDimension {
		return nil, fmt.Errorf("media/ffmpeg: height must be between %d and %d", ffmpegMinDimension, ffmpegMaxDimension)
	}

	tmpDir, cleanup, err := createTempDir()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	inputPath := filepath.Join(tmpDir, "input")
	outputPath := filepath.Join(tmpDir, "waveform.png")

	if err := os.WriteFile(inputPath, audioData, 0o600); err != nil {
		return nil, fmt.Errorf("media/ffmpeg: writing input file: %w", err)
	}

	ctx, cancel := applyTimeout(ctx, ffmpegDefaultTimeout)
	defer cancel()

	sizeStr := fmt.Sprintf("%dx%d", width, height)
	filterStr := fmt.Sprintf("showwavespic=s=%s:colors=0x4CAF50", sizeStr)

	args := []string{
		"-y",
		"-i", inputPath,
		"-filter_complex", filterStr,
		"-frames:v", "1",
		outputPath,
	}

	if err := runFFmpeg(ctx, args); err != nil {
		return nil, fmt.Errorf("media/ffmpeg: waveform generation failed: %w", err)
	}

	result, err := os.ReadFile(outputPath)
	if err != nil {
		return nil, fmt.Errorf("media/ffmpeg: reading waveform output: %w", err)
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// createTempDir creates a temporary directory for ffmpeg operations.
// Returns the directory path and a cleanup function.
func createTempDir() (string, func(), error) {
	dir, err := os.MkdirTemp("", ffmpegTempDirPrefix)
	if err != nil {
		return "", nil, fmt.Errorf("media/ffmpeg: creating temp dir: %w", err)
	}
	cleanup := func() {
		os.RemoveAll(dir)
	}
	return dir, cleanup, nil
}

// runFFmpeg executes an ffmpeg command with the given arguments.
func runFFmpeg(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, ffmpegBinary, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, stderr.String())
	}
	return nil
}

// applyTimeout adds a timeout to the context if it does not already have a deadline.
func applyTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

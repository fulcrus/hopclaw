package toolruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"

	pdflib "github.com/ledongthuc/pdf"
)

func init() {
	RegisterLayer2GroupToggle("media", "media")
	RegisterLayer2GroupToggle("search", "search")
	RegisterLayer2GroupToggle("speech", "speech")
	RegisterLayer2GroupToggle("email", "email")
	RegisterLayer2GroupToggle("media-go", "media")
}

// ==========================================================================
// Media group
// ==========================================================================

func (r *Layer2Registry) registerMediaGroup() {
	timeout := r.config.DefaultExecTimeout
	r.registerGroup("media", []string{"ffmpeg"}, []layer2ToolDef{
		{manifest: skill.ToolManifest{
			Name: "media.audio_convert", Description: "Convert an audio file to another format using ffmpeg.",
			InputSchema: mediaConvertSchema(), OutputSchema: mediaCmdOutputSchema(),
			SideEffectClass: "local_write", RequiresApproval: true, Timeout: timeout,
		}, execFn: mediaAudioConvertExec},
		{manifest: skill.ToolManifest{
			Name: "media.audio_info", Description: "Retrieve metadata and stream info from an audio file.",
			InputSchema: mediaInfoSchema(), OutputSchema: mediaInfoOutputSchema(),
			SideEffectClass: "read", Idempotent: true, Timeout: timeout,
		}, execFn: mediaInfoExec},
		{manifest: skill.ToolManifest{
			Name: "media.video_info", Description: "Retrieve metadata and stream info from a video file.",
			InputSchema: mediaInfoSchema(), OutputSchema: mediaInfoOutputSchema(),
			SideEffectClass: "read", Idempotent: true, Timeout: timeout,
		}, execFn: mediaInfoExec},
		{manifest: skill.ToolManifest{
			Name: "media.video_thumbnail", Description: "Extract a thumbnail from a video file.",
			InputSchema: mediaThumbnailSchema(), OutputSchema: mediaCmdOutputSchema(),
			SideEffectClass: "local_write", RequiresApproval: true, Timeout: timeout,
		}, execFn: mediaVideoThumbnailExec},
		{manifest: skill.ToolManifest{
			Name: "media.video_clip", Description: "Extract a clip from a video file.",
			InputSchema: mediaClipSchema(), OutputSchema: mediaCmdOutputSchema(),
			SideEffectClass: "local_write", RequiresApproval: true, Timeout: timeout,
		}, execFn: mediaVideoClipExec},
		{manifest: skill.ToolManifest{
			Name: "media.audio_extract", Description: "Extract audio track from a video file.",
			InputSchema: mediaAudioExtractSchema(), OutputSchema: mediaCmdOutputSchema(),
			SideEffectClass: "local_write", RequiresApproval: true, Timeout: timeout,
		}, execFn: mediaAudioExtractExec},
		{manifest: skill.ToolManifest{
			Name: "media.concat", Description: "Concatenate multiple media files.",
			InputSchema: mediaConcatSchema(), OutputSchema: mediaCmdOutputSchema(),
			SideEffectClass: "local_write", RequiresApproval: true, Timeout: timeout,
		}, execFn: mediaConcatExec},
		{manifest: skill.ToolManifest{
			Name: "media.subtitle_extract", Description: "Extract subtitles from a video file.",
			InputSchema: mediaSubtitleExtractSchema(), OutputSchema: mediaCmdOutputSchema(),
			SideEffectClass: "local_write", RequiresApproval: true, Timeout: timeout,
		}, execFn: mediaSubtitleExtractExec},
		{manifest: skill.ToolManifest{
			Name: "media.metadata", Description: "Get full metadata for a media file in JSON format.",
			InputSchema: mediaMetadataSchema(), OutputSchema: mediaMetadataOutputSchema(),
			SideEffectClass: "read", Idempotent: true, Timeout: timeout,
		}, execFn: mediaMetadataExec},
	})
}

func mediaInfoExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	file, err := requiredString(call.Input, "file")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolved, err := w.resolvePath(file)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("%s file: %w", call.Name, err)
	}

	stdout, stderr, exitCode, runErr := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout,
		"ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", resolved)
	if runErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("%s failed: %w", call.Name, runErr)
	}

	// Try to parse the JSON output from ffprobe.
	var probeData any
	if jsonErr := json.Unmarshal([]byte(stdout), &probeData); jsonErr != nil {
		probeData = stdout
	}

	payload := map[string]any{
		"file":      w.displayPath(resolved),
		"info":      probeData,
		"exit_code": exitCode,
	}
	if stderr != "" {
		payload["stderr"] = stderr
	}
	body, marshalErr := json.MarshalIndent(payload, "", "  ")
	if marshalErr != nil {
		return contextengine.ToolResult{}, marshalErr
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func mediaAudioConvertExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	input, err := requiredString(call.Input, "input")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	output, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolvedInput, err := w.resolvePath(input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.audio_convert input: %w", err)
	}
	resolvedOutput, err := w.resolvePath(output)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.audio_convert output: %w", err)
	}

	stdout, stderr, exitCode, runErr := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout,
		"ffmpeg", "-i", resolvedInput, "-y", resolvedOutput)
	if runErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.audio_convert failed: %w", runErr)
	}
	return mediaCmdResult(call, stdout, stderr, exitCode)
}

func mediaVideoThumbnailExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	input, err := requiredString(call.Input, "input")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	output, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	timestamp, _ := stringFrom(call.Input["timestamp"])
	if strings.TrimSpace(timestamp) == "" {
		timestamp = "00:00:01"
	}
	resolvedInput, err := w.resolvePath(input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.video_thumbnail input: %w", err)
	}
	resolvedOutput, err := w.resolvePath(output)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.video_thumbnail output: %w", err)
	}

	stdout, stderr, exitCode, runErr := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout,
		"ffmpeg", "-i", resolvedInput, "-ss", timestamp, "-vframes", "1", "-y", resolvedOutput)
	if runErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.video_thumbnail failed: %w", runErr)
	}
	return mediaCmdResult(call, stdout, stderr, exitCode)
}

func mediaVideoClipExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	input, err := requiredString(call.Input, "input")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	output, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	start, err := requiredString(call.Input, "start")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	duration, _ := stringFrom(call.Input["duration"])
	resolvedInput, err := w.resolvePath(input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.video_clip input: %w", err)
	}
	resolvedOutput, err := w.resolvePath(output)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.video_clip output: %w", err)
	}

	args := []string{"-i", resolvedInput, "-ss", start}
	if strings.TrimSpace(duration) != "" {
		args = append(args, "-t", duration)
	}
	args = append(args, "-c", "copy", "-y", resolvedOutput)

	stdout, stderr, exitCode, runErr := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout, "ffmpeg", args...)
	if runErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.video_clip failed: %w", runErr)
	}
	return mediaCmdResult(call, stdout, stderr, exitCode)
}

// ---------------------------------------------------------------------------
// media.audio_extract
// ---------------------------------------------------------------------------

// audioFormatCodec maps audio format names to ffmpeg encoder codec names.
var audioFormatCodec = map[string]string{
	"mp3":  "libmp3lame",
	"wav":  "pcm_s16le",
	"aac":  "aac",
	"ogg":  "libvorbis",
	"flac": "flac",
}

func mediaAudioExtractExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	input, err := requiredString(call.Input, "input")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	output, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	format, _ := stringFrom(call.Input["format"])
	if strings.TrimSpace(format) == "" {
		format = "mp3"
	}
	codec, ok := audioFormatCodec[format]
	if !ok {
		return contextengine.ToolResult{}, fmt.Errorf("media.audio_extract: unsupported format %q (supported: mp3, wav, aac, ogg, flac)", format)
	}

	resolvedInput, err := w.resolvePath(input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.audio_extract input: %w", err)
	}
	resolvedOutput, err := w.resolvePath(output)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.audio_extract output: %w", err)
	}

	stdout, stderr, exitCode, runErr := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout,
		"ffmpeg", "-i", resolvedInput, "-vn", "-acodec", codec, "-y", resolvedOutput)
	if runErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.audio_extract failed: %w", runErr)
	}
	return mediaCmdResult(call, stdout, stderr, exitCode)
}

// ---------------------------------------------------------------------------
// media.concat
// ---------------------------------------------------------------------------

func mediaConcatExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	inputsRaw, err := stringSliceFrom(call.Input["inputs"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.concat inputs: %w", err)
	}
	if len(inputsRaw) == 0 {
		return contextengine.ToolResult{}, fmt.Errorf("media.concat: inputs is required and must not be empty")
	}
	output, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	resolvedOutput, err := w.resolvePath(output)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.concat output: %w", err)
	}

	// Build the concat list file content.
	var listContent strings.Builder
	for _, inp := range inputsRaw {
		resolved, resolveErr := w.resolvePath(inp)
		if resolveErr != nil {
			return contextengine.ToolResult{}, fmt.Errorf("media.concat input %q: %w", inp, resolveErr)
		}
		// Escape single quotes in paths for ffmpeg concat format.
		escaped := strings.ReplaceAll(resolved, "'", "'\\''")
		listContent.WriteString(fmt.Sprintf("file '%s'\n", escaped))
	}

	// Write the list file to a temporary file.
	listFile, err := os.CreateTemp("", "hopclaw-concat-*.txt")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.concat: create temp file: %w", err)
	}
	listFilePath := listFile.Name()
	defer os.Remove(listFilePath)

	if _, err := listFile.WriteString(listContent.String()); err != nil {
		_ = listFile.Close()
		return contextengine.ToolResult{}, fmt.Errorf("media.concat: write list file: %w", err)
	}
	if err := listFile.Close(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.concat: close list file: %w", err)
	}

	stdout, stderr, exitCode, runErr := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout,
		"ffmpeg", "-f", "concat", "-safe", "0", "-i", listFilePath, "-c", "copy", "-y", resolvedOutput)
	if runErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.concat failed: %w", runErr)
	}
	return mediaCmdResult(call, stdout, stderr, exitCode)
}

// ---------------------------------------------------------------------------
// media.subtitle_extract
// ---------------------------------------------------------------------------

func mediaSubtitleExtractExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	input, err := requiredString(call.Input, "input")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	output, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	streamIndex, err := intFrom(call.Input["stream_index"], 0)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.subtitle_extract stream_index: %w", err)
	}

	resolvedInput, err := w.resolvePath(input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.subtitle_extract input: %w", err)
	}
	resolvedOutput, err := w.resolvePath(output)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.subtitle_extract output: %w", err)
	}

	mapArg := fmt.Sprintf("0:s:%d", streamIndex)
	stdout, stderr, exitCode, runErr := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout,
		"ffmpeg", "-i", resolvedInput, "-map", mapArg, "-y", resolvedOutput)
	if runErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.subtitle_extract failed: %w", runErr)
	}
	return mediaCmdResult(call, stdout, stderr, exitCode)
}

// ---------------------------------------------------------------------------
// media.metadata
// ---------------------------------------------------------------------------

func mediaMetadataExec(ctx context.Context, w *ws, cfg BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	input, err := requiredString(call.Input, "input")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolved, err := w.resolvePath(input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.metadata input: %w", err)
	}

	stdout, stderr, exitCode, runErr := runExternalCmd(ctx, w.rootAbs, cfg.DefaultExecTimeout,
		"ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", "-show_streams", resolved)
	if runErr != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.metadata failed: %w", runErr)
	}

	// Parse the JSON output from ffprobe.
	var probeData any
	if jsonErr := json.Unmarshal([]byte(stdout), &probeData); jsonErr != nil {
		probeData = stdout
	}

	payload := map[string]any{
		"file":      w.displayPath(resolved),
		"metadata":  probeData,
		"exit_code": exitCode,
	}
	if stderr != "" {
		payload["stderr"] = stderr
	}
	body, marshalErr := json.MarshalIndent(payload, "", "  ")
	if marshalErr != nil {
		return contextengine.ToolResult{}, marshalErr
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

func mediaCmdResult(call agent.ToolCall, stdout, stderr string, exitCode int) (contextengine.ToolResult, error) {
	payload := map[string]any{
		"stdout":    stdout,
		"stderr":    stderr,
		"exit_code": exitCode,
	}
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	return contextengine.ToolResult{
		ToolName:   call.Name,
		ToolCallID: call.ID,
		Content:    string(body),
	}, nil
}

// ==========================================================================
// Search group
// ==========================================================================

func (r *Layer2Registry) registerSearchGroup() {
	timeout := r.config.DefaultExecTimeout
	r.registerGroup("search", []string{}, []layer2ToolDef{
		{manifest: skill.ToolManifest{
			Name: "search.web", Description: "Search the web for information. Use this for general web results, not for digesting today's news into a table.",
			InputSchema: searchQuerySchema(), OutputSchema: searchOutputSchema(),
			SideEffectClass: "read", Idempotent: true, Timeout: timeout,
		}, execFn: searchExec},
		{manifest: skill.ToolManifest{
			Name: "search.news", Description: "Search recent news articles. Use this when you need current news results but not a full digest.",
			InputSchema: searchQuerySchema(), OutputSchema: searchOutputSchema(),
			SideEffectClass: "read", Idempotent: true, Timeout: timeout,
		}, execFn: searchExec},
		{manifest: skill.ToolManifest{
			Name: "news.digest", Description: "Search today's or recent news, fetch top sources, and return a ready-to-use Markdown/CSV table. Prefer this over hand-rolling search + fetch + regex + fs.write for news summaries and hot-topic tables.",
			InputSchema: newsDigestSchema(), OutputSchema: newsDigestOutputSchema(),
			SideEffectClass: "read", Idempotent: true, Timeout: timeout,
		}, execFn: newsDigestExec},
	})
}

// searchExec is in services.go.

// ==========================================================================
// Speech group
// ==========================================================================

func (r *Layer2Registry) registerSpeechGroup() {
	timeout := r.config.DefaultExecTimeout
	r.registerGroup("speech", []string{}, []layer2ToolDef{
		{manifest: skill.ToolManifest{
			Name: "speech.tts", Description: "Convert text to speech audio.",
			InputSchema: speechTTSSchema(), OutputSchema: stubOutputSchema(),
			SideEffectClass: "local_write", Timeout: timeout,
		}, execFn: speechExec},
		{manifest: skill.ToolManifest{
			Name: "speech.stt", Description: "Convert speech audio to text.",
			InputSchema: speechSTTSchema(), OutputSchema: stubOutputSchema(),
			SideEffectClass: "read", Timeout: timeout,
		}, execFn: speechExec},
	})
}

// speechExec is in services.go.

// ==========================================================================
// Email group
// ==========================================================================

func (r *Layer2Registry) registerEmailGroup() {
	timeout := r.config.DefaultExecTimeout
	r.registerGroup("email", []string{}, []layer2ToolDef{
		{manifest: skill.ToolManifest{
			Name: "email.send", Description: "Send an email.",
			InputSchema: emailSendSchema(), OutputSchema: stubOutputSchema(),
			SideEffectClass: "external_write", RequiresApproval: true, Timeout: timeout,
		}, execFn: emailExec},
		{manifest: skill.ToolManifest{
			Name: "email.list", Description: "List recent emails from an IMAP folder.",
			InputSchema: emailListSchema(), OutputSchema: stubOutputSchema(),
			SideEffectClass: "read", Timeout: timeout,
		}, execFn: emailExec},
		{manifest: skill.ToolManifest{
			Name: "email.read", Description: "Read one email by IMAP UID.",
			InputSchema: emailReadSchema(), OutputSchema: stubOutputSchema(),
			SideEffectClass: "read", Timeout: timeout,
		}, execFn: emailExec},
		{manifest: skill.ToolManifest{
			Name: "email.search", Description: "Search emails in an IMAP folder.",
			InputSchema: emailSearchSchema(), OutputSchema: stubOutputSchema(),
			SideEffectClass: "read", Timeout: timeout,
		}, execFn: emailExec},
		{manifest: skill.ToolManifest{
			Name: "email.download_attachment", Description: "Download an email attachment to the workspace.",
			InputSchema: emailDownloadAttachmentSchema(), OutputSchema: stubOutputSchema(),
			SideEffectClass: "local_write", RequiresApproval: true, Timeout: timeout,
		}, execFn: emailExec},
	})
}

// emailExec is in services.go.

// ==========================================================================
// Media (pure Go) group - image processing + PDF
// ==========================================================================

func (r *Layer2Registry) registerMediaGoGroup() {
	timeout := r.config.DefaultExecTimeout
	r.registerGroup("media-go", []string{}, []layer2ToolDef{
		{manifest: skill.ToolManifest{
			Name: "media.image_resize", Description: "Resize an image to specified dimensions.",
			InputSchema: mediaImageResizeSchema(), OutputSchema: mediaImageWriteOutputSchema(),
			SideEffectClass: "local_write", RequiresApproval: true, Timeout: timeout,
		}, execFn: mediaImageResizeExec},
		{manifest: skill.ToolManifest{
			Name: "media.image_convert", Description: "Convert an image between formats (PNG, JPEG, GIF).",
			InputSchema: mediaConvertSchema(), OutputSchema: mediaImageWriteOutputSchema(),
			SideEffectClass: "local_write", RequiresApproval: true, Timeout: timeout,
		}, execFn: mediaImageConvertExec},
		{manifest: skill.ToolManifest{
			Name: "media.image_crop", Description: "Crop an image to a specified rectangle.",
			InputSchema: mediaImageCropSchema(), OutputSchema: mediaImageWriteOutputSchema(),
			SideEffectClass: "local_write", RequiresApproval: true, Timeout: timeout,
		}, execFn: mediaImageCropExec},
		{manifest: skill.ToolManifest{
			Name: "media.image_info", Description: "Read image metadata (dimensions, format, color model).",
			InputSchema: mediaInfoSchema(), OutputSchema: mediaImageInfoOutputSchema(),
			SideEffectClass: "read", Idempotent: true, Timeout: timeout,
		}, execFn: mediaImageInfoExec},
		{manifest: skill.ToolManifest{
			Name: "media.pdf_read", Description: "Extract text content from a PDF file.",
			InputSchema: mediaPdfReadSchema(), OutputSchema: mediaPdfReadOutputSchema(),
			SideEffectClass: "read", Idempotent: true, Timeout: timeout,
		}, execFn: mediaPdfReadExec},
	})
}

// --- media.image_info ---

func mediaImageInfoExec(_ context.Context, w *ws, _ BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	file, err := requiredString(call.Input, "file")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	resolved, err := w.resolvePath(file)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_info file: %w", err)
	}

	f, err := os.Open(resolved)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_info open: %w", err)
	}
	defer f.Close()

	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_info decode config: %w", err)
	}

	colorModel := "unknown"
	if cfg.ColorModel != nil {
		colorModel = colorModelName(cfg.ColorModel)
	}

	result := map[string]any{
		"file":        w.displayPath(resolved),
		"width":       cfg.Width,
		"height":      cfg.Height,
		"format":      format,
		"color_model": colorModel,
	}
	body, _ := json.MarshalIndent(result, "", "  ")
	return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(body)}, nil
}

// --- media.image_convert ---

func mediaImageConvertExec(_ context.Context, w *ws, _ BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	input, err := requiredString(call.Input, "input")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	output, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	resolvedInput, err := w.resolvePath(input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_convert input: %w", err)
	}
	resolvedOutput, err := w.resolvePath(output)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_convert output: %w", err)
	}

	img, srcFormat, err := decodeImageFile(resolvedInput)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_convert decode: %w", err)
	}

	dstFormat, err := formatFromExtension(resolvedOutput)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_convert: %w", err)
	}

	if err := encodeImageFile(resolvedOutput, img, dstFormat); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_convert encode: %w", err)
	}

	result := map[string]any{
		"input":       w.displayPath(resolvedInput),
		"output":      w.displayPath(resolvedOutput),
		"src_format":  srcFormat,
		"dest_format": dstFormat,
	}
	body, _ := json.MarshalIndent(result, "", "  ")
	return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(body)}, nil
}

// --- media.image_crop ---

func mediaImageCropExec(_ context.Context, w *ws, _ BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	input, err := requiredString(call.Input, "input")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	output, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	cropX, err := intFrom(call.Input["x"], 0)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_crop x: %w", err)
	}
	cropY, err := intFrom(call.Input["y"], 0)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_crop y: %w", err)
	}
	cropW, err := intFrom(call.Input["width"], 0)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_crop width: %w", err)
	}
	cropH, err := intFrom(call.Input["height"], 0)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_crop height: %w", err)
	}

	resolvedInput, err := w.resolvePath(input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_crop input: %w", err)
	}
	resolvedOutput, err := w.resolvePath(output)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_crop output: %w", err)
	}

	img, _, err := decodeImageFile(resolvedInput)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_crop decode: %w", err)
	}

	rect := image.Rect(cropX, cropY, cropX+cropW, cropY+cropH)

	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	si, ok := img.(subImager)
	if !ok {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_crop: image type %T does not support SubImage", img)
	}
	cropped := si.SubImage(rect)

	dstFormat, err := formatFromExtension(resolvedOutput)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_crop: %w", err)
	}

	if err := encodeImageFile(resolvedOutput, cropped, dstFormat); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_crop encode: %w", err)
	}

	result := map[string]any{
		"input":  w.displayPath(resolvedInput),
		"output": w.displayPath(resolvedOutput),
		"x":      cropX,
		"y":      cropY,
		"width":  cropped.Bounds().Dx(),
		"height": cropped.Bounds().Dy(),
	}
	body, _ := json.MarshalIndent(result, "", "  ")
	return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(body)}, nil
}

// --- media.image_resize ---

func mediaImageResizeExec(_ context.Context, w *ws, _ BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	input, err := requiredString(call.Input, "input")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	output, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, err
	}

	resolvedInput, err := w.resolvePath(input)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_resize input: %w", err)
	}
	resolvedOutput, err := w.resolvePath(output)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_resize output: %w", err)
	}

	img, _, err := decodeImageFile(resolvedInput)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_resize decode: %w", err)
	}

	srcBounds := img.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	targetW, err := intFrom(call.Input["width"], 0)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_resize width: %w", err)
	}
	targetH, err := intFrom(call.Input["height"], 0)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_resize height: %w", err)
	}

	// If neither is specified, keep original size.
	if targetW <= 0 && targetH <= 0 {
		targetW = srcW
		targetH = srcH
	} else if targetW <= 0 {
		// Maintain aspect ratio based on height.
		targetW = srcW * targetH / srcH
		if targetW < 1 {
			targetW = 1
		}
	} else if targetH <= 0 {
		// Maintain aspect ratio based on width.
		targetH = srcH * targetW / srcW
		if targetH < 1 {
			targetH = 1
		}
	}

	// Nearest-neighbor resize.
	dst := image.NewNRGBA(image.Rect(0, 0, targetW, targetH))
	for y := 0; y < targetH; y++ {
		srcY := srcBounds.Min.Y + y*srcH/targetH
		for x := 0; x < targetW; x++ {
			srcX := srcBounds.Min.X + x*srcW/targetW
			dst.Set(x, y, img.At(srcX, srcY))
		}
	}

	dstFormat, err := formatFromExtension(resolvedOutput)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_resize: %w", err)
	}

	if err := encodeImageFile(resolvedOutput, dst, dstFormat); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.image_resize encode: %w", err)
	}

	result := map[string]any{
		"input":       w.displayPath(resolvedInput),
		"output":      w.displayPath(resolvedOutput),
		"src_width":   srcW,
		"src_height":  srcH,
		"dest_width":  targetW,
		"dest_height": targetH,
	}
	body, _ := json.MarshalIndent(result, "", "  ")
	return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(body)}, nil
}

// --- media.pdf_read ---

func mediaPdfReadExec(_ context.Context, w *ws, _ BuiltinsConfig, call agent.ToolCall) (contextengine.ToolResult, error) {
	path, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, err
	}
	pagesArg, _ := stringFrom(call.Input["pages"])

	resolved, err := w.resolvePath(path)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.pdf_read path: %w", err)
	}

	f, reader, err := pdflib.Open(resolved)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.pdf_read open: %w", err)
	}
	defer f.Close()

	totalPages := reader.NumPage()

	// Determine which pages to extract.
	startPage, endPage, err := parsePdfPageRange(pagesArg, totalPages)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("media.pdf_read pages: %w", err)
	}

	var textBuilder strings.Builder
	for pageNum := startPage; pageNum <= endPage; pageNum++ {
		page := reader.Page(pageNum)
		if page.V.IsNull() {
			continue
		}
		pageText, extractErr := page.GetPlainText(nil)
		if extractErr != nil {
			// Skip pages that fail to extract; note the error inline.
			textBuilder.WriteString(fmt.Sprintf("\n--- page %d: extraction error: %s ---\n", pageNum, extractErr))
			continue
		}
		if textBuilder.Len() > 0 {
			textBuilder.WriteString("\n")
		}
		textBuilder.WriteString(fmt.Sprintf("--- page %d ---\n", pageNum))
		textBuilder.WriteString(pageText)
	}

	result := map[string]any{
		"file":       w.displayPath(resolved),
		"text":       textBuilder.String(),
		"page_count": totalPages,
	}
	body, _ := json.MarshalIndent(result, "", "  ")
	return contextengine.ToolResult{ToolName: call.Name, ToolCallID: call.ID, Content: string(body)}, nil
}

// parsePdfPageRange parses a page range string like "1-3", "5", or "all".
// Returns 1-based start and end page numbers (inclusive).
func parsePdfPageRange(pagesArg string, totalPages int) (int, int, error) {
	pagesArg = strings.TrimSpace(pagesArg)
	if pagesArg == "" || strings.EqualFold(pagesArg, "all") {
		return 1, totalPages, nil
	}

	// Single page number.
	if !strings.Contains(pagesArg, "-") {
		n, err := strconv.Atoi(pagesArg)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid page number %q", pagesArg)
		}
		if n < 1 || n > totalPages {
			return 0, 0, fmt.Errorf("page %d out of range (1-%d)", n, totalPages)
		}
		return n, n, nil
	}

	// Range like "1-3".
	parts := strings.SplitN(pagesArg, "-", 2)
	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid start page %q", parts[0])
	}
	end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid end page %q", parts[1])
	}
	if start < 1 {
		start = 1
	}
	if end > totalPages {
		end = totalPages
	}
	if start > end {
		return 0, 0, fmt.Errorf("start page %d is after end page %d", start, end)
	}
	return start, end, nil
}

// --- Image helpers ---

// decodeImageFile opens a file and decodes the image using Go stdlib decoders.
func decodeImageFile(path string) (image.Image, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	img, format, err := image.Decode(f)
	if err != nil {
		return nil, "", err
	}
	return img, format, nil
}

// encodeImageFile encodes an image to the given path in the specified format.
func encodeImageFile(path string, img image.Image, format string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	switch format {
	case "png":
		return png.Encode(f, img)
	case "jpeg":
		return jpeg.Encode(f, img, &jpeg.Options{Quality: 90})
	case "gif":
		return gif.Encode(f, img, nil)
	default:
		return fmt.Errorf("unsupported output format %q (supported: png, jpeg, gif)", format)
	}
}

// formatFromExtension returns the canonical format name from a file extension.
func formatFromExtension(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".png":
		return "png", nil
	case ".jpg", ".jpeg":
		return "jpeg", nil
	case ".gif":
		return "gif", nil
	default:
		return "", fmt.Errorf("unsupported image format extension %q (supported: .png, .jpg, .jpeg, .gif)", ext)
	}
}

// colorModelName returns a human-readable name for a color model.
func colorModelName(m color.Model) string {
	switch m {
	case color.RGBAModel:
		return "RGBA"
	case color.RGBA64Model:
		return "RGBA64"
	case color.NRGBAModel:
		return "NRGBA"
	case color.NRGBA64Model:
		return "NRGBA64"
	case color.AlphaModel:
		return "Alpha"
	case color.Alpha16Model:
		return "Alpha16"
	case color.GrayModel:
		return "Gray"
	case color.Gray16Model:
		return "Gray16"
	case color.CMYKModel:
		return "CMYK"
	case color.YCbCrModel:
		return "YCbCr"
	case color.NYCbCrAModel:
		return "NYCbCrA"
	default:
		return "unknown"
	}
}

// --- Output schemas for media-go tools ---

func mediaImageInfoOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"file":        stringSchema("Image file path."),
		"width":       integerSchema("Image width in pixels."),
		"height":      integerSchema("Image height in pixels."),
		"format":      stringSchema("Image format (png, jpeg, gif)."),
		"color_model": stringSchema("Color model name."),
	}, "file", "width", "height", "format", "color_model")
}

func mediaImageWriteOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"input":  stringSchema("Input file path."),
		"output": stringSchema("Output file path."),
	}, "input", "output")
}

func mediaImageResizeSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input":  map[string]any{"type": "string", "description": "Input image file path."},
			"output": map[string]any{"type": "string", "description": "Output image file path."},
			"width":  map[string]any{"type": "integer", "description": "Target width in pixels."},
			"height": map[string]any{"type": "integer", "description": "Target height in pixels."},
		},
		"required":             []string{"input", "output"},
		"additionalProperties": false,
	}
}

func mediaImageCropSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input":  map[string]any{"type": "string", "description": "Input image file path."},
			"output": map[string]any{"type": "string", "description": "Output image file path."},
			"x":      map[string]any{"type": "integer", "description": "X offset of crop rectangle."},
			"y":      map[string]any{"type": "integer", "description": "Y offset of crop rectangle."},
			"width":  map[string]any{"type": "integer", "description": "Width of crop rectangle."},
			"height": map[string]any{"type": "integer", "description": "Height of crop rectangle."},
		},
		"required":             []string{"input", "output", "x", "y", "width", "height"},
		"additionalProperties": false,
	}
}

// Session / Memory tools moved to Layer 1 (builtin_env.go).

// ==========================================================================
// Shared stub / simple schemas
// ==========================================================================

func stubOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"status":  stringSchema("Result status."),
		"message": stringSchema("Human-readable message."),
	}, "status", "message")
}

// --- Media schemas ---

func mediaInfoSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file": map[string]any{"type": "string", "description": "Path to the media file."},
		},
		"required":             []string{"file"},
		"additionalProperties": false,
	}
}

func mediaConvertSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input":  map[string]any{"type": "string", "description": "Input file path."},
			"output": map[string]any{"type": "string", "description": "Output file path."},
		},
		"required":             []string{"input", "output"},
		"additionalProperties": false,
	}
}

func mediaThumbnailSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input":     map[string]any{"type": "string", "description": "Input video file path."},
			"output":    map[string]any{"type": "string", "description": "Output image file path."},
			"timestamp": map[string]any{"type": "string", "description": "Timestamp for the thumbnail, e.g. 00:00:01. Defaults to 00:00:01."},
		},
		"required":             []string{"input", "output"},
		"additionalProperties": false,
	}
}

func mediaClipSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input":    map[string]any{"type": "string", "description": "Input video file path."},
			"output":   map[string]any{"type": "string", "description": "Output video file path."},
			"start":    map[string]any{"type": "string", "description": "Start timestamp, e.g. 00:01:00."},
			"duration": map[string]any{"type": "string", "description": "Optional clip duration, e.g. 00:00:30."},
		},
		"required":             []string{"input", "output", "start"},
		"additionalProperties": false,
	}
}

func mediaAudioExtractSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input":  map[string]any{"type": "string", "description": "Input video file path."},
			"output": map[string]any{"type": "string", "description": "Output audio file path."},
			"format": map[string]any{"type": "string", "description": "Audio format: mp3, wav, aac, ogg, or flac. Defaults to mp3."},
		},
		"required":             []string{"input", "output"},
		"additionalProperties": false,
	}
}

func mediaConcatSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"inputs": map[string]any{
				"type":        "array",
				"description": "Array of input media file paths to concatenate.",
				"items":       map[string]any{"type": "string"},
			},
			"output": map[string]any{"type": "string", "description": "Output file path."},
		},
		"required":             []string{"inputs", "output"},
		"additionalProperties": false,
	}
}

func mediaSubtitleExtractSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input":        map[string]any{"type": "string", "description": "Input video file path."},
			"output":       map[string]any{"type": "string", "description": "Output subtitle file path (e.g. .srt, .ass)."},
			"stream_index": map[string]any{"type": "integer", "description": "Subtitle stream index. Defaults to 0."},
		},
		"required":             []string{"input", "output"},
		"additionalProperties": false,
	}
}

func mediaMetadataSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{"type": "string", "description": "Path to the media file."},
		},
		"required":             []string{"input"},
		"additionalProperties": false,
	}
}

func mediaMetadataOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"file":      stringSchema("Media file path."),
		"metadata":  map[string]any{"description": "Parsed ffprobe metadata output."},
		"exit_code": integerSchema("Process exit code."),
	}, "file", "metadata", "exit_code")
}

func mediaPdfReadSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":  map[string]any{"type": "string", "description": "Path to the PDF file."},
			"pages": map[string]any{"type": "string", "description": "Page range to extract, e.g. '1-3', '5', or 'all'. Defaults to all."},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func mediaPdfReadOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"file":       stringSchema("PDF file path."),
		"text":       stringSchema("Extracted text content."),
		"page_count": integerSchema("Total number of pages in the PDF."),
	}, "file", "text", "page_count")
}

func mediaInfoOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"file":      stringSchema("Media file path."),
		"info":      map[string]any{"description": "Parsed ffprobe output."},
		"exit_code": integerSchema("Process exit code."),
	}, "file", "info", "exit_code")
}

func mediaCmdOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"stdout":    stringSchema("Standard output."),
		"stderr":    stringSchema("Standard error."),
		"exit_code": integerSchema("Process exit code."),
	}, "stdout", "stderr", "exit_code")
}

// --- Search schemas ---

func searchQuerySchema() map[string]any {
	return objectSchema(map[string]any{
		"query":           stringSchema("Search query."),
		"count":           integerSchema("Maximum number of results to return (default: 8, max: 10)."),
		"freshness":       stringSchema("Optional time filter: day, week, month, or year."),
		"date_after":      stringSchema("Only results published after this date (YYYY-MM-DD)."),
		"date_before":     stringSchema("Only results published before this date (YYYY-MM-DD)."),
		"language":        stringSchema("Optional result language code, for example en or zh."),
		"region":          stringSchema("Optional region or market code, for example US or zh-CN."),
		"domains":         stringArraySchema("Optional allowlist of domains to prefer."),
		"exclude_domains": stringArraySchema("Optional blocklist of domains to exclude."),
	}, "query")
}

func searchResultSchema() map[string]any {
	return objectSchema(map[string]any{
		"title":        stringSchema("Result title."),
		"url":          stringSchema("Result URL."),
		"snippet":      stringSchema("Short result snippet."),
		"source":       stringSchema("Publisher or source name."),
		"published_at": stringSchema("Published timestamp or date when available."),
		"domain":       stringSchema("Normalized source domain."),
	}, "title", "url")
}

func searchOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"provider":       stringSchema("Configured search provider."),
		"kind":           stringSchema("Search kind: web or news."),
		"query":          stringSchema("Original query."),
		"executed_query": stringSchema("Expanded query sent to the provider."),
		"status_code":    integerSchema("HTTP status code returned by the provider."),
		"result_count":   integerSchema("Number of normalized results."),
		"results": map[string]any{
			"type":        "array",
			"description": "Normalized search results.",
			"items":       searchResultSchema(),
		},
	}, "provider", "kind", "query", "executed_query", "status_code", "result_count", "results")
}

func newsDigestSchema() map[string]any {
	properties := searchQuerySchema()["properties"].(map[string]any)
	out := make(map[string]any, len(properties)+1)
	for k, v := range properties {
		out[k] = v
	}
	out["fetch_top_n"] = integerSchema("How many top results to fetch for fuller summaries (default: 3, max: 5).")
	return objectSchema(out, "query")
}

func newsDigestOutputSchema() map[string]any {
	item := objectSchema(map[string]any{
		"rank":         integerSchema("1-based rank in the digest."),
		"title":        stringSchema("Article title."),
		"source":       stringSchema("Publisher or source name."),
		"published_at": stringSchema("Published timestamp or date when available."),
		"url":          stringSchema("Article URL."),
		"domain":       stringSchema("Normalized source domain."),
		"snippet":      stringSchema("Search snippet from the provider."),
		"summary":      stringSchema("Short summary built from fetched content or snippet."),
	}, "rank", "title", "url")
	return objectSchema(map[string]any{
		"provider":       stringSchema("Configured search provider."),
		"query":          stringSchema("Original query."),
		"executed_query": stringSchema("Expanded query sent to the provider."),
		"status_code":    integerSchema("HTTP status code returned by the search provider."),
		"result_count":   integerSchema("Number of digest rows."),
		"fetched_count":  integerSchema("How many top links were fetched for richer summaries."),
		"items": map[string]any{
			"type":        "array",
			"description": "Normalized digest rows.",
			"items":       item,
		},
		"markdown_table": stringSchema("Markdown table ready to paste into the final answer."),
		"csv":            stringSchema("CSV representation of the same digest rows."),
	}, "provider", "query", "executed_query", "status_code", "result_count", "fetched_count", "items", "markdown_table", "csv")
}

// --- Speech schemas ---

func speechTTSSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text":   map[string]any{"type": "string", "description": "Text to convert to speech."},
			"output": map[string]any{"type": "string", "description": "Output audio file path."},
		},
		"required":             []string{"text", "output"},
		"additionalProperties": false,
	}
}

func speechSTTSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"input": map[string]any{"type": "string", "description": "Input audio file path."},
		},
		"required":             []string{"input"},
		"additionalProperties": false,
	}
}

// --- Email schemas ---

func emailSendSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"to":      map[string]any{"type": "string", "description": "Recipient email address."},
			"subject": map[string]any{"type": "string", "description": "Email subject."},
			"body":    map[string]any{"type": "string", "description": "Email body text."},
			"attachments": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Array of file paths to attach to the email.",
			},
			"html": map[string]any{"type": "boolean", "description": "Send body as HTML instead of plain text. Defaults to false."},
		},
		"required":             []string{"to", "subject", "body"},
		"additionalProperties": false,
	}
}

func emailIDSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":     map[string]any{"type": "string", "description": "Email UID."},
			"folder": map[string]any{"type": "string", "description": "Email folder. Defaults to inbox."},
		},
		"required":             []string{"id"},
		"additionalProperties": false,
	}
}

func emailListSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"folder": map[string]any{"type": "string", "description": "Email folder to list. Defaults to inbox."},
			"limit":  map[string]any{"type": "integer", "description": "Maximum number of emails to return."},
		},
		"additionalProperties": false,
	}
}

func emailReadSchema() map[string]any {
	return emailIDSchema()
}

func emailSearchSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":  map[string]any{"type": "string", "description": "Search query matched against email text."},
			"folder": map[string]any{"type": "string", "description": "Email folder to search. Defaults to inbox."},
			"limit":  map[string]any{"type": "integer", "description": "Maximum number of emails to return."},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
}

func emailDownloadAttachmentSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":               map[string]any{"type": "string", "description": "Email UID."},
			"folder":           map[string]any{"type": "string", "description": "Email folder. Defaults to inbox."},
			"attachment_index": map[string]any{"type": "integer", "description": "0-based index of the attachment to download."},
			"output":           map[string]any{"type": "string", "description": "Output file path for the downloaded attachment."},
		},
		"required":             []string{"id", "attachment_index", "output"},
		"additionalProperties": false,
	}
}

// Session / Memory schemas removed — now in builtin_env.go.

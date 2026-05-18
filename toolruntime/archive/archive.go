// Package archive implements archive tool handlers (archive.zip, archive.unzip,
// archive.tar, archive.untar, archive.list) for the toolruntime registry.
package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
)

// Runtime is the narrow interface that archive handlers need from *Builtins.
type Runtime interface {
	JSONResult(call agent.ToolCall, payload map[string]any) (contextengine.ToolResult, error)
	ResolvePath(input string) (string, error)
	DisplayPath(absPath string) string
}

// Handler is the tool handler signature for archive tools.
type Handler func(ctx context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error)

// ToolDef pairs a tool manifest with an archive handler.
type ToolDef struct {
	Manifest skill.ToolManifest
	Handler  Handler
}

// ToolDefs returns all archive domain tool definitions.
func ToolDefs() []ToolDef {
	return []ToolDef{
		{
			Manifest: skill.ToolManifest{
				Name:             "archive.zip",
				Description:      "Create a ZIP archive from one or more files or directories.",
				InputSchema:      zipInputSchema(),
				OutputSchema:     zipOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "archive:{output}",
			},
			Handler: handleZip,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "archive.unzip",
				Description:      "Extract a ZIP archive to a directory.",
				InputSchema:      unzipInputSchema(),
				OutputSchema:     unzipOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "archive:{path}",
			},
			Handler: handleUnzip,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "archive.tar",
				Description:      "Create a tar or tar.gz archive from one or more files or directories.",
				InputSchema:      tarInputSchema(),
				OutputSchema:     tarOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "archive:{output}",
			},
			Handler: handleTar,
		},
		{
			Manifest: skill.ToolManifest{
				Name:             "archive.untar",
				Description:      "Extract a tar or tar.gz archive to a directory.",
				InputSchema:      untarInputSchema(),
				OutputSchema:     untarOutputSchema(),
				SideEffectClass:  "local_write",
				RequiresApproval: true,
				ExecutionKey:     "archive:{path}",
			},
			Handler: handleUntar,
		},
		{
			Manifest: skill.ToolManifest{
				Name:            "archive.list",
				Description:     "List the contents of a ZIP, tar, or tar.gz archive.",
				InputSchema:     listInputSchema(),
				OutputSchema:    listOutputSchema(),
				SideEffectClass: "read",
				Idempotent:      true,
				ExecutionKey:    "archive:{path}",
			},
			Handler: handleList,
		},
	}
}

// ---------------------------------------------------------------------------
// Input schemas
// ---------------------------------------------------------------------------

func zipInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"output": map[string]any{
				"type":        "string",
				"description": "Output path for the ZIP archive.",
			},
			"paths": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Files and directories to include in the archive.",
			},
		},
		"required":             []string{"output", "paths"},
		"additionalProperties": false,
	}
}

func unzipInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the ZIP archive.",
			},
			"output": map[string]any{
				"type":        "string",
				"description": "Directory to extract into. Defaults to the archive's parent directory.",
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func tarInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"output": map[string]any{
				"type":        "string",
				"description": "Output path for the tar archive.",
			},
			"paths": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Files and directories to include in the archive.",
			},
			"compress": map[string]any{
				"type":        "boolean",
				"description": "Apply gzip compression. Defaults to true.",
			},
		},
		"required":             []string{"output", "paths"},
		"additionalProperties": false,
	}
}

func untarInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the tar or tar.gz archive.",
			},
			"output": map[string]any{
				"type":        "string",
				"description": "Directory to extract into. Defaults to the archive's parent directory.",
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

func listInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the archive (ZIP, tar, or tar.gz).",
			},
		},
		"required":             []string{"path"},
		"additionalProperties": false,
	}
}

// ---------------------------------------------------------------------------
// Output schemas
// ---------------------------------------------------------------------------

func stringSchema(description string) map[string]any {
	schema := map[string]any{"type": "string"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func integerSchema(description string) map[string]any {
	schema := map[string]any{"type": "integer"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func booleanSchema(description string) map[string]any {
	schema := map[string]any{"type": "boolean"}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func arraySchema(items map[string]any, description string) map[string]any {
	schema := map[string]any{
		"type":  "array",
		"items": items,
	}
	if description != "" {
		schema["description"] = description
	}
	return schema
}

func zipOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":        stringSchema("Path to the created archive."),
		"file_count":  integerSchema("Number of files added."),
		"total_bytes": integerSchema("Total uncompressed bytes."),
	}, "path", "file_count", "total_bytes")
}

func unzipOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":        stringSchema("Path to the archive that was extracted."),
		"output_dir":  stringSchema("Directory files were extracted into."),
		"file_count":  integerSchema("Number of files extracted."),
		"total_bytes": integerSchema("Total bytes extracted."),
	}, "path", "output_dir", "file_count", "total_bytes")
}

func tarOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":        stringSchema("Path to the created archive."),
		"file_count":  integerSchema("Number of files added."),
		"total_bytes": integerSchema("Total uncompressed bytes."),
		"compressed":  booleanSchema("Whether gzip compression was applied."),
	}, "path", "file_count", "total_bytes", "compressed")
}

func untarOutputSchema() map[string]any {
	return objectSchema(map[string]any{
		"path":        stringSchema("Path to the archive that was extracted."),
		"output_dir":  stringSchema("Directory files were extracted into."),
		"file_count":  integerSchema("Number of files extracted."),
		"total_bytes": integerSchema("Total bytes extracted."),
	}, "path", "output_dir", "file_count", "total_bytes")
}

func listOutputSchema() map[string]any {
	entry := objectSchema(map[string]any{
		"name":     stringSchema("Entry name or path within the archive."),
		"size":     integerSchema("Uncompressed size in bytes."),
		"is_dir":   booleanSchema("Whether the entry is a directory."),
		"modified": stringSchema("Modification time in RFC3339 format."),
	}, "name", "size", "is_dir", "modified")
	return objectSchema(map[string]any{
		"path":    stringSchema("Path to the archive."),
		"format":  stringSchema("Detected format: zip, tar, or tar.gz."),
		"entries": arraySchema(entry, "Archive entries."),
		"count":   integerSchema("Number of entries."),
	}, "path", "format", "entries", "count")
}

// ---------------------------------------------------------------------------
// Param helpers — duplicated locally to avoid importing toolruntime.
// ---------------------------------------------------------------------------

func stringFrom(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	default:
		return "", fmt.Errorf("expected string, got %T", value)
	}
}

func requiredString(input map[string]any, key string) (string, error) {
	value, err := stringFrom(input[key])
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func stringSliceFrom(value any) ([]string, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text, err := stringFrom(item)
			if err != nil {
				return nil, err
			}
			out = append(out, text)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected array, got %T", value)
	}
}

func boolFromDefault(value any, fallback bool) (bool, error) {
	if value == nil {
		return fallback, nil
	}
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case string:
		parsed, err := strconv.ParseBool(typed)
		if err != nil {
			return false, err
		}
		return parsed, nil
	default:
		return false, fmt.Errorf("expected boolean, got %T", value)
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleZip(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	outputPath, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.zip: %w", err)
	}
	paths, err := stringSliceFrom(call.Input["paths"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.zip: %w", err)
	}
	if len(paths) == 0 {
		return contextengine.ToolResult{}, fmt.Errorf("archive.zip: paths is required")
	}

	resolvedOutput, err := rt.ResolvePath(outputPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.zip: output: %w", err)
	}

	f, err := os.Create(resolvedOutput)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.zip: %w", err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	var fileCount int
	var totalBytes int64

	for _, p := range paths {
		resolved, err := rt.ResolvePath(p)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("archive.zip: %w", err)
		}
		baseDir := filepath.Dir(resolved)
		err = filepath.Walk(resolved, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(baseDir, path)
			if err != nil {
				return err
			}
			header, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			header.Name = filepath.ToSlash(rel)
			if info.IsDir() {
				header.Name += "/"
			} else {
				header.Method = zip.Deflate
			}
			writer, err := w.CreateHeader(header)
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			src, err := os.Open(path)
			if err != nil {
				return err
			}
			defer src.Close()
			n, err := io.Copy(writer, src)
			if err != nil {
				return err
			}
			totalBytes += n
			fileCount++
			return nil
		})
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("archive.zip: %w", err)
		}
	}

	if err := w.Close(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.zip: %w", err)
	}

	return rt.JSONResult(call, map[string]any{
		"path":        rt.DisplayPath(resolvedOutput),
		"file_count":  fileCount,
		"total_bytes": totalBytes,
	})
}

func handleUnzip(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	archivePath, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.unzip: %w", err)
	}
	outputDir, _ := stringFrom(call.Input["output"])

	resolvedArchive, err := rt.ResolvePath(archivePath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.unzip: %w", err)
	}

	resolvedOutput := filepath.Dir(resolvedArchive)
	if strings.TrimSpace(outputDir) != "" {
		resolvedOutput, err = rt.ResolvePath(outputDir)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("archive.unzip: output: %w", err)
		}
	}

	r, err := zip.OpenReader(resolvedArchive)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.unzip: %w", err)
	}
	defer r.Close()

	var fileCount int
	var totalBytes int64

	for _, zf := range r.File {
		destPath := filepath.Join(resolvedOutput, zf.Name)
		// Guard against zip slip.
		if !strings.HasPrefix(filepath.Clean(destPath)+string(os.PathSeparator), filepath.Clean(resolvedOutput)+string(os.PathSeparator)) &&
			filepath.Clean(destPath) != filepath.Clean(resolvedOutput) {
			return contextengine.ToolResult{}, fmt.Errorf("archive.unzip: illegal path in archive: %s", zf.Name)
		}

		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(destPath, 0755); err != nil {
				return contextengine.ToolResult{}, fmt.Errorf("archive.unzip: %w", err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("archive.unzip: %w", err)
		}

		src, err := zf.Open()
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("archive.unzip: %w", err)
		}

		dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, zf.Mode())
		if err != nil {
			src.Close()
			return contextengine.ToolResult{}, fmt.Errorf("archive.unzip: %w", err)
		}

		n, err := io.Copy(dst, src)
		src.Close()
		dst.Close()
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("archive.unzip: %w", err)
		}
		totalBytes += n
		fileCount++
	}

	return rt.JSONResult(call, map[string]any{
		"path":        rt.DisplayPath(resolvedArchive),
		"output_dir":  rt.DisplayPath(resolvedOutput),
		"file_count":  fileCount,
		"total_bytes": totalBytes,
	})
}

func handleTar(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	outputPath, err := requiredString(call.Input, "output")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.tar: %w", err)
	}
	paths, err := stringSliceFrom(call.Input["paths"])
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.tar: %w", err)
	}
	if len(paths) == 0 {
		return contextengine.ToolResult{}, fmt.Errorf("archive.tar: paths is required")
	}
	compress, err := boolFromDefault(call.Input["compress"], true)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.tar: %w", err)
	}

	resolvedOutput, err := rt.ResolvePath(outputPath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.tar: output: %w", err)
	}

	f, err := os.Create(resolvedOutput)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.tar: %w", err)
	}
	defer f.Close()

	var tw *tar.Writer
	var gw *gzip.Writer
	if compress {
		gw = gzip.NewWriter(f)
		defer gw.Close()
		tw = tar.NewWriter(gw)
	} else {
		tw = tar.NewWriter(f)
	}
	defer tw.Close()

	var fileCount int
	var totalBytes int64

	for _, p := range paths {
		resolved, err := rt.ResolvePath(p)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("archive.tar: %w", err)
		}
		baseDir := filepath.Dir(resolved)
		err = filepath.Walk(resolved, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(baseDir, path)
			if err != nil {
				return err
			}
			header.Name = filepath.ToSlash(rel)
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			src, err := os.Open(path)
			if err != nil {
				return err
			}
			defer src.Close()
			n, err := io.Copy(tw, src)
			if err != nil {
				return err
			}
			totalBytes += n
			fileCount++
			return nil
		})
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("archive.tar: %w", err)
		}
	}

	if err := tw.Close(); err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.tar: %w", err)
	}
	if gw != nil {
		if err := gw.Close(); err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("archive.tar: %w", err)
		}
	}

	return rt.JSONResult(call, map[string]any{
		"path":        rt.DisplayPath(resolvedOutput),
		"file_count":  fileCount,
		"total_bytes": totalBytes,
		"compressed":  compress,
	})
}

func handleUntar(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	archivePath, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.untar: %w", err)
	}
	outputDir, _ := stringFrom(call.Input["output"])

	resolvedArchive, err := rt.ResolvePath(archivePath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.untar: %w", err)
	}

	resolvedOutput := filepath.Dir(resolvedArchive)
	if strings.TrimSpace(outputDir) != "" {
		resolvedOutput, err = rt.ResolvePath(outputDir)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("archive.untar: output: %w", err)
		}
	}

	f, err := os.Open(resolvedArchive)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.untar: %w", err)
	}
	defer f.Close()

	var reader io.Reader = f
	// Try gzip decompression; fall back to plain tar on failure.
	gr, gzErr := gzip.NewReader(f)
	if gzErr == nil {
		defer gr.Close()
		reader = gr
	} else {
		// Not gzip; reset file to beginning for plain tar.
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("archive.untar: %w", err)
		}
	}

	tr := tar.NewReader(reader)
	var fileCount int
	var totalBytes int64

	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("archive.untar: %w", err)
		}

		destPath := filepath.Join(resolvedOutput, header.Name)
		// Guard against tar slip.
		if !strings.HasPrefix(filepath.Clean(destPath)+string(os.PathSeparator), filepath.Clean(resolvedOutput)+string(os.PathSeparator)) &&
			filepath.Clean(destPath) != filepath.Clean(resolvedOutput) {
			return contextengine.ToolResult{}, fmt.Errorf("archive.untar: illegal path in archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, os.FileMode(header.Mode)); err != nil {
				return contextengine.ToolResult{}, fmt.Errorf("archive.untar: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
				return contextengine.ToolResult{}, fmt.Errorf("archive.untar: %w", err)
			}
			dst, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return contextengine.ToolResult{}, fmt.Errorf("archive.untar: %w", err)
			}
			n, err := io.Copy(dst, tr)
			dst.Close()
			if err != nil {
				return contextengine.ToolResult{}, fmt.Errorf("archive.untar: %w", err)
			}
			totalBytes += n
			fileCount++
		}
	}

	return rt.JSONResult(call, map[string]any{
		"path":        rt.DisplayPath(resolvedArchive),
		"output_dir":  rt.DisplayPath(resolvedOutput),
		"file_count":  fileCount,
		"total_bytes": totalBytes,
	})
}

func handleList(_ context.Context, rt Runtime, call agent.ToolCall) (contextengine.ToolResult, error) {
	archivePath, err := requiredString(call.Input, "path")
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.list: %w", err)
	}

	resolvedArchive, err := rt.ResolvePath(archivePath)
	if err != nil {
		return contextengine.ToolResult{}, fmt.Errorf("archive.list: %w", err)
	}

	// Try ZIP first.
	entries, format, err := listZip(resolvedArchive)
	if err != nil {
		// Fall back to tar / tar.gz.
		entries, format, err = listTar(resolvedArchive)
		if err != nil {
			return contextengine.ToolResult{}, fmt.Errorf("archive.list: unable to read archive: %w", err)
		}
	}

	return rt.JSONResult(call, map[string]any{
		"path":    rt.DisplayPath(resolvedArchive),
		"format":  format,
		"entries": entries,
		"count":   len(entries),
	})
}

func listZip(path string) ([]map[string]any, string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, "", err
	}
	defer r.Close()

	entries := make([]map[string]any, 0, len(r.File))
	for _, zf := range r.File {
		entries = append(entries, map[string]any{
			"name":     zf.Name,
			"size":     int64(zf.UncompressedSize64),
			"is_dir":   zf.FileInfo().IsDir(),
			"modified": zf.Modified.Format(time.RFC3339),
		})
	}
	return entries, "zip", nil
}

func listTar(path string) ([]map[string]any, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()

	format := "tar"
	var reader io.Reader = f
	gr, gzErr := gzip.NewReader(f)
	if gzErr == nil {
		defer gr.Close()
		reader = gr
		format = "tar.gz"
	} else {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, "", err
		}
	}

	tr := tar.NewReader(reader)
	var entries []map[string]any
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", err
		}
		entries = append(entries, map[string]any{
			"name":     header.Name,
			"size":     header.Size,
			"is_dir":   header.Typeflag == tar.TypeDir,
			"modified": header.ModTime.Format(time.RFC3339),
		})
	}
	return entries, format, nil
}

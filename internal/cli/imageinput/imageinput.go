package imageinput

import (
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var supportedExtensions = map[string]struct{}{
	".gif":  {},
	".jpeg": {},
	".jpg":  {},
	".png":  {},
	".webp": {},
}

// EncodeFileAsDataURI reads an image file and returns a base64 data URI.
func EncodeFileAsDataURI(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("image path is required")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read image %q: %w", path, err)
	}

	mediaType := strings.TrimSpace(mime.TypeByExtension(strings.ToLower(filepath.Ext(path))))
	if !strings.HasPrefix(mediaType, "image/") {
		mediaType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(mediaType, "image/") {
		return "", fmt.Errorf("%q is not a supported image file", path)
	}

	return "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// ExtractImagePaths removes image file paths from free-form input and returns
// the cleaned text plus data URIs for the files that were found.
func ExtractImagePaths(input string) (string, []string) {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return "", nil
	}

	remaining := make([]string, 0, len(fields))
	images := make([]string, 0, len(fields))
	for _, field := range fields {
		candidate := strings.Trim(field, "\"'(),")
		if !looksLikeImagePath(candidate) {
			remaining = append(remaining, field)
			continue
		}
		dataURI, err := EncodeFileAsDataURI(candidate)
		if err != nil {
			remaining = append(remaining, field)
			continue
		}
		images = append(images, dataURI)
	}

	return strings.Join(remaining, " "), images
}

func looksLikeImagePath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if _, ok := supportedExtensions[strings.ToLower(filepath.Ext(path))]; !ok {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

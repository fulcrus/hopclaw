package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/logging"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// maxFileSize is the maximum individual file size to include in a backup
	// (10 MB). Files exceeding this are silently skipped.
	maxFileSize = 10 * 1024 * 1024

	// backupDirName is the subdirectory under stateDir where archives are kept.
	backupDirName = "backups"

	// backupPrefix is the filename prefix for generated archives.
	backupPrefix = "backup-"

	// backupTimeFormat is the timestamp format used in archive filenames.
	backupTimeFormat = "20060102-150405"

	// backupExtension is the file extension for backup archives.
	backupExtension = ".tar.gz"

	// manifestFileName is the name of the manifest file inside the archive.
	manifestFileName = "manifest.json"

	// backupDirMode is the permission mode for the backup directory.
	backupDirMode = 0o755

	// backupFileMode is the permission mode for created backup archives.
	backupFileMode = 0o600

	// restoredFileMode is the default permission mode for restored files.
	restoredFileMode = 0o644

	// restoredDirMode is the default permission mode for restored directories.
	restoredDirMode = 0o755

	// bakSuffix is appended to existing files before they are overwritten by restore.
	bakSuffix = ".bak"
)

// skipDirs lists directory names that are never included in a backup.
var skipDirs = map[string]bool{
	"backups": true,
	"logs":    true,
	"tmp":     true,
}

// skipExtensions lists file extensions that are never included in a backup.
var skipExtensions = map[string]bool{
	".pid": true,
	".tmp": true,
	".log": true,
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

// Service provides backup creation, listing, and restoration.
type Service struct {
	stateDir  string
	backupDir string
}

// NewService returns a Service that manages backups under stateDir/backups.
func NewService(stateDir string) *Service {
	return &Service{
		stateDir:  stateDir,
		backupDir: filepath.Join(stateDir, backupDirName),
	}
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

// Create snapshots the HopClaw state directory into a timestamped tar.gz
// archive. It includes config files, cron job data, event JSONL files, and
// skills data while skipping logs, temp files, PID files, and binary files
// that exceed maxFileSize.
func (s *Service) Create(ctx context.Context) (*BackupResult, error) {
	if err := os.MkdirAll(s.backupDir, backupDirMode); err != nil {
		return nil, fmt.Errorf("backup: create dir %s: %w", s.backupDir, err)
	}

	now := time.Now()
	archiveName := backupPrefix + now.Format(backupTimeFormat) + backupExtension
	archivePath := filepath.Join(s.backupDir, archiveName)

	entries, err := s.collectFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("backup: collect files: %w", err)
	}

	hostname, _ := os.Hostname()
	manifest := Manifest{
		Version:   manifestVersion,
		CreatedAt: now,
		Hostname:  hostname,
		Files:     entries,
	}

	archiveSize, err := s.writeArchive(archivePath, manifest)
	if err != nil {
		// Clean up partial archive on failure.
		logging.DebugIfErr(os.Remove(archivePath), "remove archive file failed")
		return nil, fmt.Errorf("backup: write archive: %w", err)
	}

	return &BackupResult{
		Path:      archivePath,
		Size:      archiveSize,
		FileCount: len(entries),
		CreatedAt: now,
	}, nil
}

// collectFiles walks the state directory and returns entries eligible for
// backup. The context is checked between files so that cancellation is
// respected during long walks.
func (s *Service) collectFiles(ctx context.Context) ([]FileEntry, error) {
	var entries []FileEntry

	err := filepath.Walk(s.stateDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // skip inaccessible paths
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Skip excluded directories entirely.
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip by extension.
		if skipExtensions[filepath.Ext(path)] {
			return nil
		}

		// Skip files that exceed the size limit.
		if info.Size() > maxFileSize {
			return nil
		}

		relPath, err := filepath.Rel(s.stateDir, path)
		if err != nil {
			return nil // skip if we cannot compute the relative path
		}

		entries = append(entries, FileEntry{
			Path:     relPath,
			OrigPath: path,
			Size:     info.Size(),
			ModTime:  info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// writeArchive creates the tar.gz archive at archivePath containing the
// listed files plus the embedded manifest. It returns the total archive size.
func (s *Service) writeArchive(archivePath string, manifest Manifest) (int64, error) {
	f, err := os.OpenFile(archivePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, backupFileMode)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	// Write manifest first.
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("marshal manifest: %w", err)
	}
	if err := tw.WriteHeader(&tar.Header{
		Name:    manifestFileName,
		Size:    int64(len(manifestData)),
		Mode:    restoredFileMode,
		ModTime: manifest.CreatedAt,
	}); err != nil {
		return 0, err
	}
	if _, err := tw.Write(manifestData); err != nil {
		return 0, err
	}

	// Write each collected file.
	for _, entry := range manifest.Files {
		if err := s.addFileToArchive(tw, entry); err != nil {
			return 0, fmt.Errorf("add %s: %w", entry.Path, err)
		}
	}

	// Flush writers so the file size is accurate.
	if err := tw.Close(); err != nil {
		return 0, err
	}
	if err := gw.Close(); err != nil {
		return 0, err
	}

	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// addFileToArchive reads a single file from disk and writes it into the tar
// archive at the entry's relative path.
func (s *Service) addFileToArchive(tw *tar.Writer, entry FileEntry) error {
	file, err := os.Open(entry.OrigPath)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	header := &tar.Header{
		Name:    entry.Path,
		Size:    info.Size(),
		Mode:    int64(info.Mode()),
		ModTime: info.ModTime(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	if _, err := io.Copy(tw, file); err != nil {
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

// List returns all backup archives found in the backup directory, sorted by
// creation time descending (newest first).
func (s *Service) List() ([]BackupResult, error) {
	dirEntries, err := os.ReadDir(s.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("backup: read dir %s: %w", s.backupDir, err)
	}

	var results []BackupResult
	for _, de := range dirEntries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if !strings.HasPrefix(name, backupPrefix) || !strings.HasSuffix(name, backupExtension) {
			continue
		}

		info, err := de.Info()
		if err != nil {
			continue
		}

		createdAt := parseBackupTime(name)
		fileCount := countArchiveFiles(filepath.Join(s.backupDir, name))

		results = append(results, BackupResult{
			Path:      filepath.Join(s.backupDir, name),
			Size:      info.Size(),
			FileCount: fileCount,
			CreatedAt: createdAt,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})
	return results, nil
}

// parseBackupTime extracts the timestamp from a backup filename. Returns the
// zero time when parsing fails.
func parseBackupTime(name string) time.Time {
	name = strings.TrimPrefix(name, backupPrefix)
	name = strings.TrimSuffix(name, backupExtension)
	t, _ := time.Parse(backupTimeFormat, name)
	return t
}

// countArchiveFiles reads the manifest from a tar.gz and returns the file
// count. Returns -1 when the manifest cannot be read.
func countArchiveFiles(archivePath string) int {
	manifest, err := readManifestFromArchive(archivePath)
	if err != nil {
		return -1
	}
	return len(manifest.Files)
}

// ---------------------------------------------------------------------------
// Restore
// ---------------------------------------------------------------------------

// Restore extracts files from the backup archive at backupPath back to their
// original locations. Existing files are renamed with a .bak suffix before
// being overwritten. The manifest is read first to discover which files the
// archive contains.
func (s *Service) Restore(ctx context.Context, backupPath string) (*RestoreResult, error) {
	manifest, err := readManifestFromArchive(backupPath)
	if err != nil {
		return nil, fmt.Errorf("backup: read manifest: %w", err)
	}

	// Build a lookup of relative-path -> original-path from the manifest.
	origPaths := make(map[string]string, len(manifest.Files))
	for _, fe := range manifest.Files {
		origPaths[fe.Path] = fe.OrigPath
	}

	restored, err := s.extractArchive(ctx, backupPath, origPaths)
	if err != nil {
		return nil, fmt.Errorf("backup: extract: %w", err)
	}

	return &RestoreResult{
		FilesRestored: restored,
		BackupPath:    backupPath,
	}, nil
}

// extractArchive reads through the tar.gz at archivePath and writes each file
// (except the manifest itself) to its original location. Returns the number
// of files restored.
func (s *Service) extractArchive(ctx context.Context, archivePath string, origPaths map[string]string) (int, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return 0, err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	restored := 0

	for {
		select {
		case <-ctx.Done():
			return restored, ctx.Err()
		default:
		}

		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return restored, err
		}

		// Skip the manifest file itself.
		if header.Name == manifestFileName {
			continue
		}

		destPath, ok := origPaths[header.Name]
		if !ok {
			// Fall back to restoring under the state directory.
			destPath = filepath.Join(s.stateDir, header.Name)
		}

		if err := restoreFile(tr, destPath, header); err != nil {
			return restored, fmt.Errorf("restore %s: %w", header.Name, err)
		}
		restored++
	}

	return restored, nil
}

// restoreFile writes the contents of r to destPath, backing up any existing
// file first.
func restoreFile(r io.Reader, destPath string, header *tar.Header) error {
	// Ensure the parent directory exists.
	if err := os.MkdirAll(filepath.Dir(destPath), restoredDirMode); err != nil {
		return err
	}

	// Back up existing file.
	if _, err := os.Stat(destPath); err == nil {
		if err := os.Rename(destPath, destPath+bakSuffix); err != nil {
			return fmt.Errorf("create backup of %s: %w", destPath, err)
		}
	}

	mode := os.FileMode(header.Mode)
	if mode == 0 {
		mode = restoredFileMode
	}

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, r); err != nil {
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// readManifestFromArchive opens a backup archive and reads its embedded
// manifest.json.
func readManifestFromArchive(archivePath string) (*Manifest, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("manifest.json not found in archive")
		}
		if err != nil {
			return nil, err
		}
		if header.Name == manifestFileName {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			var m Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				return nil, fmt.Errorf("parse manifest: %w", err)
			}
			return &m, nil
		}
	}
}

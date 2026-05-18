package backup

import "time"

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// manifestVersion is the current backup manifest schema version.
	manifestVersion = 1
)

// ---------------------------------------------------------------------------
// Manifest
// ---------------------------------------------------------------------------

// Manifest is written inside every backup archive as manifest.json.
type Manifest struct {
	Version   int         `json:"version"`
	CreatedAt time.Time   `json:"created_at"`
	Hostname  string      `json:"hostname"`
	Files     []FileEntry `json:"files"`
}

// FileEntry describes a single file stored in the backup archive.
type FileEntry struct {
	Path     string    `json:"path"`      // relative path within the archive
	OrigPath string    `json:"orig_path"` // original absolute path on disk
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
}

// ---------------------------------------------------------------------------
// Results
// ---------------------------------------------------------------------------

// BackupResult is returned after a successful backup creation.
type BackupResult struct {
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	FileCount int       `json:"file_count"`
	CreatedAt time.Time `json:"created_at"`
}

// RestoreResult is returned after a successful restore operation.
type RestoreResult struct {
	FilesRestored int    `json:"files_restored"`
	BackupPath    string `json:"backup_path"`
}

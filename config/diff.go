package config

import (
	"fmt"
	"reflect"
	"strings"
)

// ChangeKind describes what happened to a config section.
type ChangeKind string

const (
	ChangeAdded   ChangeKind = "added"
	ChangeRemoved ChangeKind = "removed"
	ChangeUpdated ChangeKind = "updated"
)

// Change records a single config section that changed.
type Change struct {
	Section string     `json:"section"`
	Kind    ChangeKind `json:"kind"`
}

// ChangeSet holds all detected changes between two configs.
type ChangeSet struct {
	Changes []Change `json:"changes"`
	Fatal   bool     `json:"fatal"` // true if contains non-reloadable changes
}

// HasChanges returns true if any changes were detected.
func (cs ChangeSet) HasChanges() bool {
	return len(cs.Changes) > 0
}

// HasSection checks whether a specific section changed.
func (cs ChangeSet) HasSection(section string) bool {
	for _, c := range cs.Changes {
		if c.Section == section || strings.HasPrefix(c.Section, section+".") {
			return true
		}
	}
	return false
}

// Sections returns all changed section names.
func (cs ChangeSet) Sections() []string {
	out := make([]string, len(cs.Changes))
	for i, c := range cs.Changes {
		out[i] = c.Section
	}
	return out
}

// String returns a human-readable summary.
func (cs ChangeSet) String() string {
	if !cs.HasChanges() {
		return "no changes"
	}
	parts := make([]string, len(cs.Changes))
	for i, c := range cs.Changes {
		parts[i] = fmt.Sprintf("%s (%s)", c.Section, c.Kind)
	}
	return strings.Join(parts, ", ")
}

// nonReloadableSections lists config sections that require a restart.
var nonReloadableSections = map[string]bool{
	"server.address": true,
	"store.backend":  true,
	"store.path":     true,
}

// Diff compares two configs and returns the set of changes.
func Diff(old, new Config) ChangeSet {
	var changes []Change
	fatal := false

	// Compare top-level sections using reflection.
	sections := []struct {
		name string
		old  any
		new  any
	}{
		{"server", old.Server, new.Server},
		{"auth", old.Auth, new.Auth},
		{"store", old.Store, new.Store},
		{"agent", old.Agent, new.Agent},
		{"runtime", old.Runtime, new.Runtime},
		{"update", old.Update, new.Update},
		{"diagnostics", old.Diagnostics, new.Diagnostics},
		{"skills", old.Skills, new.Skills},
		{"models", old.Models, new.Models},
		{"tools", old.Tools, new.Tools},
		{"hosts", old.Hosts, new.Hosts},
		{"channels", old.Channels, new.Channels},
		{"plugins", old.Plugins, new.Plugins},
		{"cron", old.Cron, new.Cron},
		{"watch", old.Watch, new.Watch},
		{"heartbeat", old.Heartbeat, new.Heartbeat},
		{"wire", old.Wire, new.Wire},
		{"wakeup", old.Wakeup, new.Wakeup},
		{"allowlist", old.Allowlist, new.Allowlist},
		{"sandbox", old.Sandbox, new.Sandbox},
		{"isolation", old.Isolation, new.Isolation},
		{"tunnel", old.Tunnel, new.Tunnel},
		{"exec_approval", old.ExecApproval, new.ExecApproval},
		{"channel_health", old.ChannelHealth, new.ChannelHealth},
		{"embedding", old.Embedding, new.Embedding},
		{"security", old.Security, new.Security},
		{"discovery", old.Discovery, new.Discovery},
		{"canvas", old.Canvas, new.Canvas},
		{"usage_storage", old.UsageStorage, new.UsageStorage},
		{"memory_storage", old.MemoryStorage, new.MemoryStorage},
		{"logging", old.Logging, new.Logging},
		{"locale", old.Locale, new.Locale},
	}

	for _, s := range sections {
		if !reflect.DeepEqual(s.old, s.new) {
			changes = append(changes, Change{
				Section: s.name,
				Kind:    ChangeUpdated,
			})
		}
	}

	// Check for fatal (non-reloadable) changes.
	for _, c := range changes {
		if nonReloadableSections[c.Section] {
			fatal = true
			break
		}
		// Also check sub-sections like server.address.
		if c.Section == "server" && old.Server.Address != new.Server.Address {
			fatal = true
			break
		}
		if c.Section == "store" && (old.Store.Backend != new.Store.Backend || old.Store.Path != new.Store.Path) {
			fatal = true
			break
		}
	}

	return ChangeSet{
		Changes: changes,
		Fatal:   fatal,
	}
}

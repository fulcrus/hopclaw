package repl

import (
	"encoding/json"

	"github.com/fulcrus/hopclaw/acp"
)

type Streamer struct {
	updates     chan acp.SessionUpdateNotification
	permissions chan acp.PermissionRequest
	commands    chan []acp.Command
}

func NewStreamer(notifications <-chan acp.Notification) *Streamer {
	streamer := &Streamer{
		updates:     make(chan acp.SessionUpdateNotification, 128),
		permissions: make(chan acp.PermissionRequest, 8),
		commands:    make(chan []acp.Command, 8),
	}
	go streamer.loop(notifications)
	return streamer
}

func (s *Streamer) Updates() <-chan acp.SessionUpdateNotification {
	return s.updates
}

func (s *Streamer) Permissions() <-chan acp.PermissionRequest {
	return s.permissions
}

func (s *Streamer) Commands() <-chan []acp.Command {
	return s.commands
}

func (s *Streamer) loop(notifications <-chan acp.Notification) {
	defer close(s.updates)
	defer close(s.permissions)
	defer close(s.commands)

	for notification := range notifications {
		switch notification.Method {
		case "acp/sessionUpdate":
			var update acp.SessionUpdateNotification
			if json.Unmarshal(notification.Params, &update) == nil {
				s.updates <- update
			}
		case "acp/permissionRequest":
			var request acp.PermissionRequest
			if json.Unmarshal(notification.Params, &request) == nil {
				s.permissions <- request
			}
		case "acp/commandsUpdate":
			var payload struct {
				Commands []acp.Command `json:"commands"`
			}
			if json.Unmarshal(notification.Params, &payload) == nil {
				s.commands <- payload.Commands
			}
		}
	}
}

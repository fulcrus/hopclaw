package apiresponse

import "github.com/fulcrus/hopclaw/eventbus"

type OK struct {
	OK bool `json:"ok"`
}

type Error struct {
	Code  string `json:"code,omitempty"`
	Error string `json:"error"`
}

type CountedList struct {
	Items any `json:"items"`
	Count int `json:"count"`
}

type CursorList struct {
	Items        any                   `json:"items"`
	Count        int                   `json:"count"`
	CursorStatus eventbus.CursorStatus `json:"cursor_status"`
	NextCursor   string                `json:"next_cursor"`
}

type NodeIDOK struct {
	OK     bool   `json:"ok"`
	NodeID string `json:"node_id"`
}

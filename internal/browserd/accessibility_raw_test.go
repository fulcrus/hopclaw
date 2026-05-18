package browserd

import (
	"encoding/json"
	"testing"
)

func TestRawAXTreeHandlesUnknownPropertyNames(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"nodes": [
			{
				"nodeId": "1",
				"ignored": false,
				"ignoredReasons": [
					{"name": "uninteresting", "value": {"type": "boolean", "value": true}}
				],
				"role": {"type": "role", "value": "generic"},
				"childIds": ["2"]
			},
			{
				"nodeId": "2",
				"parentId": "1",
				"ignored": false,
				"role": {"type": "role", "value": "textbox"},
				"name": {"type": "string", "value": "Search"},
				"backendDOMNodeId": 42,
				"properties": [
					{"name": "focused", "value": {"type": "boolean", "value": true}}
				]
			}
		]
	}`)

	var response struct {
		Nodes []*rawAXNode `json:"nodes"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("Unmarshal(raw AX response) error = %v", err)
	}

	if got := response.Nodes[0].IgnoredReasons[0].Name; got != "uninteresting" {
		t.Fatalf("ignoredReasons[0].name = %q, want uninteresting", got)
	}

	root, refs := buildCompactAriaTree(response.Nodes, ariaSnapshotMaxDepth)
	if root == nil {
		t.Fatal("root = nil")
	}
	if root.Role != "textbox" || root.Name != "Search" {
		t.Fatalf("root = %#v, want textbox/Search", root)
	}
	if !root.Focused {
		t.Fatalf("root.Focused = false, want true")
	}
	if len(refs) != 1 || refs["e1"] != 42 {
		t.Fatalf("refs = %#v, want e1->42", refs)
	}
}

func TestBuildCompactAriaTreeFallsBackToParentLinks(t *testing.T) {
	t.Parallel()

	nodes := []*rawAXNode{
		{
			NodeID: "1",
			Role:   &rawAXValue{Value: json.RawMessage(`"RootWebArea"`)},
		},
		{
			NodeID:           "2",
			ParentID:         "1",
			Role:             &rawAXValue{Value: json.RawMessage(`"button"`)},
			Name:             &rawAXValue{Value: json.RawMessage(`"Submit"`)},
			BackendDOMNodeID: 99,
		},
	}

	root, refs := buildCompactAriaTree(nodes, ariaSnapshotMaxDepth)
	if root == nil {
		t.Fatal("root = nil")
	}
	if len(root.Children) != 1 {
		t.Fatalf("len(root.Children) = %d, want 1", len(root.Children))
	}
	if root.Children[0].Role != "button" || root.Children[0].Name != "Submit" {
		t.Fatalf("child = %#v, want button/Submit", root.Children[0])
	}
	if len(refs) != 1 || refs["e1"] != 99 {
		t.Fatalf("refs = %#v, want e1->99", refs)
	}
}

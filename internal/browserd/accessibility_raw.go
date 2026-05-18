package browserd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/accessibility"
	cdp "github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

type rawAXNode struct {
	NodeID           string            `json:"nodeId"`
	Ignored          bool              `json:"ignored"`
	IgnoredReasons   []rawAXProperty   `json:"ignoredReasons,omitempty"`
	Role             *rawAXValue       `json:"role,omitempty"`
	ChromeRole       *rawAXValue       `json:"chromeRole,omitempty"`
	Name             *rawAXValue       `json:"name,omitempty"`
	Description      *rawAXValue       `json:"description,omitempty"`
	Value            *rawAXValue       `json:"value,omitempty"`
	Properties       []rawAXProperty   `json:"properties,omitempty"`
	ParentID         string            `json:"parentId,omitempty"`
	ChildIDs         []string          `json:"childIds,omitempty"`
	BackendDOMNodeID cdp.BackendNodeID `json:"backendDOMNodeId,omitempty"`
	FrameID          cdp.FrameID       `json:"frameId,omitempty"`
}

type rawAXProperty struct {
	Name  string      `json:"name"`
	Value *rawAXValue `json:"value,omitempty"`
}

type rawAXValue struct {
	Type  string          `json:"type,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}

func getFullAXTreeRaw(ctx context.Context, depth int) ([]*rawAXNode, error) {
	params := accessibility.GetFullAXTree()
	if depth > 0 {
		params = params.WithDepth(int64(depth))
	}
	var response struct {
		Nodes []*rawAXNode `json:"nodes"`
	}
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		if err := accessibility.Enable().Do(ctx); err != nil {
			return err
		}
		return cdp.Execute(ctx, accessibility.CommandGetFullAXTree, params, &response)
	})); err != nil {
		return nil, err
	}
	return response.Nodes, nil
}

func rawAXValueString(v *rawAXValue) string {
	if v == nil || len(v.Value) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(v.Value, &s); err == nil {
		return s
	}
	return string(v.Value)
}

func rawAXValueBool(v *rawAXValue) bool {
	if v == nil || len(v.Value) == 0 {
		return false
	}
	var b bool
	if err := json.Unmarshal(v.Value, &b); err == nil {
		return b
	}
	return false
}

func rawAXValueTristate(v *rawAXValue) string {
	if v == nil || len(v.Value) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(v.Value, &s); err == nil {
		return s
	}
	var b bool
	if err := json.Unmarshal(v.Value, &b); err == nil {
		if b {
			return "true"
		}
		return "false"
	}
	return ""
}

func rawAXValueInt(v *rawAXValue) int {
	if v == nil || len(v.Value) == 0 {
		return 0
	}
	var n float64
	if err := json.Unmarshal(v.Value, &n); err == nil {
		return int(n)
	}
	return 0
}

func rawAXNodeGraph(nodes []*rawAXNode) (map[string]*rawAXNode, []*rawAXNode, map[string][]*rawAXNode) {
	index := make(map[string]*rawAXNode, len(nodes))
	childrenByParent := make(map[string][]*rawAXNode, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		index[node.NodeID] = node
		if strings.TrimSpace(node.ParentID) != "" {
			childrenByParent[node.ParentID] = append(childrenByParent[node.ParentID], node)
		}
	}

	roots := make([]*rawAXNode, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if strings.TrimSpace(node.ParentID) == "" {
			roots = append(roots, node)
		}
	}
	if len(roots) == 0 && len(nodes) > 0 && nodes[0] != nil {
		roots = append(roots, nodes[0])
	}
	return index, roots, childrenByParent
}

func rawAXChildren(node *rawAXNode, index map[string]*rawAXNode, childrenByParent map[string][]*rawAXNode) []*rawAXNode {
	if node == nil {
		return nil
	}

	seen := make(map[string]struct{}, len(node.ChildIDs))
	children := make([]*rawAXNode, 0, len(node.ChildIDs))
	for _, childID := range node.ChildIDs {
		child, ok := index[childID]
		if !ok || child == nil {
			continue
		}
		children = append(children, child)
		seen[child.NodeID] = struct{}{}
	}
	for _, child := range childrenByParent[node.NodeID] {
		if child == nil {
			continue
		}
		if _, ok := seen[child.NodeID]; ok {
			continue
		}
		children = append(children, child)
	}
	return children
}

func buildCompactAriaTree(nodes []*rawAXNode, maxDepth int) (*ariaNodeInfo, map[string]cdp.BackendNodeID) {
	if maxDepth <= 0 {
		maxDepth = ariaSnapshotMaxDepth
	}

	index, roots, childrenByParent := rawAXNodeGraph(nodes)
	refs := make(map[string]cdp.BackendNodeID)
	refIdx := 1

	var convert func(n *rawAXNode, depth int) []ariaNodeInfo
	convert = func(n *rawAXNode, depth int) []ariaNodeInfo {
		if n == nil || depth > maxDepth {
			return nil
		}

		role := rawAXValueString(n.Role)
		name := rawAXValueString(n.Name)

		children := make([]ariaNodeInfo, 0, len(n.ChildIDs))
		for _, child := range rawAXChildren(n, index, childrenByParent) {
			children = append(children, convert(child, depth+1)...)
		}

		// Ignored nodes and generic wrappers: skip self, pass children up.
		if n.Ignored || ((role == "generic" || role == "none" || role == "") && name == "") {
			return children
		}

		info := ariaNodeInfo{
			Role:        role,
			Name:        name,
			Value:       rawAXValueString(n.Value),
			Description: rawAXValueString(n.Description),
			Children:    children,
		}

		for _, prop := range n.Properties {
			switch prop.Name {
			case "focused":
				info.Focused = rawAXValueBool(prop.Value)
			case "required":
				info.Required = rawAXValueBool(prop.Value)
			case "disabled":
				info.Disabled = rawAXValueBool(prop.Value)
			case "checked":
				info.Checked = rawAXValueTristate(prop.Value)
			case "expanded":
				info.Expanded = rawAXValueTristate(prop.Value)
			case "level":
				info.Level = rawAXValueInt(prop.Value)
			}
		}

		if isInteractiveRole(role) && n.BackendDOMNodeID != 0 {
			ref := fmt.Sprintf("%s%d", ariaRefPrefix, refIdx)
			refIdx++
			info.Ref = ref
			refs[ref] = n.BackendDOMNodeID
		}

		return []ariaNodeInfo{info}
	}

	topLevel := make([]ariaNodeInfo, 0, len(roots))
	for _, root := range roots {
		topLevel = append(topLevel, convert(root, 0)...)
	}

	switch len(topLevel) {
	case 0:
		return nil, refs
	case 1:
		root := topLevel[0]
		return &root, refs
	default:
		return &ariaNodeInfo{
			Role:     "document",
			Children: topLevel,
		}, refs
	}
}

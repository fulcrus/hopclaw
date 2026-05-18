package plugin

import "context"

type ToolRequest struct {
	Input    map[string]any
	Metadata map[string]any
}

type ToolOutput struct {
	Output     string
	Structured map[string]any
}

type ToolPlugin interface {
	Tool() Tool
}

type Tool struct {
	Decl        ToolDecl
	ExecuteFunc func(ctx context.Context, runtime PluginRuntime, request ToolRequest) (ToolOutput, error)
}

func (t Tool) Execute(ctx context.Context, runtime PluginRuntime, request ToolRequest) (ToolOutput, error) {
	if runtime == nil {
		return ToolOutput{}, ErrNilRuntime
	}
	if t.ExecuteFunc == nil {
		return ToolOutput{}, ErrNotImplemented
	}
	request.Input = cloneMapAny(request.Input)
	request.Metadata = cloneMapAny(request.Metadata)
	output, err := t.ExecuteFunc(ctx, runtime, request)
	if err != nil {
		return ToolOutput{}, err
	}
	output.Structured = cloneMapAny(output.Structured)
	return output, nil
}

func (t Tool) Manifest() ToolDecl {
	decl := t.Decl
	decl.InputSchema = cloneMapAny(decl.InputSchema)
	return decl
}

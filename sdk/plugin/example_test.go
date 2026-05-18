package plugin

import (
	"context"
	"fmt"
)

func Example() {
	m := Manifest{
		Name:    "my-custom-provider",
		Version: "1.0.0",
		Providers: map[string]ProviderDecl{
			"my-llm": {
				API:          "openai-completions",
				BaseURL:      "https://api.my-llm.com/v1",
				DefaultModel: "my-llm-chat",
			},
		},
	}

	fmt.Println(len(ValidateManifest(m)) == 0)
	// Output: true
}

func ExampleHookSet() {
	hooks := HookSet{
		HookFuncs{
			OnLoadFunc: func(context.Context, PluginRuntime) error {
				fmt.Println("first")
				return nil
			},
		},
		HookFuncs{
			OnLoadFunc: func(context.Context, PluginRuntime) error {
				fmt.Println("second")
				return nil
			},
		},
	}

	_ = hooks.OnLoad(context.Background(), NewMockRuntime())

	// Output:
	// first
	// second
}

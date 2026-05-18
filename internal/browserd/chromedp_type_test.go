package browserd

import (
	"strings"
	"testing"
)

func TestFocusAndPrepareTypeTargetRejectsNonTextTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		info typePreparationResult
		want string
	}{
		{
			name: "select",
			info: typePreparationResult{Found: true, Tag: "select", Typable: false},
			want: "use browser.select",
		},
		{
			name: "checkbox",
			info: typePreparationResult{Found: true, Tag: "input", InputType: "checkbox", Typable: false},
			want: "use browser.click",
		},
		{
			name: "radio",
			info: typePreparationResult{Found: true, Tag: "input", InputType: "radio", Typable: false},
			want: "use browser.click",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := typePreparationError("input[name=\"demo\"]", tt.info)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func TestFocusAndPrepareTypeTargetAllowsTextInputs(t *testing.T) {
	t.Parallel()

	if err := typePreparationError("input[name=\"demo\"]", typePreparationResult{
		Found:     true,
		Tag:       "input",
		InputType: "text",
		Typable:   true,
	}); err != nil {
		t.Fatalf("typePreparationError returned %v for text input", err)
	}
}

func TestBuildClickPreparationIncludesSubmitFallback(t *testing.T) {
	t.Parallel()

	selectorJS := buildClickPreparationJS("button[type=\"submit\"]")
	if !strings.Contains(selectorJS, "closest('button, input, a, label") {
		t.Fatalf("selector click JS = %q, want interactive-target normalization", selectorJS)
	}
	if !strings.Contains(selectorJS, "form.requestSubmit(el)") {
		t.Fatalf("selector click JS = %q, want requestSubmit fallback", selectorJS)
	}
	if !strings.Contains(selectorJS, "form.submit()") {
		t.Fatalf("selector click JS = %q, want submit fallback", selectorJS)
	}

	ariaJS := buildClickPreparationCallFunctionJS()
	if !strings.Contains(ariaJS, "form.requestSubmit(el)") {
		t.Fatalf("aria click JS = %q, want requestSubmit fallback", ariaJS)
	}
	if !strings.Contains(ariaJS, "form.submit()") {
		t.Fatalf("aria click JS = %q, want submit fallback", ariaJS)
	}
}

func TestBuildTypePreparationCallFunctionNormalizesToRealInputControl(t *testing.T) {
	t.Parallel()

	ariaJS := buildTypePreparationCallFunctionJS(false)
	if !strings.Contains(ariaJS, "label.control") {
		t.Fatalf("aria type JS = %q, want label.control fallback", ariaJS)
	}
	if !strings.Contains(ariaJS, "base.querySelector(selector)") {
		t.Fatalf("aria type JS = %q, want descendant input lookup", ariaJS)
	}
	if !strings.Contains(ariaJS, "blockedInputTypes = new Set(['checkbox', 'radio', 'submit', 'button', 'reset', 'file', 'color', 'range'])") {
		t.Fatalf("aria type JS = %q, want specialized input types to stay typable", ariaJS)
	}
}

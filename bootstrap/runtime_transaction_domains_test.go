package bootstrap

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatRuntimeTransactionApplyErrorIncludesAppliedDomains(t *testing.T) {
	t.Parallel()

	err := formatRuntimeTransactionApplyError([]runtimeTransactionDomain{
		{name: "models"},
		{name: "tools"},
	}, errors.New("channels: boom"))
	if err == nil {
		t.Fatal("formatRuntimeTransactionApplyError() = nil, want wrapped error")
	}
	if !strings.Contains(err.Error(), "applied before failure: models, tools") {
		t.Fatalf("error = %q, want applied domain list", err)
	}
}

func TestFormatRuntimeTransactionApplyErrorWithoutAppliedDomainsPreservesMessage(t *testing.T) {
	t.Parallel()

	err := formatRuntimeTransactionApplyError(nil, errors.New("channels: boom"))
	if err == nil {
		t.Fatal("formatRuntimeTransactionApplyError() = nil, want error")
	}
	if err.Error() != "channels: boom" {
		t.Fatalf("error = %q, want original message", err)
	}
}

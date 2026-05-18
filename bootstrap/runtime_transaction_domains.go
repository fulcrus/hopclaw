package bootstrap

import (
	"context"
	"fmt"
	"strings"
)

type runtimeTransactionDomain struct {
	name     string
	apply    func(context.Context) error
	commit   func(context.Context)
	rollback func(context.Context)
}

func applyRuntimeTransactionDomains(ctx context.Context, domains []runtimeTransactionDomain) ([]runtimeTransactionDomain, error) {
	if len(domains) == 0 {
		return nil, nil
	}
	applied := make([]runtimeTransactionDomain, 0, len(domains))
	for _, domain := range domains {
		if domain.apply == nil {
			continue
		}
		if err := domain.apply(ctx); err != nil {
			if domain.name != "" {
				return applied, fmt.Errorf("%s: %w", domain.name, err)
			}
			return applied, err
		}
		applied = append(applied, domain)
	}
	return applied, nil
}

func formatRuntimeTransactionApplyError(applied []runtimeTransactionDomain, err error) error {
	if err == nil {
		return nil
	}
	names := runtimeTransactionDomainNames(applied)
	if len(names) == 0 {
		return err
	}
	return fmt.Errorf("%w (applied before failure: %s)", err, strings.Join(names, ", "))
}

func runtimeTransactionDomainNames(domains []runtimeTransactionDomain) []string {
	names := make([]string, 0, len(domains))
	for _, domain := range domains {
		if strings.TrimSpace(domain.name) == "" {
			continue
		}
		names = append(names, domain.name)
	}
	return names
}

func rollbackRuntimeTransactionDomains(ctx context.Context, domains []runtimeTransactionDomain) {
	for i := len(domains) - 1; i >= 0; i-- {
		if domains[i].rollback != nil {
			domains[i].rollback(ctx)
		}
	}
}

func commitRuntimeTransactionDomains(ctx context.Context, domains []runtimeTransactionDomain) {
	for _, domain := range domains {
		if domain.commit != nil {
			domain.commit(ctx)
		}
	}
}

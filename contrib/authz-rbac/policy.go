package authzrbac

import (
	"context"
	"sort"
	"strings"

	"github.com/fulcrus/hopclaw/authz"
	"github.com/fulcrus/hopclaw/config"
)

// Role represents a user role in the built-in RBAC policy.
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
	RoleAgent    Role = "agent"
)

const (
	metadataKeyRole    = "role"
	metadataKeyGroups  = "groups"
	defaultScopePrefix = "role:"
	implicitRole       = RoleOperator
)

// RBACPolicy implements the stable authz decider contract with a role matrix.
type RBACPolicy struct {
	mode              string
	defaultRole       Role
	roleMetadataKeys  []string
	groupMetadataKeys []string
	scopePrefixes     []string
	groupRoleMapping  map[string]Role
	rules             map[Role]map[authz.Resource]map[authz.Action]bool
}

// NewDefaultDecider returns the built-in operator control-plane policy.
func NewDefaultDecider() *RBACPolicy {
	p := newEmptyPolicy()
	p.mode = "overlay"

	p.grant(RoleAdmin, authz.ResourceAll, authz.ActionRead, authz.ActionWrite, authz.ActionAdmin, authz.ActionApprove, authz.ActionExecute)
	p.grant(RoleOperator, authz.ResourceAll, authz.ActionRead, authz.ActionWrite, authz.ActionExecute, authz.ActionApprove)

	p.grant(RoleViewer, authz.ResourceRuns, authz.ActionRead)
	p.grant(RoleViewer, authz.ResourceSessions, authz.ActionRead)
	p.grant(RoleViewer, authz.ResourceTools, authz.ActionRead)
	p.grant(RoleViewer, authz.ResourceApprovals, authz.ActionRead)

	p.grant(RoleAgent, authz.ResourceTools, authz.ActionExecute, authz.ActionRead)
	p.grant(RoleAgent, authz.ResourceRuns, authz.ActionExecute, authz.ActionRead)
	p.grant(RoleAgent, authz.ResourceSessions, authz.ActionExecute, authz.ActionRead)

	return p
}

// NewFromConfig builds a policy from config, starting from the default matrix
// in overlay mode or an empty matrix in replace mode.
func NewFromConfig(cfg config.AuthRBACConfig) *RBACPolicy {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	var policy *RBACPolicy
	switch mode {
	case "replace":
		policy = newEmptyPolicy()
		policy.mode = "replace"
	default:
		policy = NewDefaultDecider()
		policy.mode = "overlay"
	}

	if role := normalizeRole(cfg.DefaultRole); role != "" {
		policy.defaultRole = role
	}
	if items := normalizeStringList(cfg.RoleMetadataKeys, false); len(items) > 0 {
		policy.roleMetadataKeys = items
	}
	if items := normalizeStringList(cfg.GroupMetadataKeys, false); len(items) > 0 {
		policy.groupMetadataKeys = items
	}
	if items := normalizeStringList(cfg.ScopePrefixes, false); len(items) > 0 {
		policy.scopePrefixes = items
	}
	if len(cfg.GroupRoles) > 0 {
		if mode == "replace" {
			policy.groupRoleMapping = make(map[string]Role, len(cfg.GroupRoles))
		}
		for group, role := range cfg.GroupRoles {
			name := strings.ToLower(strings.TrimSpace(group))
			if name == "" {
				continue
			}
			policy.groupRoleMapping[name] = normalizeRole(role)
		}
	}
	policy.applyRoleConfigs(cfg.Roles)
	return policy
}

// Decide evaluates the request against the resolved role.
func (p *RBACPolicy) Decide(_ context.Context, req authz.AuthorizationRequest) (authz.AuthorizationDecision, error) {
	role := p.ResolveRoleOrDefault(req.Principal, defaultRoleFromRequest(req))
	allowed := p.IsAllowed(role, req.Resource, req.Action)
	decision := authz.AuthorizationDecision{
		Allowed: allowed,
		Reason:  decisionReason(role, req.Resource, req.Action, allowed),
		Source:  "rbac",
		Metadata: map[string]string{
			"mode":          strings.TrimSpace(p.mode),
			"resolved_role": string(role),
		},
	}
	return decision, nil
}

// DescribeAuthorization renders the effective RBAC matrix for authz APIs.
func (p *RBACPolicy) DescribeAuthorization() authz.Summary {
	if p == nil {
		return authz.Summary{}
	}

	roleNames := make([]string, 0, len(p.rules))
	resourcesSet := make(map[string]struct{})
	actionsSet := make(map[string]struct{})
	for role := range p.rules {
		roleNames = append(roleNames, string(role))
	}
	sort.Strings(roleNames)

	bindings := make([]authz.BindingSummary, 0, len(roleNames))
	for _, roleName := range roleNames {
		role := Role(roleName)
		resourceNames := make([]string, 0, len(p.rules[role]))
		actionSet := make(map[string]struct{})
		for resource := range p.rules[role] {
			resourceNames = append(resourceNames, string(resource))
			resourcesSet[string(resource)] = struct{}{}
		}
		sort.Strings(resourceNames)

		descriptionParts := make([]string, 0, len(resourceNames))
		for _, resourceName := range resourceNames {
			actionNames := make([]string, 0, len(p.rules[role][authz.Resource(resourceName)]))
			for action := range p.rules[role][authz.Resource(resourceName)] {
				actionNames = append(actionNames, string(action))
				actionsSet[string(action)] = struct{}{}
				actionSet[string(action)] = struct{}{}
			}
			sort.Strings(actionNames)
			descriptionParts = append(descriptionParts, resourceName+" ("+strings.Join(actionNames, ", ")+")")
		}

		bindings = append(bindings, authz.BindingSummary{
			Name:        roleName,
			Kind:        "role",
			Description: strings.Join(descriptionParts, " · "),
			Resources:   append([]string(nil), resourceNames...),
			Actions:     sortedKeys(actionSet),
		})
	}

	metadata := map[string]string{
		"default_role":        string(p.defaultRole),
		"role_metadata_keys":  strings.Join(p.roleMetadataKeys, ","),
		"group_metadata_keys": strings.Join(p.groupMetadataKeys, ","),
		"scope_prefixes":      strings.Join(p.scopePrefixes, ","),
	}
	if p.defaultRole == "" {
		metadata["implicit_authenticated_role"] = string(implicitRole)
	}

	notes := []string{
		"Built-in role matrix support is provided by contrib/authz-rbac.",
	}
	if p.defaultRole == "" {
		notes = append(notes, "Authenticated callers without explicit role claims fall back to operator; set auth.rbac.default_role to override this compatibility behavior.")
	}

	return authz.Summary{
		Kind:          "rbac",
		Name:          "contrib/authz-rbac",
		Mode:          strings.TrimSpace(p.mode),
		DefaultEffect: "deny",
		Resources:     sortedKeys(resourcesSet),
		Actions:       sortedKeys(actionsSet),
		Bindings:      bindings,
		Notes:         notes,
		Metadata:      trimEmptyMap(metadata),
	}
}

// ResolveRoleOrDefault resolves the caller role, honoring configured defaults.
func (p *RBACPolicy) ResolveRoleOrDefault(principal *authz.Principal, fallback Role) Role {
	if p == nil {
		return fallback
	}
	if p.defaultRole != "" {
		fallback = p.defaultRole
	}
	if principal == nil {
		return fallback
	}
	if principal.Metadata != nil {
		for _, key := range p.roleMetadataKeys {
			if role := normalizeRole(principal.Metadata[key]); role != "" {
				return role
			}
		}
	}
	for _, raw := range principal.Scopes {
		scope := strings.TrimSpace(raw)
		if scope == "" {
			continue
		}
		for _, prefix := range p.scopePrefixes {
			if prefix == "" || !strings.HasPrefix(scope, prefix) {
				continue
			}
			if role := normalizeRole(strings.TrimPrefix(scope, prefix)); role != "" {
				return role
			}
		}
	}
	if principal.Metadata != nil {
		for _, key := range p.groupMetadataKeys {
			if role := p.roleFromGroups(principal.Metadata[key]); role != "" {
				return role
			}
		}
	}
	return fallback
}

func defaultRoleFromRequest(req authz.AuthorizationRequest) Role {
	if req.Principal == nil {
		return RoleViewer
	}
	return implicitRole
}

func decisionReason(role Role, resource authz.Resource, action authz.Action, allowed bool) string {
	if allowed {
		return "role " + string(role) + " allows " + string(resource) + "/" + string(action)
	}
	return "role " + string(role) + " does not allow " + string(resource) + "/" + string(action)
}

func (p *RBACPolicy) IsAllowed(role Role, resource authz.Resource, action authz.Action) bool {
	if p == nil {
		return false
	}
	role = normalizeRole(string(role))
	resource = normalizeResource(resource)
	action = normalizeAction(action)
	if role == "" || resource == "" || action == "" {
		return false
	}
	roleRules, ok := p.rules[role]
	if !ok {
		return false
	}
	if resourceActions, ok := roleRules[resource]; ok && resourceActions[action] {
		return true
	}
	if wildcardActions, ok := roleRules[authz.ResourceAll]; ok && wildcardActions[action] {
		return true
	}
	return false
}

func (p *RBACPolicy) roleFromGroups(groups string) Role {
	for _, raw := range strings.Split(groups, ",") {
		group := strings.ToLower(strings.TrimSpace(raw))
		if group == "" {
			continue
		}
		if role, ok := p.groupRoleMapping[group]; ok && role != "" {
			return role
		}
	}
	return ""
}

func (p *RBACPolicy) applyRoleConfigs(items []config.AuthRBACRoleConfig) {
	if len(items) == 0 {
		return
	}
	index := make(map[Role]config.AuthRBACRoleConfig, len(items))
	for _, item := range items {
		role := normalizeRole(item.Name)
		if role == "" {
			continue
		}
		index[role] = item
	}
	state := make(map[Role]int, len(index))
	var apply func(Role)
	apply = func(role Role) {
		switch state[role] {
		case 1, 2:
			return
		}
		item, ok := index[role]
		if !ok {
			return
		}
		state[role] = 1
		if item.Replace {
			delete(p.rules, role)
		}
		for _, parentName := range item.Extends {
			parent := normalizeRole(parentName)
			if parent == "" {
				continue
			}
			apply(parent)
			p.copyRoleGrants(parent, role)
		}
		for _, grant := range item.Grants {
			resource := normalizeResource(authz.Resource(strings.TrimSpace(grant.Resource)))
			if resource == "" {
				continue
			}
			actions := make([]authz.Action, 0, len(grant.Permissions))
			for _, raw := range grant.Permissions {
				if action := normalizeAction(authz.Action(strings.TrimSpace(raw))); action != "" {
					actions = append(actions, action)
				}
			}
			p.grant(role, resource, actions...)
		}
		state[role] = 2
	}
	for role := range index {
		apply(role)
	}
}

func (p *RBACPolicy) grant(role Role, resource authz.Resource, actions ...authz.Action) {
	role = normalizeRole(string(role))
	resource = normalizeResource(resource)
	if role == "" || resource == "" {
		return
	}
	if _, ok := p.rules[role]; !ok {
		p.rules[role] = make(map[authz.Resource]map[authz.Action]bool)
	}
	if _, ok := p.rules[role][resource]; !ok {
		p.rules[role][resource] = make(map[authz.Action]bool)
	}
	for _, raw := range actions {
		action := normalizeAction(raw)
		if action == "" {
			continue
		}
		p.rules[role][resource][action] = true
	}
}

func (p *RBACPolicy) copyRoleGrants(from Role, to Role) {
	from = normalizeRole(string(from))
	to = normalizeRole(string(to))
	if from == "" || to == "" {
		return
	}
	resources, ok := p.rules[from]
	if !ok {
		return
	}
	for resource, actions := range resources {
		for action, allowed := range actions {
			if allowed {
				p.grant(to, resource, action)
			}
		}
	}
}

func newEmptyPolicy() *RBACPolicy {
	return &RBACPolicy{
		roleMetadataKeys:  []string{metadataKeyRole},
		groupMetadataKeys: []string{metadataKeyGroups},
		scopePrefixes:     []string{defaultScopePrefix},
		groupRoleMapping:  cloneDefaultGroupRoleMapping(),
		rules:             make(map[Role]map[authz.Resource]map[authz.Action]bool),
	}
}

func cloneDefaultGroupRoleMapping() map[string]Role {
	return map[string]Role{
		"admins":    RoleAdmin,
		"admin":     RoleAdmin,
		"operators": RoleOperator,
		"operator":  RoleOperator,
		"viewers":   RoleViewer,
		"viewer":    RoleViewer,
		"agents":    RoleAgent,
		"agent":     RoleAgent,
	}
}

func normalizeRole(value string) Role {
	return Role(strings.ToLower(strings.TrimSpace(value)))
}

func normalizeResource(value authz.Resource) authz.Resource {
	resource, ok := authz.ParseResource(string(value))
	if !ok {
		return ""
	}
	return resource
}

func normalizeAction(value authz.Action) authz.Action {
	action, ok := authz.ParseAction(string(value))
	if !ok {
		return ""
	}
	return action
}

func normalizeStringList(items []string, lower bool) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, raw := range items {
		value := strings.TrimSpace(raw)
		if lower {
			value = strings.ToLower(value)
		}
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func sortedKeys(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func trimEmptyMap(items map[string]string) map[string]string {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]string, len(items))
	for key, value := range items {
		if strings.TrimSpace(value) == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

export function buildSettingsPluginsSection({
  api,
  showToast,
  t,
  settledValue,
  moduleHealth,
  healthyModuleStates,
  defaultPageSize,
}) {
  return {
    pluginsData: [],
    pluginsModules: [],
    pluginSource: '',
    pluginsError: '',
    pluginsModulesUnavailable: false,
    pluginsPage: 1,
    pluginsPageSize: defaultPageSize,
    pluginModulesPage: 1,
    pluginModulesPageSize: defaultPageSize,

    async installPlugin() {
      const source = this.pluginSource.trim();
      if (!source) return;
      try {
        await api.post('/operator/plugins', { source });
        showToast(t('settingsPluginInstalled') || 'Plugin installed', 'success');
        this.pluginSource = '';
        await this.loadPlugins();
      } catch (_) {}
    },

    async deletePlugin(name) {
      if (!name || !confirm((t('settingsDeletePluginConfirm') || 'Delete plugin') + ' "' + name + '"?')) return;
      try {
        await api.del('/operator/plugins/' + encodeURIComponent(name));
        showToast(t('settingsPluginRemoved') || 'Plugin removed', 'success');
        await this.loadPlugins();
      } catch (_) {}
    },

    pluginComponentSummary(plugin) {
      const counts = plugin && plugin.component_counts;
      if (!counts || typeof counts !== 'object') return '';
      return Object.keys(counts).sort().map(key => key + ': ' + counts[key]).join(' · ');
    },

    pluginRuntimeModule(plugin) {
      const runtimeModule = plugin && plugin.runtime_module;
      return runtimeModule && typeof runtimeModule === 'object' ? runtimeModule : null;
    },

    pluginRuntimeModuleSummary(plugin) {
      const runtimeModule = this.pluginRuntimeModule(plugin);
      if (!runtimeModule) return '';
      const parts = [];
      const level = String(runtimeModule.level || '').trim().toLowerCase();
      const delivery = String(runtimeModule.delivery || '').trim().toLowerCase();
      const health = runtimeModule.health && typeof runtimeModule.health === 'object'
        ? String(runtimeModule.health.status || '').trim().toLowerCase()
        : '';
      if (level) parts.push(level);
      if (delivery) parts.push(delivery);
      if (health) parts.push(health);
      return parts.join(' · ');
    },

    pluginRuntimeModuleHealthSummary(plugin) {
      const runtimeModule = this.pluginRuntimeModule(plugin);
      if (!runtimeModule || !runtimeModule.health || typeof runtimeModule.health !== 'object') return '';
      return String(runtimeModule.health.summary || '').trim();
    },

    pluginModuleKey(module) {
      return String(module && (module.id || module.ID || module.name || module.Name) || '').trim();
    },

    pluginModuleName(module) {
      return String(module && (module.name || module.Name || module.id || module.ID) || '').trim() || '-';
    },

    pluginModuleSource(module) {
      return String(module && (module.source || module.Source) || 'builtin').trim().toLowerCase() || 'builtin';
    },

    pluginModuleSourceBadge(source) {
      switch (String(source || '').trim().toLowerCase()) {
        case 'plugin':
          return 'hc-badge-blue';
        case 'external':
          return 'hc-badge-orange';
        default:
          return 'hc-badge-gray';
      }
    },

    pluginModuleHealthStatus(module) {
      return moduleHealth(module && (module.health || module.Health)) || 'unknown';
    },

    pluginModuleHealthSummary(module) {
      const health = module && (module.health || module.Health);
      if (!health || typeof health !== 'object') return '';
      return String(health.summary || health.Summary || '').trim();
    },

    pluginModuleDescription(module) {
      return String(module && (module.description || module.Description) || '').trim();
    },

    pluginModuleDelivery(module) {
      return String(module && (module.delivery || module.Delivery) || '').trim().toLowerCase();
    },

    pluginModuleVersion(module) {
      return String(module && (module.version || module.Version) || '').trim();
    },

    pluginModuleDependencies(module) {
      const deps = module && (module.dependencies || module.Dependencies);
      return Array.isArray(deps) ? deps.filter(Boolean) : [];
    },

    pluginModuleMetaSummary(module) {
      const parts = [];
      const version = this.pluginModuleVersion(module);
      if (version) parts.push('v' + version);
      const deps = this.pluginModuleDependencies(module);
      if (deps.length > 0) parts.push(deps.length + ' dep' + (deps.length === 1 ? '' : 's'));
      return parts.join(' · ');
    },

    pluginModuleContributions(module) {
      const contributions = module && (module.contributions || module.Contributions);
      return contributions && typeof contributions === 'object' ? contributions : {};
    },

    pluginModuleContributionTotal(module) {
      const contributions = this.pluginModuleContributions(module);
      return Number(contributions.total_count || contributions.TotalCount || 0) || 0;
    },

    pluginModuleContributionBadges(module) {
      const contributions = this.pluginModuleContributions(module);
      const badges = [];
      const toolCount = Number(contributions.tool_count || contributions.ToolCount || 0) || 0;
      const channelCount = Number(contributions.channel_count || contributions.ChannelCount || 0) || 0;
      const providerCount = Number(contributions.provider_count || contributions.ProviderCount || 0) || 0;
      const mcpServerCount = Number(contributions.mcp_server_count || contributions.MCPServerCount || 0) || 0;
      const agentCount = Number(contributions.agent_count || contributions.AgentCount || 0) || 0;
      const skillDirCount = Number(contributions.skill_dir_count || contributions.SkillDirCount || 0) || 0;
      const hookDirCount = Number(contributions.hook_dir_count || contributions.HookDirCount || 0) || 0;
      if (toolCount > 0) badges.push(toolCount + ' tool' + (toolCount === 1 ? '' : 's'));
      if (channelCount > 0) badges.push(channelCount + ' channel' + (channelCount === 1 ? '' : 's'));
      if (providerCount > 0) badges.push(providerCount + ' provider' + (providerCount === 1 ? '' : 's'));
      if (mcpServerCount > 0) badges.push(mcpServerCount + ' MCP');
      if (agentCount > 0) badges.push(agentCount + ' agent' + (agentCount === 1 ? '' : 's'));
      if (skillDirCount > 0) badges.push(skillDirCount + ' skill dir' + (skillDirCount === 1 ? '' : 's'));
      if (hookDirCount > 0) badges.push(hookDirCount + ' hook dir' + (hookDirCount === 1 ? '' : 's'));
      return badges;
    },

    pluginModuleContributionPreview(module) {
      const contributions = this.pluginModuleContributions(module);
      const names = []
        .concat(contributions.tool_names || contributions.ToolNames || [])
        .concat(contributions.channel_names || contributions.ChannelNames || [])
        .concat(contributions.provider_names || contributions.ProviderNames || [])
        .concat(contributions.mcp_server_names || contributions.MCPServerNames || [])
        .concat(contributions.agent_names || contributions.AgentNames || [])
        .map(item => String(item || '').trim())
        .filter(Boolean);
      if (names.length === 0) return '';
      const preview = names.slice(0, 4).join(', ');
      if (names.length <= 4) return preview;
      return preview + ' +' + (names.length - 4);
    },

    pluginModuleSummaryChips() {
      const counts = this.pluginsModules.reduce((acc, module) => {
        const source = this.pluginModuleSource(module);
        acc[source] = (acc[source] || 0) + 1;
        return acc;
      }, {});
      const chips = Object.keys(counts)
        .sort()
        .map(source => counts[source] + ' ' + source);
      const degraded = this.pluginsModules.filter(module => {
        const health = this.pluginModuleHealthStatus(module);
        return !healthyModuleStates.includes(health) && health !== 'unknown';
      }).length;
      if (degraded > 0) chips.push(degraded + ' degraded');
      return chips;
    },

    sortPluginModules(items) {
      return (Array.isArray(items) ? items.slice() : []).sort((left, right) => {
        const leftHealth = this.pluginModuleHealthStatus(left);
        const rightHealth = this.pluginModuleHealthStatus(right);
        const leftPriority = (!healthyModuleStates.includes(leftHealth) && leftHealth !== 'unknown' ? 4 : 0) +
          (this.pluginModuleSource(left) !== 'builtin' ? 2 : 0);
        const rightPriority = (!healthyModuleStates.includes(rightHealth) && rightHealth !== 'unknown' ? 4 : 0) +
          (this.pluginModuleSource(right) !== 'builtin' ? 2 : 0);
        if (leftPriority !== rightPriority) return rightPriority - leftPriority;
        return this.pluginModuleName(left).localeCompare(this.pluginModuleName(right));
      });
    },

    async loadPluginModules() {
      const extensionsResult = await Promise.allSettled([
        api.get('/operator/extensions', { background: true }),
      ]);
      const extensionsData = settledValue(extensionsResult[0]);
      const modules = extensionsData ? (Array.isArray(extensionsData.modules) ? extensionsData.modules : []) : [];
      this.pluginsModules = this.sortPluginModules(modules);
      this.pluginsModulesUnavailable = !(extensionsData && Array.isArray(extensionsData.modules));
      if (this.pluginModulesPage > this.pluginModulesTotalPages) this.pluginModulesPage = 1;
    },

    async loadPlugins() {
      this.pluginsError = '';
      const pluginsResult = await Promise.allSettled([
        api.get('/operator/plugins'),
      ]);
      const data = settledValue(pluginsResult[0]);

      if (pluginsResult[0].status === 'fulfilled') {
        this.pluginsData = data ? (Array.isArray(data) ? data : (data.items || [])) : [];
      } else {
        this.pluginsData = [];
        this.pluginsError = (pluginsResult[0].reason && pluginsResult[0].reason.message) || t('loadError');
      }
      await this.loadPluginModules();
      if (this.pluginsPage > this.pluginsTotalPages) this.pluginsPage = 1;
    },
  };
}

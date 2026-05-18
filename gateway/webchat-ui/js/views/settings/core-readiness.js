export function buildSettingsCoreReadinessSection({
  api,
  t,
  settledValue,
  capabilityHealth,
  moduleHealth,
  healthyCapabilityStates,
  healthyModuleStates,
  connectedChannelStates,
  warningChannelStates,
}) {
  return {
    coreSetupStatus: null,
    coreModules: [],
    coreCapabilities: [],
    coreChannelsHealth: [],
    coreReadinessLoading: false,
    coreReadinessWarnings: [],
    coreSetupUnavailable: false,
    coreModulesUnavailable: false,
    coreCapabilitiesUnavailable: false,
    coreChannelsUnavailable: false,

    coreConfiguredProviders() {
      const providers = (this.coreSetupStatus && this.coreSetupStatus.providers) || [];
      return Array.from(new Set((providers || []).filter(Boolean)));
    },

    coreDetectedProviders() {
      const providers = (this.coreSetupStatus && this.coreSetupStatus.detected_providers) || [];
      return Array.from(new Set((providers || []).map(item => typeof item === 'string' ? item : (item.name || item.id || '')).filter(Boolean)));
    },

    coreCapabilityReady(name) {
      return this.coreCapabilities.some(capability => {
        const manifest = capability.manifest || capability.Manifest || {};
        const capName = String(capability.name || capability.Name || manifest.name || manifest.Name || '').toLowerCase();
        return (capName === name || capName.indexOf(name + '.') === 0) &&
          healthyCapabilityStates.includes(capabilityHealth(capability.health || capability.Health));
      });
    },

    coreConnectedChannelsCount() {
      return this.coreChannelsHealth.filter(item => connectedChannelStates.includes(String(item.state || item.status || '').toLowerCase())).length;
    },

    coreWarningChannelsCount() {
      return this.coreChannelsHealth.filter(item => warningChannelStates.includes(String(item.state || item.status || '').toLowerCase())).length;
    },

    coreModuleHealth(module) {
      return moduleHealth(module && (module.health || module.Health)) || 'unknown';
    },

    coreDegradedModulesCount() {
      return this.coreModules.filter(module => {
        const health = this.coreModuleHealth(module);
        return !healthyModuleStates.includes(health) && health !== 'unknown';
      }).length;
    },

    coreModuleSourceCounts() {
      return this.coreModules.reduce((counts, module) => {
        const source = String(module && (module.source || module.Source) || 'builtin').trim().toLowerCase() || 'builtin';
        counts[source] = (counts[source] || 0) + 1;
        return counts;
      }, {});
    },

    coreModulesSummary() {
      if (this.coreModulesUnavailable) return t('settingsReadinessCapPackUnavailable') || 'Capability pack snapshot is unavailable right now';
      if (this.coreModules.length === 0) return t('settingsReadinessNoCapPacks') || 'No capability pack metadata reported yet';
      const counts = this.coreModuleSourceCounts();
      const parts = Object.keys(counts)
        .sort()
        .map(source => counts[source] + ' ' + source);
      let summary = this.coreModules.length + ' ' + (t('settingsReadinessPacksLoaded') || 'pack(s) loaded');
      if (parts.length > 0) summary += ' (' + parts.join(', ') + ')';
      return summary;
    },

    coreReadinessItems() {
      const configured = this.coreConfiguredProviders();
      const detected = this.coreDetectedProviders();
      const connectedChannels = this.coreConnectedChannelsCount();
      const warningChannels = this.coreWarningChannelsCount();
      const degradedModules = this.coreDegradedModulesCount();
      const hasPlugins = (this.coreModuleSourceCounts().plugin || 0) > 0;
      return [
        {
          key: 'models',
          title: t('settingsReadinessModels') || 'Models',
          desc: this.coreSetupUnavailable
            ? (t('settingsReadinessSetupUnavailable') || 'Setup status is unavailable right now')
            : (configured.length ? ((t('settingsReadinessReady') || 'Ready') + ': ' + configured.join(', ')) : (detected.length ? ((t('settingsReadinessDetectedEnvKeys') || 'Detected env keys') + ': ' + detected.join(', ')) : (t('settingsReadinessNoProvider') || 'No provider configured yet'))),
          href: configured.length ? '#/settings/models' : '#/setup',
          cta: configured.length ? (t('settingsReadinessReview') || 'Review') : (t('settingsReadinessConfigure') || 'Configure'),
          ok: !this.coreSetupUnavailable && configured.length > 0,
          warn: this.coreSetupUnavailable || (!configured.length && detected.length > 0),
        },
        {
          key: 'channels',
          title: t('settingsReadinessChannels') || 'Channels',
          desc: this.coreChannelsUnavailable
            ? (t('settingsReadinessChannelHealthUnavailable') || 'Channel health is unavailable right now')
            : (connectedChannels > 0 ? (connectedChannels + ' ' + (t('settingsReadinessConnectedChannels') || 'connected channel(s)')) : (t('settingsReadinessNoActiveChannel') || 'No active channel connection yet')),
          href: '#/settings/channels',
          cta: connectedChannels > 0 ? (t('settingsReadinessManage') || 'Manage') : (t('settingsReadinessConnect') || 'Connect'),
          ok: !this.coreChannelsUnavailable && connectedChannels > 0,
          warn: this.coreChannelsUnavailable || warningChannels > 0,
        },
        {
          key: 'modules',
          title: t('settingsReadinessCapPacks') || 'Capability packs',
          desc: this.coreModulesUnavailable
            ? (t('settingsReadinessCapPackUnavailable') || 'Capability pack snapshot is unavailable right now')
            : (degradedModules > 0 ? (degradedModules + ' ' + (t('settingsReadinessCapPackNeedAttention') || 'capability pack(s) need attention')) : ((t('settingsReadinessReady') || 'Ready') + ': ' + this.coreModulesSummary())),
          href: hasPlugins ? '#/settings/plugins' : '#/settings/infrastructure',
          cta: this.coreModules.length > 0 ? (t('settingsReadinessInspect') || 'Inspect') : (t('settingsReadinessReview') || 'Review'),
          ok: !this.coreModulesUnavailable && this.coreModules.length > 0 && degradedModules === 0,
          warn: this.coreModulesUnavailable || degradedModules > 0,
        },
        {
          key: 'browser',
          title: t('settingsReadinessBrowserHelper') || 'Browser helper',
          desc: this.coreCapabilitiesUnavailable
            ? (t('settingsReadinessCapUnavailable') || 'Capability snapshot is unavailable right now')
            : (this.coreCapabilityReady('browser') ? (t('settingsReadinessBrowserHealthy') || 'Browser capability reports healthy') : (t('settingsReadinessBrowserNotReady') || 'Browser helper is not ready yet')),
          href: '#/settings/infrastructure',
          cta: this.coreCapabilityReady('browser') ? (t('settingsReadinessInspect') || 'Inspect') : (t('settingsReadinessFix') || 'Fix'),
          ok: !this.coreCapabilitiesUnavailable && this.coreCapabilityReady('browser'),
          warn: this.coreCapabilitiesUnavailable,
        },
        {
          key: 'desktop',
          title: t('settingsReadinessDesktopHelper') || 'Desktop helper',
          desc: this.coreCapabilitiesUnavailable
            ? (t('settingsReadinessCapUnavailable') || 'Capability snapshot is unavailable right now')
            : (this.coreCapabilityReady('desktop') ? (t('settingsReadinessDesktopHealthy') || 'Desktop capability reports healthy') : (t('settingsReadinessDesktopNotReady') || 'Desktop helper is not ready yet')),
          href: '#/settings/infrastructure',
          cta: this.coreCapabilityReady('desktop') ? (t('settingsReadinessInspect') || 'Inspect') : (t('settingsReadinessFix') || 'Fix'),
          ok: !this.coreCapabilitiesUnavailable && this.coreCapabilityReady('desktop'),
          warn: this.coreCapabilitiesUnavailable,
        },
      ];
    },

    coreReadinessCompleted() {
      return this.coreReadinessItems().filter(item => item.ok).length;
    },

    coreReadinessSummary() {
      if (this.coreReadinessWarnings.length > 0) return this.coreReadinessWarnings[0];
      const configured = this.coreConfiguredProviders();
      const degradedModules = this.coreDegradedModulesCount();
      if (!configured.length) {
        const detected = this.coreDetectedProviders();
        return detected.length ? ((t('settingsReadinessKeysDetected') || 'Keys detected for') + ' ' + detected.join(', ')) : (t('settingsReadinessNoModelConfigured') || 'No model configured yet');
      }
      if (degradedModules > 0) return degradedModules + ' ' + (t('settingsReadinessCapPackNeedAttention') || 'capability pack(s) need attention');
      if (this.coreWarningChannelsCount() > 0) return this.coreWarningChannelsCount() + ' ' + (t('settingsReadinessNeedReview') || 'need review');
      return configured.join(', ') + ' · ' + this.coreModulesSummary();
    },

    coreReadinessHeadline() {
      if (this.coreReadinessWarnings.length > 0) {
        return t('settingsReadinessUnavailableWarning') || 'Some readiness signals are unavailable. Fix backend reachability before trusting this score.';
      }
      if (this.coreDegradedModulesCount() > 0) {
        return t('settingsReadinessDegradedWarning') || 'Core setup is mostly there, but some capability packs still need attention before the runtime looks trustworthy.';
      }
      const completed = this.coreReadinessCompleted();
      const total = this.coreReadinessItems().length || 5;
      if (completed >= total) return t('settingsReadinessAllReady') || 'Models, channels, packs, and operator helpers all report ready. This console looks trustworthy enough for real work.';
      if (completed >= Math.max(2, total - 2)) return t('settingsReadinessPartlyReady') || 'The basics are partly there. Finish the remaining helpers so runs, automations, and delivery feel dependable.';
      return t('settingsReadinessNeedsEssentials') || 'Core setup still needs a few essentials. This panel tracks the real runtime signals, not just page grouping.';
    },

    async loadCoreReadiness() {
      this.coreReadinessLoading = true;
      try {
        const [setupResult, extensionsResult, channelHealthResult, capabilitiesResult] = await Promise.allSettled([
          api.get('/operator/setup/status', { background: true }),
          api.get('/operator/extensions', { background: true }),
          api.get('/operator/channels/health', { background: true }),
          api.get('/operator/capabilities', { background: true }),
        ]);
        const setupStatus = settledValue(setupResult);
        const extensionsData = settledValue(extensionsResult);
        const channelHealth = settledValue(channelHealthResult);
        const capabilities = settledValue(capabilitiesResult);
        this.coreSetupStatus = setupStatus;
        this.coreSetupUnavailable = !setupStatus;
        const extensionModules = extensionsData ? (Array.isArray(extensionsData.modules) ? extensionsData.modules : []) : [];
        const extensionCaps = extensionsData ? (Array.isArray(extensionsData.capabilities) ? extensionsData.capabilities : []) : [];
        const extensionChannels = extensionsData ? (Array.isArray(extensionsData.channels) ? extensionsData.channels : []) : [];
        const moduleSourceAvailable = (extensionsData && Array.isArray(extensionsData.modules));
        const capabilitySourceAvailable = (extensionsData && Array.isArray(extensionsData.capabilities)) || Boolean(capabilities);
        const channelSourceAvailable = (extensionsData && Array.isArray(extensionsData.channels)) || Boolean(channelHealth);
        this.coreModules = extensionModules;
        this.coreModulesUnavailable = this.coreModules.length === 0 && !moduleSourceAvailable;
        this.coreChannelsHealth = extensionChannels.length
          ? extensionChannels.map(item => item.health || { name: item.name || '', state: item.status || 'unknown' })
          : (channelHealth ? (Array.isArray(channelHealth) ? channelHealth : (channelHealth.items || channelHealth.channels || [])) : []);
        this.coreChannelsUnavailable = this.coreChannelsHealth.length === 0 && !channelSourceAvailable;
        this.coreCapabilities = extensionCaps.length
          ? extensionCaps
          : (capabilities ? (Array.isArray(capabilities) ? capabilities : (capabilities.items || capabilities.capabilities || [])) : []);
        this.coreCapabilitiesUnavailable = this.coreCapabilities.length === 0 && !capabilitySourceAvailable;
        const warnings = [];
        if (this.coreSetupUnavailable) warnings.push(t('settingsReadinessSetupWarning') || 'Setup readiness unavailable');
        if (this.coreModulesUnavailable) warnings.push(t('settingsReadinessCapPackSnapshotWarning') || 'Capability pack snapshot unavailable');
        if (this.coreChannelsUnavailable) warnings.push(t('settingsReadinessChannelHealthWarning') || 'Channel health unavailable');
        if (this.coreCapabilitiesUnavailable) warnings.push(t('settingsReadinessCapSnapshotWarning') || 'Capability snapshot unavailable');
        this.coreReadinessWarnings = warnings;
      } catch (_) {
        this.coreSetupStatus = null;
        this.coreModules = [];
        this.coreChannelsHealth = [];
        this.coreCapabilities = [];
        this.coreSetupUnavailable = true;
        this.coreModulesUnavailable = true;
        this.coreChannelsUnavailable = true;
        this.coreCapabilitiesUnavailable = true;
        this.coreReadinessWarnings = [t('settingsReadinessSignalsUnavailable') || 'Core readiness signals unavailable'];
      }
      this.coreReadinessLoading = false;
    },
  };
}

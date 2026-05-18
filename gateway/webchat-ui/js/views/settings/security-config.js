export function buildSettingsSecurityConfigSection({
  api,
  showToast,
  t,
  layer2Features,
}) {
  return {
    securityConfig: {},
    toolsConfig: {},
    secForm: {
      exec_mode: 'deny',
      allowlist: [],
      approval_timeout: '',
      grace_period: '',
      allow_private: false,
      allow_local: false,
      deny_hosts: [],
      allowed_roots: [],
      deny_patterns: [],
      skip_dirs: [],
      layer2: {},
      patterns: [],
      dangerous_tools: [],
      sandbox: false,
    },

    configText: '{}',
    configSection: '',
    configSections: [],

    populateSecForm(secData, toolsData) {
      const sec = secData || {};
      const tools = toolsData || {};
      const approval = sec.exec_approval || sec.approval_policy || {};

      this.secForm.exec_mode = approval.mode || 'deny';
      this.secForm.allowlist = Array.isArray(approval.allowlist || sec.allowlist) ? [...(approval.allowlist || sec.allowlist)] : [];
      this.secForm.approval_timeout = approval.approval_timeout || '';
      this.secForm.grace_period = approval.grace_period || '';

      const net = sec.network || {};
      this.secForm.allow_private = net.allow_private === true;
      this.secForm.allow_local = net.allow_local === true;
      this.secForm.deny_hosts = Array.isArray(net.deny_hosts) ? [...net.deny_hosts] : [];

      const fs = sec.filesystem || {};
      this.secForm.allowed_roots = Array.isArray(fs.allowed_roots) ? [...fs.allowed_roots] : [];
      this.secForm.deny_patterns = Array.isArray(fs.deny_patterns) ? [...fs.deny_patterns] : [];
      this.secForm.skip_dirs = Array.isArray(fs.skip_dirs) ? [...fs.skip_dirs] : [];

      this.secForm.layer2 = {};
      const layer2 = tools.layer2 || {};
      for (const feature of layer2Features) {
        this.secForm.layer2[feature] = layer2[feature] === true;
      }

      this.secForm.patterns = Array.isArray(sec.patterns) ? sec.patterns.map(pattern => ({ ...pattern })) : [];
      this.secForm.dangerous_tools = Array.isArray(tools.dangerous) ? [...tools.dangerous] : [];
      this.secForm.sandbox = sec.sandbox === true || sec.sandbox === 'enabled';
    },

    buildSecurityPayload() {
      const payload = { ...this.securityConfig };
      const approval = { mode: this.secForm.exec_mode };

      if (this.secForm.exec_mode === 'allowlist') {
        approval.allowlist = this.secForm.allowlist.filter(item => item.trim());
      }
      if (this.secForm.exec_mode === 'approve') {
        if (this.secForm.approval_timeout) approval.approval_timeout = Number(this.secForm.approval_timeout);
        if (this.secForm.grace_period) approval.grace_period = Number(this.secForm.grace_period);
      }
      payload.exec_approval = approval;

      payload.network = {
        allow_private: this.secForm.allow_private,
        allow_local: this.secForm.allow_local,
        deny_hosts: this.secForm.deny_hosts.filter(item => item.trim()),
      };

      payload.filesystem = {
        allowed_roots: this.secForm.allowed_roots.filter(item => item.trim()),
        deny_patterns: this.secForm.deny_patterns.filter(item => item.trim()),
        skip_dirs: this.secForm.skip_dirs.filter(item => item.trim()),
      };

      payload.patterns = this.secForm.patterns.filter(pattern => pattern.name && pattern.pattern);
      payload.sandbox = this.secForm.sandbox;
      return payload;
    },

    buildToolsPayload() {
      const payload = { ...this.toolsConfig };
      const layer2 = {};
      for (const feature of layer2Features) {
        layer2[feature] = this.secForm.layer2[feature] === true;
      }
      payload.layer2 = layer2;
      payload.dangerous = this.secForm.dangerous_tools.filter(item => item.trim());
      return payload;
    },

    async saveSecurityConfig() {
      try {
        const payload = this.buildSecurityPayload();
        await api.put('/operator/config/security', payload);
        showToast(t('settingsSecuritySaved') || 'Security settings saved', 'success');
        await this.loadSecurity();
      } catch (_) {}
    },

    async saveToolsConfig() {
      try {
        const payload = this.buildToolsPayload();
        await api.put('/operator/config/tools', payload);
        showToast(t('settingsToolsSaved') || 'Tools settings saved', 'success');
        await this.loadToolsConfig();
      } catch (_) {}
    },

    async previewConfig() {
      try {
        const result = await api.post('/operator/config/preview', { changed_paths: [] });
        if (result) showToast('Preview: ' + JSON.stringify(result).substring(0, 200), 'info');
      } catch (_) {}
    },

    async saveConfigSection() {
      if (!this.configSection) {
        showToast(t('settingsSelectSectionFirst') || 'Select a section first', 'warning');
        return;
      }
      let parsed;
      try {
        parsed = JSON.parse(this.configText);
      } catch (_) {
        showToast(t('settingsInvalidJson') || 'Invalid JSON', 'error');
        return;
      }
      const sectionData = parsed[this.configSection] || parsed;
      try {
        await api.put('/operator/config/' + encodeURIComponent(this.configSection), sectionData);
        showToast((t('settingsSectionSaved') || 'Section saved') + ': ' + this.configSection, 'success');
        await this.loadConfig();
      } catch (_) {}
    },

    async loadSecurity() {
      try {
        this.securityConfig = await api.get('/operator/config/security');
      } catch (_) {
        this.securityConfig = {};
      }
      await this.loadToolsConfig();
      this.populateSecForm(this.securityConfig, this.toolsConfig);
    },

    async loadToolsConfig() {
      try {
        this.toolsConfig = await api.get('/operator/config/tools');
      } catch (_) {
        this.toolsConfig = {};
      }
    },

    async loadConfig() {
      try {
        const fullConfig = await api.get('/operator/config');
        this.configText = JSON.stringify(fullConfig, null, 2);
        this.configSections = typeof fullConfig === 'object' && fullConfig !== null
          ? Object.keys(fullConfig)
          : [];
      } catch (_) {
        this.configText = '{}';
        this.configSections = [];
      }
    },
  };
}

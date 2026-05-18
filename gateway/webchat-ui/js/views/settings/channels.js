export function buildSettingsChannelsSection({
  api,
  showToast,
  t,
  normalizeChannelTypeKey,
  defaultChannelFormConfig,
  effectiveChannelTypeOptions,
  effectiveChannelSchema,
  channelFieldDisplayValue,
  channelFieldRowCount,
  channelFieldPayloadValue,
}) {
  return {
    channelsList: [],
    channelsHealth: [],
    channelsMatrix: [],
    channelsThreadBindings: [],
    channelsError: '',
    showChannelForm: false,
    editingChannel: null,
    chFormName: '',
    chFormType: '',
    chFormConfig: {},
    chFormEnabled: true,

    get channelTypeOptions() {
      return effectiveChannelTypeOptions(this.setupCatalog);
    },

    get currentChannelFields() {
      const schema = effectiveChannelSchema(this.chFormType, this.setupCatalog);
      return schema && Array.isArray(schema.fields) ? schema.fields : [];
    },

    get mergedChannels() {
      const healthMap = {};
      for (const health of this.channelsHealth) {
        const name = health.name || health.channel;
        if (name) healthMap[name] = health;
      }
      const matrixMap = {};
      for (const item of this.channelsMatrix) {
        if (item && item.name) matrixMap[item.name] = item;
      }

      if (this.channelsList.length > 0) {
        return this.channelsList.map(channel => {
          const health = healthMap[channel.name] || {};
          const matrix = matrixMap[channel.name] || {};
          return {
            ...channel,
            type: channel.config ? (channel.config.type || channel.name) : channel.name,
            health_status: health.status || health.health || 'unknown',
            capability_matrix: matrix.capabilities || {},
          };
        });
      }

      return this.channelsHealth.map(health => ({
        name: health.name || health.channel || '-',
        type: health.type || '-',
        enabled: true,
        health_status: health.status || health.health || 'unknown',
        source: health.source || 'yaml',
        capability_matrix: (matrixMap[health.name || health.channel || ''] || {}).capabilities || {},
      }));
    },

    channelSchemaLabel(type) {
      const schema = effectiveChannelSchema(type, this.setupCatalog);
      return (schema && schema.label) || normalizeChannelTypeKey(type) || type;
    },

    channelFieldValue(field) {
      const hasValue = this.chFormConfig && Object.prototype.hasOwnProperty.call(this.chFormConfig, field.key);
      return channelFieldDisplayValue(field, hasValue ? this.chFormConfig[field.key] : '');
    },

    channelFieldRows(field) {
      return channelFieldRowCount(field);
    },

    toggleChannelForm() {
      if (this.showChannelForm) {
        this.cancelChannelForm();
      } else {
        this.showChannelForm = true;
        this.editingChannel = null;
        this.chFormName = '';
        this.chFormType = '';
        this.chFormConfig = defaultChannelFormConfig('', this.setupCatalog);
        this.chFormEnabled = true;
      }
    },

    onChannelTypeChange() {
      this.chFormType = normalizeChannelTypeKey(this.chFormType);
      if (!this.editingChannel) {
        if (this.chFormType) {
          this.chFormName = this.chFormType;
        }
        this.chFormConfig = defaultChannelFormConfig(this.chFormType, this.setupCatalog);
      }
    },

    cancelChannelForm() {
      this.showChannelForm = false;
      this.editingChannel = null;
      this.chFormName = '';
      this.chFormType = '';
      this.chFormConfig = defaultChannelFormConfig('', this.setupCatalog);
      this.chFormEnabled = true;
    },

    editChannel(channel) {
      this.editingChannel = channel;
      this.showChannelForm = false;
      this.chFormName = channel.name || '';
      this.chFormType = normalizeChannelTypeKey((channel.config && channel.config.type) ? channel.config.type : channel.type || channel.name || '');
      this.chFormConfig = defaultChannelFormConfig(this.chFormType, this.setupCatalog);
      const config = channel.config || {};
      const schema = effectiveChannelSchema(this.chFormType, this.setupCatalog);
      if (schema) {
        for (const field of schema.fields) {
          if (field.type === 'password') {
            this.chFormConfig[field.key] = '';
          } else if (Object.prototype.hasOwnProperty.call(config, field.key)) {
            this.chFormConfig[field.key] = config[field.key];
          } else {
            this.chFormConfig[field.key] = field.type === 'bool' ? false : '';
          }
        }
      }
      this.chFormEnabled = channel.enabled !== false;
    },

    async saveChannel() {
      if (!this.chFormName.trim()) {
        showToast(t('settingsChannelNameRequired') || 'Channel name is required', 'warning');
        return;
      }
      if (!this.chFormType) {
        showToast(t('settingsChannelTypeRequired') || 'Channel type is required', 'warning');
        return;
      }

      const channelType = normalizeChannelTypeKey(this.chFormType);
      const config = { type: channelType };
      const schema = effectiveChannelSchema(channelType, this.setupCatalog);
      if (schema) {
        for (const field of schema.fields) {
          const value = channelFieldPayloadValue(field, this.chFormConfig[field.key]);
          if (value !== undefined) {
            config[field.key] = value;
          }
        }
      }

      try {
        if (this.editingChannel) {
          await api.put('/operator/channels/' + encodeURIComponent(this.chFormName.trim()), {
            config,
            enabled: this.chFormEnabled,
          });
        } else {
          await api.post('/operator/channels', {
            name: this.chFormName.trim(),
            config,
            enabled: this.chFormEnabled,
          });
        }
        showToast(t('settingsSaved') || 'Saved', 'success');
        this.cancelChannelForm();
        await this.loadChannels();
      } catch (_) {}
    },

    async deleteChannel(name) {
      if (!name || !confirm((t('settingsDeleteChannelConfirm') || 'Delete channel') + ' "' + name + '"?')) return;
      try {
        await api.del('/operator/channels/' + encodeURIComponent(name));
        showToast(t('settingsChannelDeleted') || 'Channel deleted', 'success');
        await this.loadChannels();
      } catch (_) {}
    },

    async validateChannel(name) {
      try {
        const result = await api.post('/operator/channels/validate', { channel: name });
        const valid = result && result.valid === true;
        const message = (result && result.message) || (valid ? (t('settingsValid') || 'Valid') : (t('settingsInvalid') || 'Invalid'));
        showToast(message, valid ? 'success' : 'warning');
      } catch (_) {}
    },

    async testChannel(name) {
      const targetId = window.prompt(t('settingsTargetIdPrompt') || 'Target ID or conversation ID');
      if (!targetId) return;
      try {
        const result = await api.post('/operator/channels/test-message', {
          channel: name,
          target_id: targetId,
          message: 'Test message from HopClaw',
        });
        showToast((result && result.message) || (t('settingsTestMessageSent') || 'Test message sent'), 'success');
      } catch (_) {}
    },

    async loadChannels() {
      this.channelsError = '';
      try {
        const [listData, extensionsData, healthData, matrixData, bindingsData] = await Promise.all([
          api.get('/operator/channels').catch(() => null),
          api.get('/operator/extensions').catch(() => null),
          api.get('/operator/channels/health').catch(() => null),
          api.get('/operator/channels/matrix').catch(() => null),
          api.get('/operator/channels/thread-bindings').catch(() => null),
        ]);

        if (listData) {
          this.channelsList = Array.isArray(listData) ? listData : (listData.items || []);
        } else {
          this.channelsList = [];
        }

        const extensionChannels = extensionsData ? (Array.isArray(extensionsData.channels) ? extensionsData.channels : []) : [];
        if (extensionChannels.length) {
          this.channelsHealth = extensionChannels.map(item => item.health || { name: item.name || '', state: item.status || 'unknown' });
          this.channelsMatrix = extensionChannels.map(item => ({
            name: item.name || '',
            status: item.status || '',
            capabilities: item.capability_matrix || {},
          }));
        } else {
          if (healthData) {
            this.channelsHealth = Array.isArray(healthData) ? healthData : (healthData.items || healthData.channels || []);
          } else {
            this.channelsHealth = [];
          }
          this.channelsMatrix = matrixData ? (Array.isArray(matrixData) ? matrixData : (matrixData.items || [])) : [];
        }
        this.channelsThreadBindings = bindingsData ? (Array.isArray(bindingsData) ? bindingsData : (bindingsData.items || [])) : [];
      } catch (err) {
        this.channelsList = [];
        this.channelsHealth = [];
        this.channelsMatrix = [];
        this.channelsThreadBindings = [];
        this.channelsError = (err && err.message) || t('loadError');
      }
    },

    async deleteThreadBinding(item) {
      if (!item || !item.channel || !item.thread_id) return;
      if (!confirm((t('settingsDeleteBindingConfirm') || 'Delete thread binding for') + ' ' + item.channel + '/' + item.thread_id + '?')) return;
      try {
        await api.del('/operator/channels/thread-bindings/' + encodeURIComponent(item.channel) + '/' + encodeURIComponent(item.thread_id));
        showToast(t('settingsBindingDeleted') || 'Binding deleted', 'success');
        await this.loadChannels();
      } catch (_) {}
    },
  };
}

export function buildSettingsModelsSection({
  api,
  showToast,
  t,
  defaultProviderAPI,
  defaultProviderFieldValues,
  effectiveProviderAPISchema,
  normalizeProviderAPI,
  providerCapabilityBadges,
  providerFieldDisplayValue,
  providerFieldEmptyPayload,
  providerFieldIsTextarea,
  providerFieldPayloadValue,
  providerFieldRequiresExplicitMutation,
  providerFieldRows,
}) {
  return {
    modelsData: [],
    modelsError: '',
    showAddForm: false,
    editingProvider: null,
    validationResult: null,
    testResult: null,
    modelPresetID: '',
    formName: '',
    formApi: 'openai-completions',
    formProviderFields: {},
    formProviderTouched: {},
    formProviderClears: {},

    get currentProviderFields() {
      const schema = effectiveProviderAPISchema(this.formApi, this.setupCatalog);
      return schema && Array.isArray(schema.fields) ? schema.fields : [];
    },

    get currentProviderBasicFields() {
      return this.currentProviderFields.filter((field) => field && field.advanced !== true);
    },

    get currentProviderAdvancedFields() {
      return this.currentProviderFields.filter((field) => field && field.advanced === true);
    },

    resetModelForm() {
      this.modelPresetID = '';
      this.formName = '';
      this.formApi = defaultProviderAPI(this.setupCatalog);
      this.formProviderFields = defaultProviderFieldValues(this.formApi, this.setupCatalog);
      this.formProviderTouched = {};
      this.formProviderClears = {};
    },

    onModelApiTypeChange() {
      const nextAPI = normalizeProviderAPI(this.formApi) || defaultProviderAPI(this.setupCatalog);
      const previousFields = this.formProviderFields || {};
      const previousTouched = this.formProviderTouched || {};
      const previousClears = this.formProviderClears || {};
      const nextFields = {
        ...defaultProviderFieldValues(nextAPI, this.setupCatalog),
      };
      const nextTouched = {};
      const nextClears = {};
      const schema = effectiveProviderAPISchema(nextAPI, this.setupCatalog);
      for (const field of (schema && Array.isArray(schema.fields) ? schema.fields : [])) {
        if (Object.prototype.hasOwnProperty.call(previousFields, field.key)) {
          nextFields[field.key] = previousFields[field.key];
        }
        if (previousTouched[field.key] === true) nextTouched[field.key] = true;
        if (previousClears[field.key] === true) nextClears[field.key] = true;
      }
      this.formApi = nextAPI;
      this.formProviderFields = nextFields;
      this.formProviderTouched = nextTouched;
      this.formProviderClears = nextClears;
      const preset = this.currentProviderPreset();
      if (preset) {
        this.applyProviderPreset(preset, { preserveName: true, preserveAPI: true, preserveTypedValues: false });
      }
    },

    providerPresetOptions() {
      return this.setupCatalog && Array.isArray(this.setupCatalog.providers) ? this.setupCatalog.providers : [];
    },

    providerCatalogProfile(id) {
      const normalized = String(id || '').trim().toLowerCase();
      if (!normalized) return null;
      return this.providerPresetOptions().find((item) => String(item && item.id || '').trim().toLowerCase() === normalized) || null;
    },

    inferredProviderCatalogProfile() {
      const apiType = normalizeProviderAPI(this.formApi);
      const baseURL = String(this.formProviderFields && this.formProviderFields.base_url || '').trim();
      if (!apiType || !baseURL) return null;
      return this.providerPresetOptions().find((item) =>
        normalizeProviderAPI(item && item.api) === apiType &&
        String(item && item.base_url || '').trim() === baseURL
      ) || null;
    },

    currentProviderPreset() {
      return this.providerCatalogProfile(this.modelPresetID)
        || this.providerCatalogProfile(this.formName)
        || this.inferredProviderCatalogProfile();
    },

    currentProviderCatalogID() {
      const preset = this.currentProviderPreset();
      return preset ? String(preset.id || '').trim() : '';
    },

    currentProviderPresetBadges() {
      return providerCapabilityBadges(this.currentProviderPreset());
    },

    currentProviderAPIBadges() {
      return providerCapabilityBadges(effectiveProviderAPISchema(this.formApi, this.setupCatalog));
    },

    providerFieldPlaceholder(field) {
      const preset = this.currentProviderPreset();
      if (!field) return '';
      if (field.defaultValue) return field.defaultValue;
      if (preset) {
        if (field.key === 'api_key' && preset.api_key_hint) return preset.api_key_hint;
        if (field.key === 'base_url' && preset.base_url) return preset.base_url;
        if (field.key === 'default_model' && Array.isArray(preset.default_models) && preset.default_models.length > 0) {
          return preset.default_models[0];
        }
      }
      return field.placeholder || '';
    },

    providerAPIKeyEnvHint() {
      const preset = this.currentProviderPreset();
      const envVars = preset && Array.isArray(preset.env_vars) ? preset.env_vars.filter(Boolean) : [];
      if (envVars.length === 0) return '';
      return (t('settingsCommonEnvVars') || 'Common environment variables') + ': ' + envVars.join(', ');
    },

    providerFieldInputType(field) {
      const type = String(field && field.type || '').trim();
      if (type === 'password') return 'password';
      if (type === 'url') return 'url';
      return 'text';
    },

    providerFieldIsTextarea(field) {
      return providerFieldIsTextarea(field);
    },

    providerFieldRows(field) {
      return providerFieldRows(field);
    },

    formProviderFieldValue(field) {
      if (!field) return '';
      return providerFieldDisplayValue(field, this.formProviderFields ? this.formProviderFields[field.key] : '');
    },

    setModelProviderFieldValue(field, value) {
      if (!field) return;
      this.formProviderFields = { ...(this.formProviderFields || {}), [field.key]: value };
      this.formProviderTouched = { ...(this.formProviderTouched || {}), [field.key]: true };
      if (this.formProviderClears && this.formProviderClears[field.key]) {
        const nextClears = { ...this.formProviderClears };
        delete nextClears[field.key];
        this.formProviderClears = nextClears;
      }
      this.validationResult = null;
      this.testResult = null;
    },

    providerFieldClearRequested(field) {
      return !!(field && this.formProviderClears && this.formProviderClears[field.key]);
    },

    setProviderFieldClear(field, checked) {
      if (!field) return;
      const nextClears = { ...(this.formProviderClears || {}) };
      const nextTouched = { ...(this.formProviderTouched || {}) };
      const nextFields = { ...(this.formProviderFields || {}) };
      if (checked) {
        nextClears[field.key] = true;
        nextTouched[field.key] = true;
        nextFields[field.key] = '';
      } else {
        delete nextClears[field.key];
        const value = providerFieldPayloadValue(field, nextFields[field.key], { preserveEmpty: false });
        if (value === undefined && providerFieldRequiresExplicitMutation(field)) {
          delete nextTouched[field.key];
        }
      }
      this.formProviderClears = nextClears;
      this.formProviderTouched = nextTouched;
      this.formProviderFields = nextFields;
      this.validationResult = null;
      this.testResult = null;
    },

    editingProviderHasStoredField(field) {
      if (!field || !this.editingProvider) return false;
      if (field.key === 'headers') return Number(this.editingProvider.header_count || 0) > 0;
      if (field.key === 'api_keys') return Number(this.editingProvider.api_keys_count || 0) > 0;
      if (field.type === 'password') return this.editingProvider.has_key === true;
      return Boolean(String(this.editingProvider[field.key] || '').trim());
    },

    showProviderFieldClear(field) {
      return !!(this.editingProvider && field && providerFieldRequiresExplicitMutation(field));
    },

    providerFieldClearLabel(field) {
      if (!field) return t('settingsClearConfiguredValue') || 'Clear configured value';
      if (field.key === 'headers') return t('settingsClearExtraHeaders') || 'Clear existing extra headers';
      if (field.key === 'api_keys') return t('settingsClearKeyPool') || 'Clear existing key pool';
      return (t('settingsClearExisting') || 'Clear existing') + ' ' + field.label;
    },

    providerFieldRetentionHint(field) {
      if (!field || !this.editingProvider) return '';
      if (this.providerFieldClearRequested(field)) {
        return t('settingsFieldClearOnSave') || 'This field will be cleared when you save the provider.';
      }
      if (field.key === 'headers' && Number(this.editingProvider.header_count || 0) > 0) {
        return t('settingsHeadersRetentionHint') || 'Existing extra headers are configured. Leave blank to keep them, enter new lines to replace them, or use clear to remove them.';
      }
      if (field.key === 'api_keys' && Number(this.editingProvider.api_keys_count || 0) > 0) {
        return t('settingsKeysRetentionHint') || 'Existing fallback keys are configured. Leave blank to keep them, enter new lines to replace them, or use clear to remove them.';
      }
      if (providerFieldRequiresExplicitMutation(field) && this.editingProviderHasStoredField(field)) {
        return t('settingsFieldRetentionHint') || 'Leave blank to keep the existing value. Enter a new value to replace it, or use clear to remove it.';
      }
      return '';
    },

    providerAdvancedFieldsExpanded() {
      return this.currentProviderAdvancedFields.some((field) => {
        if (this.providerFieldClearRequested(field)) return true;
        if (this.editingProviderHasStoredField(field)) return true;
        return providerFieldPayloadValue(field, this.formProviderFields && this.formProviderFields[field.key], { preserveEmpty: false }) !== undefined;
      });
    },

    applyProviderPreset(preset, options = {}) {
      if (!preset) return;
      const preserveName = options.preserveName === true;
      const preserveAPI = options.preserveAPI === true;
      const preserveTypedValues = options.preserveTypedValues !== false;
      const nextName = preserveName && this.formName ? this.formName : (preset.id || this.formName);
      const nextAPI = preserveAPI
        ? (normalizeProviderAPI(this.formApi) || normalizeProviderAPI(preset.api) || defaultProviderAPI(this.setupCatalog))
        : (normalizeProviderAPI(preset.api) || normalizeProviderAPI(this.formApi) || defaultProviderAPI(this.setupCatalog));
      const nextFields = preserveTypedValues
        ? { ...defaultProviderFieldValues(nextAPI, this.setupCatalog), ...this.formProviderFields }
        : defaultProviderFieldValues(nextAPI, this.setupCatalog);
      const nextTouched = preserveTypedValues ? { ...(this.formProviderTouched || {}) } : {};
      const nextClears = preserveTypedValues ? { ...(this.formProviderClears || {}) } : {};
      const schema = effectiveProviderAPISchema(nextAPI, this.setupCatalog);

      this.formName = nextName;
      this.formApi = nextAPI;
      for (const field of (schema && Array.isArray(schema.fields) ? schema.fields : [])) {
        if (field.key === 'base_url' && preset.base_url && !nextFields[field.key]) {
          nextFields[field.key] = preset.base_url;
        }
        if (field.key === 'default_model' && Array.isArray(preset.default_models) && preset.default_models.length > 0 && !nextFields[field.key]) {
          nextFields[field.key] = preset.default_models[0];
        }
      }
      this.formProviderFields = nextFields;
      this.formProviderTouched = nextTouched;
      this.formProviderClears = nextClears;
    },

    onModelPresetChange() {
      const preset = this.providerCatalogProfile(this.modelPresetID);
      if (!preset) return;
      this.applyProviderPreset(preset, { preserveTypedValues: false });
    },

    editProvider(provider) {
      if (provider && provider.mutable === false) return;
      this.editingProvider = provider;
      this.showAddForm = false;
      this.validationResult = null;
      this.testResult = null;
      this.modelPresetID = '';
      this.formName = provider.name || '';
      this.formApi = normalizeProviderAPI(provider.api || provider.api_type) || defaultProviderAPI(this.setupCatalog);
      this.formProviderFields = defaultProviderFieldValues(this.formApi, this.setupCatalog);
      this.formProviderTouched = {};
      this.formProviderClears = {};
      const schema = effectiveProviderAPISchema(this.formApi, this.setupCatalog);
      for (const field of schema.fields) {
        if (providerFieldRequiresExplicitMutation(field)) {
          this.formProviderFields[field.key] = '';
        } else {
          this.formProviderFields[field.key] = providerFieldDisplayValue(field, provider[field.key]);
        }
      }
    },

    appendProviderFieldPayload(body) {
      const schema = effectiveProviderAPISchema(body.api, this.setupCatalog);
      for (const field of (schema && Array.isArray(schema.fields) ? schema.fields : [])) {
        const clearRequested = this.providerFieldClearRequested(field);
        const touched = !!(this.formProviderTouched && this.formProviderTouched[field.key]);
        if (this.editingProvider && providerFieldRequiresExplicitMutation(field) && !clearRequested && !touched) {
          continue;
        }
        if (clearRequested) {
          body[field.key] = providerFieldEmptyPayload(field);
          continue;
        }
        const value = providerFieldPayloadValue(field, this.formProviderFields && this.formProviderFields[field.key], {
          preserveEmpty: !!this.editingProvider && touched,
        });
        if (value !== undefined) {
          body[field.key] = value;
        }
      }
      return body;
    },

    buildProviderMutationBody() {
      const name = String(this.formName || '').trim();
      if (!name) return null;
      const body = {
        name,
        api: normalizeProviderAPI(this.formApi) || defaultProviderAPI(this.setupCatalog),
      };
      return this.appendProviderFieldPayload(body);
    },

    buildProviderConnectionBody() {
      const provider = String(this.formName || '').trim();
      if (!provider) return null;
      const body = {
        provider,
        api: normalizeProviderAPI(this.formApi) || defaultProviderAPI(this.setupCatalog),
      };
      const catalogProvider = this.currentProviderCatalogID();
      if (catalogProvider) body.catalog_provider = catalogProvider;
      return this.appendProviderFieldPayload(body);
    },

    async saveProvider() {
      const body = this.buildProviderMutationBody();
      if (!body) return;
      try {
        if (this.editingProvider) {
          await api.put('/operator/models/' + encodeURIComponent(body.name), body);
        } else {
          await api.post('/operator/models', body);
        }
        showToast(t('settingsSaved') || 'Saved', 'success');
        this.showAddForm = false;
        this.editingProvider = null;
        await this.loadModels();
      } catch (_) {}
    },

    async deleteProvider(name) {
      if (!confirm((t('settingsDeleteProviderConfirm') || 'Delete provider') + ' "' + name + '"?')) return;
      try {
        await api.del('/operator/models/' + encodeURIComponent(name));
        showToast(t('settingsDeleted') || 'Deleted', 'success');
        await this.loadModels();
      } catch (_) {}
    },

    async validateForm() {
      const body = this.buildProviderConnectionBody();
      if (!body) return;
      try {
        this.validationResult = await api.post('/operator/models/validate', body);
      } catch (_) {
        this.validationResult = { valid: false, message: t('settingsValidationFailed') || 'Validation failed' };
      }
    },

    async testForm() {
      const body = this.buildProviderConnectionBody();
      if (!body) return;
      body.message = 'Hello';
      try {
        this.testResult = await api.post('/operator/models/test-chat', body);
      } catch (err) {
        this.testResult = { ok: false, reply: 'Error: ' + err.message };
      }
    },

    async loadModels() {
      this.modelsError = '';
      try {
        const data = await api.get('/operator/models');
        this.modelsData = data.providers || data.items || (Array.isArray(data) ? data : []);
      } catch (err) {
        this.modelsData = [];
        this.modelsError = (err && err.message) || t('loadError');
      }
    },
  };
}

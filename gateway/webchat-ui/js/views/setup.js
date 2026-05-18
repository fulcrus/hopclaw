// ---------------------------------------------------------------------------
// Setup Wizard View - Petite Vue component
// ---------------------------------------------------------------------------

import { api, showToast } from '../api.js';
import { t } from '../i18n/index.js';
import {
  defaultProviderAPI,
  defaultProviderFieldValues,
  effectiveProviderAPISchema,
  normalizeProviderAPI,
  providerCapabilityBadges,
  providerFieldDisplayValue,
  providerFieldIsTextarea as isProviderFieldTextarea,
  providerFieldPayloadValue,
  providerFieldRows as providerFieldRowCount,
} from '../provider-api-forms.js';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const STEP_COUNT = 3;
const DEFAULT_PROVIDER_ICON = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="32" height="32"><rect x="3" y="3" width="18" height="18" rx="4"/><path d="M8 12h8"/><path d="M12 8v8"/></svg>';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function esc(s) {
  if (!s) return '';
  const div = document.createElement('div');
  div.appendChild(document.createTextNode(s));
  return div.innerHTML;
}

function providerColorSeed(seed) {
  let hash = 0;
  const input = String(seed || 'provider');
  for (let i = 0; i < input.length; i++) {
    hash = ((hash << 5) - hash) + input.charCodeAt(i);
    hash |= 0;
  }
  return Math.abs(hash);
}

function providerInitials(label) {
  const parts = String(label || '')
    .trim()
    .split(/[\s/._-]+/)
    .map((part) => part.replace(/[^a-z0-9]/gi, ''))
    .filter(Boolean);
  if (parts.length === 0) return 'AI';
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[1][0]).toUpperCase();
}

function providerCardLogo(profile) {
  const label = profile && (profile.display_name || profile.displayName || profile.id) ? (profile.display_name || profile.displayName || profile.id) : 'Provider';
  const initials = providerInitials(label);
  if (!initials) return DEFAULT_PROVIDER_ICON;
  const hue = providerColorSeed(profile && profile.id) % 360;
  const bg = `hsl(${hue} 85% 95%)`;
  const fg = `hsl(${hue} 64% 34%)`;
  return `<svg viewBox="0 0 64 64" fill="none" width="32" height="32" aria-hidden="true">
    <rect x="2" y="2" width="60" height="60" rx="18" fill="${bg}"/>
    <text x="32" y="38" text-anchor="middle" font-size="24" font-weight="700" font-family="ui-sans-serif, system-ui, sans-serif" fill="${fg}">${esc(initials)}</text>
  </svg>`;
}

function providerCardFromCatalog(profile) {
  if (!profile || !profile.id) return null;
  return {
    id: profile.id,
    name: profile.display_name || profile.id,
    description: profile.description || '',
    api: profile.api || 'openai-completions',
    baseUrl: profile.base_url || '',
    defaultModels: Array.isArray(profile.default_models) ? profile.default_models.slice() : [],
    envVars: Array.isArray(profile.env_vars) ? profile.env_vars.slice() : [],
    apiKeyHint: profile.api_key_hint || '',
    capability_matrix: profile.capability_matrix || {},
    svg: providerCardLogo(profile),
  };
}

// ---------------------------------------------------------------------------
// Setup View component
// ---------------------------------------------------------------------------

export function SetupView() {
  const $template = `
    <div class="hc-setup">
      <!-- Progress bar -->
      <div class="hc-setup-progress">
        <div v-for="i in 3" :key="i" style="display:contents">
          <div class="hc-setup-step" :class="{ done: i < step, active: i === step }">
            <div class="hc-setup-step-num">{{ i }}</div>
            <span>{{ getStepLabel(i) }}</span>
          </div>
          <div v-if="i < 3" class="hc-setup-step-line" :class="{ done: i < step }"></div>
        </div>
      </div>

      <!-- Step 1: Choose Provider -->
      <div v-if="step === 1" class="hc-setup-body" data-testid="setup-step-1">
        <h2 class="hc-setup-title">{{ t('setupChooseProvider') || 'Choose Your AI Provider' }}</h2>
        <p class="hc-setup-subtitle">{{ t('setupChooseDesc') || 'Select an LLM provider to get started.' }}</p>

        <div v-if="configuredProviders().length > 0" class="hc-setup-existing">
          <div class="hc-setup-existing-title">{{ t('setupExistingTitle') || 'Current configuration' }}</div>
          <div class="hc-setup-existing-copy">{{ t('setupExistingCopy') || 'HopClaw already has model providers configured. You can keep using the current setup or switch to a new provider below.' }}</div>
          <div class="hc-setup-existing-tags">
            <span v-for="name in configuredProviders()" :key="'cfg-'+name" class="hc-setup-detect-tag">{{ name }}</span>
          </div>
          <div class="hc-setup-existing-actions">
            <a href="#/assistant" class="hc-btn hc-btn-sm hc-btn-secondary">{{ t('setupUseCurrentSetup') || 'Use current setup' }}</a>
            <a href="#/settings/models" class="hc-btn hc-btn-sm hc-btn-ghost">{{ t('setupManageModels') || 'Manage models' }}</a>
          </div>
        </div>

        <div v-if="setupStatus && setupStatus.detected_providers && setupStatus.detected_providers.length > 0" class="hc-setup-detect">
          <div class="hc-setup-detect-title">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>
            {{ t('setupDetected') || 'Detected from environment' }}
          </div>
          <span v-for="dp in setupStatus.detected_providers" class="hc-setup-detect-tag">{{ dp.name || dp }}</span>
        </div>

        <div v-if="setupCatalogError" class="hc-setup-result" style="margin-bottom:16px">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><circle cx="12" cy="12" r="10"/><path d="M12 8v5"/><circle cx="12" cy="16" r="1"/></svg>
          {{ setupCatalogError }}
        </div>

        <div class="hc-setup-providers">
          <button
               v-for="p in providers" :key="p.id"
               type="button"
               :data-testid="'setup-provider-card-' + p.id"
               class="hc-setup-provider-card"
               :class="{ selected: provider && provider.id === p.id, detected: isProviderDetected(p) }"
               @click="selectProvider(p)">
            <div class="hc-setup-provider-logo" v-html="p.svg"></div>
            <div class="hc-setup-provider-name">{{ p.name }}</div>
            <div class="hc-setup-provider-desc">{{ p.description }}</div>
            <div v-if="providerFeatureBadges(p).length" class="hc-result-chip-row" style="margin-top:8px">
              <span v-for="badge in providerFeatureBadges(p)" :key="p.id + '-cap-' + badge" class="hc-result-chip">{{ badge }}</span>
            </div>
            <div v-if="isProviderDetected(p)" class="hc-setup-provider-badge">{{ t('setupDetectedBadge') || 'Detected' }}</div>
            <div v-if="provider && provider.id === p.id" class="hc-setup-provider-selected-mark" aria-hidden="true">Selected</div>
          </button>
        </div>

        <div class="hc-setup-actions">
          <button class="hc-btn hc-btn-ghost" @click="skip()">{{ t('setupSkip') || 'Skip to Chat' }}</button>
          <button class="hc-btn hc-btn-primary" data-testid="setup-next-step" @click="nextStep()" :disabled="!provider">{{ t('setupNext') || 'Next' }}</button>
        </div>
      </div>

      <!-- Step 2: Configure -->
      <div v-if="step === 2" class="hc-setup-body" data-testid="setup-step-2">
        <h2 class="hc-setup-title">{{ t('setupConfigure') || 'Configure ' + (provider ? provider.name : '') }}</h2>
        <p class="hc-setup-subtitle">{{ t('setupConfigureDesc') || 'Enter your provider credentials and validate the live connection.' }}</p>
        <div v-if="currentProviderAPIBadges().length" class="hc-result-chip-row" style="margin-bottom:12px">
          <span v-for="badge in currentProviderAPIBadges()" :key="'setup-api-cap-' + badge" class="hc-result-chip">{{ badge }}</span>
        </div>

        <div class="hc-setup-form">
          <div v-for="field in currentProviderBasicFields" :key="field.key" class="hc-form-group">
              <label class="hc-form-label">{{ field.label }}<span v-if="field.required"> *</span></label>
              <select
                v-if="field.key === 'default_model' && availableModels.length > 0"
                class="hc-form-select"
                :data-testid="'setup-provider-field-' + field.key"
                :value="providerFormValue(field.key)"
                @change="setProviderFormValue(field.key, $event.target.value)"
              >
                <option value="">{{ t('setupSelectModel') || 'Select a model...' }}</option>
                <option v-for="m in availableModels" :key="modelId(m)" :value="modelId(m)">{{ modelId(m) }}</option>
              </select>
              <textarea
                v-else-if="providerFieldIsTextarea(field)"
                class="hc-form-input"
                :data-testid="'setup-provider-field-' + field.key"
                :rows="providerFieldRows(field)"
                :placeholder="providerFieldPlaceholder(field)"
                :value="providerFormValue(field.key)"
                @input="setProviderFormValue(field.key, $event.target.value)"
                spellcheck="false"
                style="resize:vertical;font-family:var(--font-mono)"
              ></textarea>
              <input
                v-else
                class="hc-form-input"
                :data-testid="'setup-provider-field-' + field.key"
                :type="providerFieldInputType(field)"
                :placeholder="providerFieldPlaceholder(field)"
                :value="providerFormValue(field.key)"
                @input="setProviderFormValue(field.key, $event.target.value)"
              />
              <p v-if="field.description" class="hc-form-hint">{{ field.description }}</p>
              <p v-if="field.key === 'api_key' && providerAPIKeyEnvHint()" class="hc-form-hint">{{ providerAPIKeyEnvHint() }}</p>
              <p v-if="field.key === 'default_model' && availableModels.length === 0" class="hc-form-hint">{{ t('setupValidateFirst') || 'Validate your provider to see available models.' }}</p>
          </div>

          <details v-if="currentProviderAdvancedFields.length" :open="providerAdvancedFieldsExpanded()" style="margin-bottom:16px;border:1px solid var(--border-light);border-radius:var(--radius-sm);padding:12px;background:var(--surface)">
            <summary style="cursor:pointer;font-weight:600;color:var(--ink2)">{{ t('setupAdvancedOptions') || 'Advanced Connection Options' }}</summary>
            <div style="margin-top:12px">
              <div v-for="field in currentProviderAdvancedFields" :key="'advanced-' + field.key" class="hc-form-group">
                  <label class="hc-form-label">{{ field.label }}<span v-if="field.required"> *</span></label>
                  <textarea
                    v-if="providerFieldIsTextarea(field)"
                    class="hc-form-input"
                    :data-testid="'setup-provider-field-' + field.key"
                    :rows="providerFieldRows(field)"
                    :placeholder="providerFieldPlaceholder(field)"
                    :value="providerFormValue(field.key)"
                    @input="setProviderFormValue(field.key, $event.target.value)"
                    spellcheck="false"
                    style="resize:vertical;font-family:var(--font-mono)"
                  ></textarea>
                  <input
                    v-else
                    class="hc-form-input"
                    :data-testid="'setup-provider-field-' + field.key"
                    :type="providerFieldInputType(field)"
                    :placeholder="providerFieldPlaceholder(field)"
                    :value="providerFormValue(field.key)"
                    @input="setProviderFormValue(field.key, $event.target.value)"
                  />
                  <p v-if="field.description" class="hc-form-hint">{{ field.description }}</p>
                  <p v-if="field.key === 'api_key' && providerAPIKeyEnvHint()" class="hc-form-hint">{{ providerAPIKeyEnvHint() }}</p>
                  <p v-if="field.key === 'default_model' && availableModels.length === 0" class="hc-form-hint">{{ t('setupValidateFirst') || 'Validate your provider to see available models.' }}</p>
              </div>
            </div>
          </details>

          <div class="hc-setup-validate">
            <button class="hc-btn hc-btn-secondary" data-testid="setup-validate-provider" @click="validateKey()" :disabled="validating || !canValidateProvider()">
              <span v-if="validating"><span class="hc-spinner"></span> {{ t('setupValidating') || 'Validating...' }}</span>
              <span v-else-if="validated"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><polyline points="20 6 9 17 4 12"/></svg> {{ t('setupValidated') || 'Validated' }}</span>
              <span v-else>{{ t('setupValidateKey') || 'Validate Provider' }}</span>
            </button>
            <button class="hc-btn hc-btn-secondary" data-testid="setup-test-message" @click="sendTestMessage()" :disabled="!validated || testing">
              <span v-if="testing"><span class="hc-spinner"></span> {{ t('setupTesting') || 'Testing...' }}</span>
              <span v-else>{{ t('setupTestMessage') || 'Send Test Message' }}</span>
            </button>
          </div>

          <div v-if="validated && !validating" class="hc-setup-result hc-setup-result-success" data-testid="setup-validation-result">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>
            {{ setupValidationMessage() }}
          </div>

          <div v-if="testResult" class="hc-setup-test-result" data-testid="setup-test-result">
            <div class="hc-setup-test-header">{{ t('setupTestResult') || 'Test Result' }}</div>
            <div class="hc-setup-test-reply">{{ testResult.reply || testResult.content || '' }}</div>
            <div class="hc-setup-test-meta">
              <span v-if="testResult.latency_ms">{{ t('setupLatency') || 'Latency' }}: {{ testResult.latency_ms }}ms</span>
              <span v-if="testResult.tokens">{{ t('setupTokens') || 'Tokens' }}: {{ testResult.tokens }}</span>
            </div>
          </div>
        </div>

        <div class="hc-setup-actions">
          <button class="hc-btn hc-btn-ghost" @click="prevStep()">{{ t('setupBack') || 'Back' }}</button>
          <button class="hc-btn hc-btn-primary" data-testid="setup-next-step" @click="nextStep()" :disabled="!validated">{{ t('setupNext') || 'Next' }}</button>
        </div>
      </div>

      <!-- Step 3: Ready -->
      <div v-if="step === 3" class="hc-setup-body hc-setup-ready" data-testid="setup-step-3">
        <div class="hc-setup-ready-icon">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" width="48" height="48"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>
        </div>
        <h2 class="hc-setup-title">{{ t('setupReady') || "You're All Set!" }}</h2>
        <p class="hc-setup-subtitle">{{ t('setupReadyDesc') || 'Your AI provider has been configured successfully.' }}</p>

        <div class="hc-setup-summary">
          <div class="hc-setup-summary-row">
            <span class="hc-setup-summary-label">{{ t('setupProvider') || 'Provider' }}</span>
            <span class="hc-setup-summary-value">{{ provider ? provider.name : '' }}</span>
          </div>
          <div v-if="selectedModel()" class="hc-setup-summary-row">
            <span class="hc-setup-summary-label">{{ t('setupModel') || 'Model' }}</span>
            <span class="hc-setup-summary-value">{{ selectedModel() }}</span>
          </div>
          <div v-if="baseURL()" class="hc-setup-summary-row">
            <span class="hc-setup-summary-label">{{ t('setupBaseUrl') || 'Base URL' }}</span>
            <span class="hc-setup-summary-value">{{ baseURL() }}</span>
          </div>
        </div>

        <div class="hc-setup-links">
          <a href="#/settings/channels" class="hc-setup-link">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><path d="M4 11a9 9 0 0 1 9 9"/><path d="M4 4a16 16 0 0 1 16 16"/><circle cx="5" cy="19" r="1"/></svg>
            {{ t('setupConnectChannel') || 'Connect a Channel' }}
          </a>
          <a href="#/settings/infrastructure" class="hc-setup-link">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
            {{ t('setupVerifyHosts') || 'Verify Browser / Desktop' }}
          </a>
        </div>

        <div class="hc-setup-actions">
          <button class="hc-btn hc-btn-primary hc-btn-lg" data-testid="setup-start-chatting" @click="finish()">{{ t('setupStartChatting') || 'Start Chatting' }}</button>
        </div>
      </div>
    </div>
  `;

  return {
    $template,
    t,

    step: 1,
    provider: null,
    setupStatus: null,
    providerForm: {},
    availableModels: [],
    validated: false,
    validating: false,
    testResult: null,
    testing: false,
    setupCatalog: null,
    setupCatalogError: '',
    providers: [],

    getStepLabel(i) {
      switch (i) {
        case 1: return t('setupStep1') || 'Choose Provider';
        case 2: return t('setupStep2') || 'Configure';
        case 3: return t('setupStep3') || 'Ready';
        default: return '';
      }
    },

    modelId(m) {
      return typeof m === 'string' ? m : m.id || m.name;
    },

    currentProviderAPI() {
      return normalizeProviderAPI(this.provider && this.provider.api) || defaultProviderAPI(this.setupCatalog);
    },

    providerFeatureBadges(provider) {
      return providerCapabilityBadges(provider);
    },

    currentProviderAPIBadges() {
      const schema = effectiveProviderAPISchema(this.currentProviderAPI(), this.setupCatalog);
      return providerCapabilityBadges(schema);
    },

    get currentProviderFields() {
      const schema = effectiveProviderAPISchema(this.currentProviderAPI(), this.setupCatalog);
      return schema && Array.isArray(schema.fields) ? schema.fields : [];
    },

    get currentProviderBasicFields() {
      return this.currentProviderFields.filter((field) => field && field.advanced !== true);
    },

    get currentProviderAdvancedFields() {
      return this.currentProviderFields.filter((field) => field && field.advanced === true);
    },

    selectedModel() {
      return this.providerFormValue('default_model');
    },

    baseURL() {
      return this.providerFormValue('base_url');
    },

    buildProviderFormDefaults(provider) {
      const apiName = normalizeProviderAPI(provider && provider.api) || defaultProviderAPI(this.setupCatalog);
      const next = defaultProviderFieldValues(apiName, this.setupCatalog);
      const fields = effectiveProviderAPISchema(apiName, this.setupCatalog).fields || [];
      for (const field of fields) {
        if (field.key === 'api_key' && provider && provider.apiKeyHint && !next[field.key]) next[field.key] = '';
        if (field.key === 'base_url' && provider && provider.baseUrl && !next[field.key]) next[field.key] = provider.baseUrl;
        if (field.key === 'default_model' && provider && Array.isArray(provider.defaultModels) && provider.defaultModels.length > 0 && !next[field.key]) {
          next[field.key] = provider.defaultModels[0];
        }
      }
      return next;
    },

    providerFormValue(key) {
      const field = this.currentProviderFields.find((item) => item && item.key === key) || null;
      const hasValue = this.providerForm && Object.prototype.hasOwnProperty.call(this.providerForm, key);
      return providerFieldDisplayValue(field, hasValue ? this.providerForm[key] : '');
    },

    providerFieldPlaceholder(field) {
      if (!field) return '';
      if (field.defaultValue) return field.defaultValue;
      if (field.key === 'api_key' && this.provider && this.provider.apiKeyHint) return this.provider.apiKeyHint;
      if (field.key === 'base_url' && this.provider && this.provider.baseUrl) return this.provider.baseUrl;
      if (field.key === 'default_model' && this.provider && Array.isArray(this.provider.defaultModels) && this.provider.defaultModels.length > 0) {
        return this.provider.defaultModels[0];
      }
      return field.placeholder || '';
    },

    providerAPIKeyEnvHint() {
      const envVars = this.provider && Array.isArray(this.provider.envVars) ? this.provider.envVars.filter(Boolean) : [];
      if (envVars.length === 0) return '';
      const template = t('setupCommonEnvVars') || 'Common environment variables: {vars}';
      return template.replace('{vars}', envVars.join(', '));
    },

    setupValidationMessage() {
      const template = t('setupKeyValidWithModels') || 'Provider is valid. {count} models available.';
      return template.replace('{count}', String(this.availableModels.length));
    },

    setupValidationToastMessage() {
      return t('setupProviderValidated') || 'Provider validated successfully';
    },

    providerFieldInputType(field) {
      const type = String(field && field.type || '').trim();
      if (type === 'password') return 'password';
      if (type === 'url') return 'url';
      return 'text';
    },

    providerFieldIsTextarea(field) {
      return isProviderFieldTextarea(field);
    },

    providerFieldRows(field) {
      return providerFieldRowCount(field);
    },

    providerAdvancedFieldsExpanded() {
      return this.currentProviderAdvancedFields.some((field) =>
        providerFieldPayloadValue(field, this.providerForm && this.providerForm[field.key], { preserveEmpty: false }) !== undefined
      );
    },

    setProviderFormValue(key, value, options = {}) {
      this.providerForm = { ...(this.providerForm || {}), [key]: value };
      if (options.resetValidation !== false) {
        this.validated = false;
        this.testResult = null;
      }
    },

    buildProviderConnectionBody() {
      if (!this.provider || !this.provider.id) return null;
      const body = {
        provider: this.provider.id,
        catalog_provider: this.provider.id,
        api: this.currentProviderAPI(),
      };
      for (const field of this.currentProviderFields) {
        const value = providerFieldPayloadValue(field, this.providerForm && this.providerForm[field.key], { preserveEmpty: false });
        if (value !== undefined) body[field.key] = value;
      }
      return body;
    },

    buildProviderSaveBody() {
      const body = this.buildProviderConnectionBody();
      if (!body) return null;
      body.name = this.provider.id;
      return body;
    },

    canValidateProvider() {
      if (!this.provider) return false;
      for (const field of this.currentProviderFields) {
        if (!field.required) continue;
        if (!String(this.providerFormValue(field.key) || '').trim()) return false;
      }
      return true;
    },

    isProviderDetected(provider) {
      if (!this.setupStatus || !this.setupStatus.detected_providers) return false;
      return this.setupStatus.detected_providers.some(dp => {
        const name = typeof dp === 'string' ? dp : dp.name || dp.id || '';
        return name.toLowerCase() === provider.id.toLowerCase() ||
               name.toLowerCase() === provider.name.toLowerCase();
      });
    },

    configuredProviders() {
      return this.setupStatus && Array.isArray(this.setupStatus.providers) ? this.setupStatus.providers : [];
    },

    syncDetectedProvider() {
      if (this.provider || !this.setupStatus || !Array.isArray(this.setupStatus.detected_providers) || this.setupStatus.detected_providers.length === 0) {
        return;
      }
      const detected = this.setupStatus.detected_providers[0];
      const detectedName = (typeof detected === 'string' ? detected : detected.id || detected.name || '').toLowerCase();
      const match = this.providers.find((provider) =>
        provider.id.toLowerCase() === detectedName || provider.name.toLowerCase() === detectedName
      );
      if (match) {
        this.selectProvider(match);
      }
    },

    syncSelectedProvider() {
      if (!this.provider || !this.provider.id) return;
      const match = this.providers.find((item) => item.id === this.provider.id);
      if (!match) return;
      const previousValues = { ...(this.providerForm || {}) };
      this.provider = match;
      this.providerForm = { ...this.buildProviderFormDefaults(match), ...previousValues };
    },

    selectProvider(p) {
      this.provider = p;
      this.validated = false;
      this.availableModels = [];
      this.testResult = null;
      this.providerForm = this.buildProviderFormDefaults(p);
    },

    nextStep() {
      if (this.step < STEP_COUNT) this.step++;
    },

    prevStep() {
      if (this.step > 1) this.step--;
    },

    skip() {
      window.location.hash = '#/chat';
    },

    async persistAgentDefaultModel() {
      const model = String(this.selectedModel() || '').trim();
      if (!this.provider || !this.provider.id || !model) return;
      await api.put('/operator/config/agent', {
        default_model: this.provider.id + '/' + model,
      }, { errorToast: false });
    },

    async finish() {
      // Save the provider to the config store before navigating to chat.
      if (this.provider) {
        const body = this.buildProviderSaveBody();
        if (!body) {
          showToast('Provider configuration is incomplete.', 'error');
          return;
        }
        try {
          await api.post('/operator/models', body, { errorToast: false });
        } catch (err) {
          // If provider already exists, try updating instead.
          if (err && err.status === 409) {
            try {
              await api.put('/operator/models/' + encodeURIComponent(this.provider.id), body, { errorToast: false });
            } catch (updateErr) {
              showToast((updateErr && updateErr.message) || 'Failed to update provider configuration.', 'error');
              return;
            }
          } else {
            showToast((err && err.message) || 'Failed to save provider configuration.', 'error');
            return;
          }
        }
        try {
          await this.persistAgentDefaultModel();
        } catch (err) {
          showToast((err && err.message) || 'Failed to save the default agent model.', 'error');
          return;
        }
      }
      window.location.hash = '#/chat';
    },

    async validateKey() {
      if (!this.canValidateProvider() || this.validating) return;
      this.validating = true;
      this.validated = false;
      this.availableModels = [];

      try {
        const body = this.buildProviderConnectionBody();
        const result = await api.post('/operator/models/validate', body);
        if (!result || result.valid !== true) {
          this.validated = false;
          this.availableModels = [];
          showToast((result && result.message) || 'Provider validation failed', 'error');
          return;
        }
        this.validated = true;
        this.availableModels = result.models || result.available_models || [];
        if (this.availableModels.length > 0) {
          const first = this.availableModels[0];
          const currentModel = this.selectedModel();
          const hasCurrentModel = currentModel && this.availableModels.some((item) => this.modelId(item) === currentModel);
          if (!hasCurrentModel) {
            this.setProviderFormValue('default_model', typeof first === 'string' ? first : first.id || first.name || '', { resetValidation: false });
          }
        }
        showToast(this.setupValidationToastMessage(), 'success');
      } catch (_) {
        this.validated = false;
        this.availableModels = [];
      } finally {
        this.validating = false;
      }
    },

    async sendTestMessage() {
      if (!this.validated || this.testing) return;
      this.testing = true;
      this.testResult = null;

      try {
        const body = this.buildProviderConnectionBody();
        body.message = 'Hello! Please respond with a brief greeting.';
        this.testResult = await api.post('/operator/models/test-chat', body);
        if (!this.testResult || this.testResult.ok !== true) {
          showToast((this.testResult && this.testResult.reply) || 'Test message failed', 'error');
          return;
        }
      } catch (err) {
        this.testResult = { reply: 'Error: ' + err.message };
      } finally {
        this.testing = false;
      }
    },

    async loadSetupCatalog() {
      try {
        this.setupCatalog = await api.get('/operator/setup/catalog');
        const items = this.setupCatalog && Array.isArray(this.setupCatalog.providers) ? this.setupCatalog.providers : [];
        this.providers = items.map(providerCardFromCatalog).filter(Boolean);
        this.setupCatalogError = this.providers.length > 0 ? '' : 'No provider presets are available from the setup catalog.';
      } catch (err) {
        this.setupCatalog = null;
        this.providers = [];
        this.setupCatalogError = (err && err.message) || 'Unable to load the setup catalog.';
      }
      this.syncSelectedProvider();
      this.syncDetectedProvider();
    },

    async loadSetupStatus() {
      try {
        this.setupStatus = await api.get('/operator/setup/status');
        this.syncDetectedProvider();
      } catch (_) {
        this.setupStatus = null;
      }
    },

    mounted() {
      this.providers = [];
      this.loadSetupCatalog();
      this.loadSetupStatus();
    },

    unmounted() {},
  };
}

const LEGACY_PROVIDER_API_OPTIONS = [
  'openai-completions',
  'openai-responses',
  'anthropic-messages',
  'google-generative-ai',
  'bedrock-converse',
  'github-copilot',
  'ollama',
];

const LEGACY_PROVIDER_API_SCHEMAS = {
  'openai-completions': {
    id: 'openai-completions',
    displayName: 'OpenAI-compatible Completions',
    fields: [
      { key: 'base_url', label: 'Base URL', type: 'url', placeholder: 'https://api.openai.com/v1' },
      { key: 'api_key', label: 'API Key', type: 'password', required: true },
      { key: 'api_keys', label: 'API Key Pool', type: 'string_list', advanced: true, description: 'Optional fallback keys. Enter one key per line to replace the active key pool.' },
      { key: 'headers', label: 'Extra Headers', type: 'string_map', advanced: true, description: 'Optional HTTP headers. Enter one header per line using Header-Name: value.' },
      { key: 'timeout', label: 'Request Timeout', type: 'duration', advanced: true, description: 'Optional Go duration such as 30s, 90s, or 2m.' },
      { key: 'default_model', label: 'Default Model', type: 'text', placeholder: 'gpt-4o' },
    ],
  },
  'openai-responses': {
    id: 'openai-responses',
    displayName: 'OpenAI Responses API',
    fields: [
      { key: 'base_url', label: 'Base URL', type: 'url', placeholder: 'https://api.openai.com/v1' },
      { key: 'api_key', label: 'API Key', type: 'password', required: true },
      { key: 'api_keys', label: 'API Key Pool', type: 'string_list', advanced: true, description: 'Optional fallback keys. Enter one key per line to replace the active key pool.' },
      { key: 'headers', label: 'Extra Headers', type: 'string_map', advanced: true, description: 'Optional HTTP headers. Enter one header per line using Header-Name: value.' },
      { key: 'timeout', label: 'Request Timeout', type: 'duration', advanced: true, description: 'Optional Go duration such as 30s, 90s, or 2m.' },
      { key: 'default_model', label: 'Default Model', type: 'text', placeholder: 'gpt-4.1' },
    ],
  },
  'anthropic-messages': {
    id: 'anthropic-messages',
    displayName: 'Anthropic Messages',
    fields: [
      { key: 'base_url', label: 'Base URL', type: 'url', placeholder: 'https://api.anthropic.com' },
      { key: 'api_key', label: 'API Key', type: 'password', required: true },
      { key: 'api_keys', label: 'API Key Pool', type: 'string_list', advanced: true, description: 'Optional fallback keys. Enter one key per line to replace the active key pool.' },
      { key: 'headers', label: 'Extra Headers', type: 'string_map', advanced: true, description: 'Optional HTTP headers. Enter one header per line using Header-Name: value.' },
      { key: 'timeout', label: 'Request Timeout', type: 'duration', advanced: true, description: 'Optional Go duration such as 30s, 90s, or 2m.' },
      { key: 'default_model', label: 'Default Model', type: 'text', placeholder: 'claude-sonnet-4-20250514' },
    ],
  },
  'google-generative-ai': {
    id: 'google-generative-ai',
    displayName: 'Google Generative AI',
    fields: [
      { key: 'base_url', label: 'Base URL', type: 'url', placeholder: 'https://generativelanguage.googleapis.com' },
      { key: 'api_key', label: 'API Key', type: 'password', required: true },
      { key: 'api_keys', label: 'API Key Pool', type: 'string_list', advanced: true, description: 'Optional fallback keys. Enter one key per line to replace the active key pool.' },
      { key: 'headers', label: 'Extra Headers', type: 'string_map', advanced: true, description: 'Optional HTTP headers. Enter one header per line using Header-Name: value.' },
      { key: 'timeout', label: 'Request Timeout', type: 'duration', advanced: true, description: 'Optional Go duration such as 30s, 90s, or 2m.' },
      { key: 'default_model', label: 'Default Model', type: 'text', placeholder: 'gemini-2.0-flash' },
    ],
  },
  'bedrock-converse': {
    id: 'bedrock-converse',
    displayName: 'AWS Bedrock Converse',
    fields: [
      { key: 'region', label: 'AWS Region', type: 'text', required: true, placeholder: 'us-east-1' },
      { key: 'access_key_id', label: 'Access Key ID', type: 'text', required: true, placeholder: 'AKIA...' },
      { key: 'secret_key', label: 'Secret Access Key', type: 'password', required: true, placeholder: 'Enter your AWS secret access key' },
      { key: 'session_token', label: 'Session Token', type: 'password', placeholder: 'Optional temporary session token' },
      { key: 'timeout', label: 'Request Timeout', type: 'duration', advanced: true, description: 'Optional Go duration such as 30s, 90s, or 2m.' },
      { key: 'default_model', label: 'Default Model', type: 'text', placeholder: 'anthropic.claude-3-5-sonnet-20241022-v2:0' },
    ],
  },
  'ollama': {
    id: 'ollama',
    displayName: 'Ollama',
    fields: [
      { key: 'base_url', label: 'Base URL', type: 'url', required: true, defaultValue: 'http://127.0.0.1:11434/v1', placeholder: 'http://127.0.0.1:11434/v1' },
      { key: 'headers', label: 'Extra Headers', type: 'string_map', advanced: true, description: 'Optional HTTP headers. Enter one header per line using Header-Name: value.' },
      { key: 'timeout', label: 'Request Timeout', type: 'duration', advanced: true, description: 'Optional Go duration such as 30s, 90s, or 2m.' },
      { key: 'default_model', label: 'Default Model', type: 'text', placeholder: 'llama3.3' },
    ],
  },
  'github-copilot': {
    id: 'github-copilot',
    displayName: 'GitHub Copilot',
    fields: [
      { key: 'api_key', label: 'GitHub Token', type: 'password', placeholder: 'ghp_... (optional when env vars are already configured)' },
      { key: 'api_keys', label: 'Token Pool', type: 'string_list', advanced: true, description: 'Optional fallback tokens. Enter one token per line to replace the active token pool.' },
      { key: 'headers', label: 'Extra Headers', type: 'string_map', advanced: true, description: 'Optional HTTP headers. Enter one header per line using Header-Name: value.' },
      { key: 'timeout', label: 'Request Timeout', type: 'duration', advanced: true, description: 'Optional Go duration such as 30s, 90s, or 2m.' },
      { key: 'default_model', label: 'Default Model', type: 'text', placeholder: 'gpt-4o' },
    ],
  },
};

const MULTILINE_PROVIDER_FIELD_TYPES = new Set(['string_list', 'string_map']);

function defaultProviderFieldType(key, secret) {
  if (secret) return 'password';
  if (key === 'base_url') return 'url';
  return 'text';
}

function normalizeProviderFieldType(type, key, secret) {
  const normalized = String(type || '').trim().toLowerCase();
  switch (normalized) {
    case 'url':
    case 'password':
    case 'duration':
    case 'string_list':
    case 'string_map':
    case 'text':
      return normalized;
    default:
      return defaultProviderFieldType(key, secret);
  }
}

function splitProviderFieldLines(raw) {
  return String(raw || '')
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

function parseProviderFieldMap(raw) {
  if (raw && typeof raw === 'object' && !Array.isArray(raw)) {
    const result = {};
    for (const [key, value] of Object.entries(raw)) {
      const name = String(key || '').trim();
      if (!name) continue;
      result[name] = String(value || '').trim();
    }
    return result;
  }
  const result = {};
  for (const line of splitProviderFieldLines(raw)) {
    const index = line.indexOf(':');
    if (index < 0) {
      result[line] = '';
      continue;
    }
    const name = line.slice(0, index).trim();
    if (!name) continue;
    result[name] = line.slice(index + 1).trim();
  }
  return result;
}

export function normalizeProviderAPI(api) {
  const normalized = String(api || '').trim().toLowerCase();
  switch (normalized) {
    case '':
      return '';
    case 'custom':
    case 'openai':
      return 'openai-completions';
    case 'responses':
      return 'openai-responses';
    case 'anthropic':
      return 'anthropic-messages';
    case 'google':
    case 'gemini':
      return 'google-generative-ai';
    case 'bedrock':
    case 'amazon-bedrock':
      return 'bedrock-converse';
    case 'copilot':
      return 'github-copilot';
    default:
      return normalized;
  }
}

function capabilityMatrix(source) {
  if (!source || typeof source !== 'object') return {};
  return source.capability_matrix || source.capabilityMatrix || {};
}

function providerFieldFromCatalog(field) {
  const key = String(field && field.id || '').trim();
  if (!key) return null;
  const secret = field.secret === true;
  return {
    key,
    label: field.label || key,
    type: normalizeProviderFieldType(field.type, key, secret),
    required: field.required === true,
    placeholder: field.placeholder || '',
    defaultValue: field.default_value || '',
    description: field.description || '',
    secret,
    advanced: field.advanced === true,
  };
}

function legacyProviderAPISchema(api) {
  const normalized = normalizeProviderAPI(api);
  return LEGACY_PROVIDER_API_SCHEMAS[normalized] || LEGACY_PROVIDER_API_SCHEMAS['openai-completions'];
}

export function providerAPIProfile(setupCatalog, api) {
  const normalized = normalizeProviderAPI(api);
  if (!normalized) return null;
  const items = setupCatalog && Array.isArray(setupCatalog.provider_apis) ? setupCatalog.provider_apis : [];
  return items.find((item) => normalizeProviderAPI(item && item.id) === normalized) || null;
}

export function effectiveProviderAPIOptions(setupCatalog) {
  const items = setupCatalog && Array.isArray(setupCatalog.provider_apis) ? setupCatalog.provider_apis : [];
  if (items.length > 0) {
    const deduped = [];
    for (const item of items) {
      const id = normalizeProviderAPI(item && item.id);
      if (id && !deduped.includes(id)) deduped.push(id);
    }
    if (deduped.length > 0) return deduped;
  }
  return LEGACY_PROVIDER_API_OPTIONS.slice();
}

export function defaultProviderAPI(setupCatalog) {
  const options = effectiveProviderAPIOptions(setupCatalog);
  if (options.includes('openai-completions')) return 'openai-completions';
  return options[0] || 'openai-completions';
}

export function effectiveProviderAPISchema(api, setupCatalog) {
  const profile = providerAPIProfile(setupCatalog, api);
  if (profile) {
    const fields = Array.isArray(profile.fields) ? profile.fields.map(providerFieldFromCatalog).filter(Boolean) : [];
    return {
      id: normalizeProviderAPI(profile.id) || profile.id || 'openai-completions',
      displayName: profile.display_name || profile.id || 'Provider API',
      description: profile.description || '',
      fields,
      capability_matrix: capabilityMatrix(profile),
    };
  }
  return legacyProviderAPISchema(api);
}

export function defaultProviderFieldValues(api, setupCatalog) {
  const schema = effectiveProviderAPISchema(api, setupCatalog);
  const values = {};
  for (const field of (schema && Array.isArray(schema.fields) ? schema.fields : [])) {
    if (field.defaultValue) values[field.key] = field.defaultValue;
  }
  return values;
}

export function providerFieldIsTextarea(field) {
  return MULTILINE_PROVIDER_FIELD_TYPES.has(String(field && field.type || '').trim());
}

export function providerFieldRows(field) {
  return String(field && field.type || '').trim() === 'string_map' ? 6 : 5;
}

export function providerFieldRequiresExplicitMutation(field) {
  const type = String(field && field.type || '').trim();
  return field && (field.secret === true || type === 'string_list' || type === 'string_map');
}

export function providerFieldEmptyPayload(field) {
  const type = String(field && field.type || '').trim();
  if (type === 'string_list') return [];
  if (type === 'string_map') return {};
  return '';
}

export function providerFieldDisplayValue(field, value) {
  const type = String(field && field.type || '').trim();
  if (type === 'string_list') {
    if (Array.isArray(value)) return value.join('\n');
    return String(value || '');
  }
  if (type === 'string_map') {
    const items = parseProviderFieldMap(value);
    return Object.keys(items)
      .sort()
      .map((key) => key + ': ' + items[key])
      .join('\n');
  }
  return value == null ? '' : String(value);
}

export function providerFieldPayloadValue(field, rawValue, options = {}) {
  const preserveEmpty = options.preserveEmpty === true;
  const type = String(field && field.type || '').trim();
  if (type === 'string_list') {
    const value = Array.isArray(rawValue)
      ? rawValue.map((item) => String(item || '').trim()).filter(Boolean)
      : splitProviderFieldLines(rawValue);
    if (value.length > 0) return value;
    return preserveEmpty ? [] : undefined;
  }
  if (type === 'string_map') {
    const value = parseProviderFieldMap(rawValue);
    if (Object.keys(value).length > 0) return value;
    return preserveEmpty ? {} : undefined;
  }
  const value = String(rawValue == null ? '' : rawValue).trim();
  if (value) return value;
  return preserveEmpty ? '' : undefined;
}

export function providerCapabilityBadges(source) {
  const caps = capabilityMatrix(source);
  const tags = [];
  if (caps.supports_tools || caps.SupportsTools) tags.push('tools');
  if (caps.supports_streaming || caps.SupportsStreaming) tags.push('stream');
  if (caps.supports_vision || caps.SupportsVision) tags.push('vision');
  if (caps.supports_reasoning || caps.SupportsReasoning) tags.push('reasoning');
  if (caps.supports_json_mode || caps.SupportsJSONMode) tags.push('json');
  if (caps.supports_embeddings || caps.SupportsEmbeddings) tags.push('embed');
  return tags;
}

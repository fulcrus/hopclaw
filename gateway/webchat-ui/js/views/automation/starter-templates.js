function normalizeStarterTemplateItem(item) {
  if (!item || typeof item !== 'object') return null;
  const id = String(item.id || '').trim();
  const kind = String(item.kind || '').trim();
  if (!id || !kind) return null;
  return {
    ...item,
    id,
    kind,
    name: String(item.name || '').trim(),
    headline: String(item.headline || '').trim(),
    summary: String(item.summary || '').trim(),
    outcome: String(item.outcome || '').trim(),
    tags: Array.isArray(item.tags) ? item.tags.filter(tag => typeof tag === 'string' && tag.trim()).map(tag => tag.trim()) : [],
    setup_hints: Array.isArray(item.setup_hints) ? item.setup_hints.filter(hint => typeof hint === 'string' && hint.trim()).map(hint => hint.trim()) : [],
  };
}

export function buildAutomationStarterTemplatesSection({
  api,
  showToast,
  t,
  store,
  isMounted,
  routeState,
  templateKindsForTab,
  templateTabForKind,
  templateDefaults,
  templateWizardFieldList,
  templateValuePresent,
  templateCanQuickCreate,
  buildHookEventOptions,
  findHookEventSpec,
  buildHookPhaseOptions,
  selectOptionValue,
  selectOptionLabel,
  selectOptionsIncludeValue,
  normalizeHookConfig,
  formatStatusText,
  defaultNewWatch,
  buildCreateSource,
}) {
  return {
    starterTemplates: [],
    starterTemplatesLoading: false,
    starterTemplatesError: false,
    starterTemplatesCollapsed: true,
    templateWizardOpen: false,
    templateWizardCreating: false,
    templateWizardTemplate: null,
    templateWizardValues: {},

    starterTemplatesForCurrentTab() {
      const kinds = templateKindsForTab(this.tab);
      if (kinds.length === 0) return [];
      return (this.starterTemplates || [])
        .map(normalizeStarterTemplateItem)
        .filter(item => item && kinds.includes(item.kind));
    },

    routeTemplateID() {
      return String(routeState().query.get('template') || '').trim();
    },

    clearRouteTemplateSelection() {
      const route = routeState();
      if (!route.hash || route.hash === route.path) return;
      const url = new URL(window.location.href);
      url.hash = route.path;
      window.history.replaceState({}, '', url.toString());
      if (store) store.route = route.path;
    },

    consumeRouteTemplateSelection() {
      const templateID = this.routeTemplateID();
      if (!templateID || !Array.isArray(this.starterTemplates) || this.starterTemplates.length === 0) return;
      const template = this.starterTemplates.find(item => String(item && item.id || '').trim() === templateID);
      if (!template) return;
      this.tab = templateTabForKind(template.kind);
      this.openStarterTemplateWizard(template);
      this.clearRouteTemplateSelection();
    },

    async loadStarterTemplates() {
      this.starterTemplatesLoading = true;
      this.starterTemplatesError = false;
      try {
        const data = await api.get('/operator/automation/templates');
        if (!isMounted()) return;
        this.starterTemplates = Array.isArray(data && data.items)
          ? data.items.map(normalizeStarterTemplateItem).filter(Boolean)
          : [];
        this.consumeRouteTemplateSelection();
      } catch (_) {
        if (isMounted()) this.starterTemplatesError = true;
      }
      if (isMounted()) this.starterTemplatesLoading = false;
    },

    templateCanQuickCreate,

    openStarterTemplateWizard(template) {
      if (!template || !template.kind) return;
      this.templateWizardTemplate = template;
      const values = Object.assign({}, templateDefaults(template));
      this.templateWizardValues = String(template.kind || '').trim() === 'hook'
        ? normalizeHookConfig(values, this.hookEventsCatalog)
        : values;
      this.templateWizardOpen = true;
    },

    closeStarterTemplateWizard() {
      this.templateWizardOpen = false;
      this.templateWizardCreating = false;
      this.templateWizardTemplate = null;
      this.templateWizardValues = {};
    },

    templateWizardCurrentTemplate() {
      return normalizeStarterTemplateItem(this.templateWizardTemplate);
    },

    templateWizardKind() {
      const template = this.templateWizardCurrentTemplate();
      return template ? template.kind : '';
    },

    templateWizardName() {
      const template = this.templateWizardCurrentTemplate();
      return template ? template.name : '';
    },

    templateWizardHeadline() {
      const template = this.templateWizardCurrentTemplate();
      if (!template) return '';
      return template.headline || template.summary || '';
    },

    templateWizardOutcome() {
      const template = this.templateWizardCurrentTemplate();
      if (!template) return '';
      return template.outcome || template.summary || '';
    },

    templateWizardSetupHints() {
      const template = this.templateWizardCurrentTemplate();
      return template && Array.isArray(template.setup_hints) ? template.setup_hints : [];
    },

    templateWizardRequiredFields() {
      return templateWizardFieldList(this.templateWizardTemplate, true);
    },

    templateWizardRecommendedFields() {
      const allFields = templateWizardFieldList(this.templateWizardTemplate, false);
      return allFields.slice(this.templateWizardRequiredFields().length);
    },

    templateWizardHookEventOptions() {
      return buildHookEventOptions(this.hookEventsCatalog, this.templateWizardValues && this.templateWizardValues.trigger);
    },

    templateWizardSelectedHookEventSpec() {
      return findHookEventSpec(this.hookEventsCatalog, this.templateWizardValues && this.templateWizardValues.trigger);
    },

    templateWizardHookPhaseOptions() {
      return buildHookPhaseOptions(this.templateWizardSelectedHookEventSpec());
    },

    templateWizardFieldOptions(field) {
      const name = String((field && field.field) || '').trim();
      if (name === 'trigger') return this.templateWizardHookEventOptions();
      if (name === 'phase') return this.templateWizardHookPhaseOptions();
      return Array.isArray(field && field.options) ? field.options : [];
    },

    templateWizardOptionValue(option) {
      return selectOptionValue(option);
    },

    templateWizardOptionLabel(option) {
      return selectOptionLabel(option);
    },

    templateWizardMissingSelectValue(field) {
      if (!field || field.type !== 'select') return false;
      return !selectOptionsIncludeValue(this.templateWizardFieldOptions(field), this.templateWizardValues[field.field]);
    },

    templateWizardCurrentOptionLabel(field) {
      const value = this.templateWizardValues[field.field];
      const name = String((field && field.field) || '').trim();
      if (name === 'trigger') return String(value || '');
      return formatStatusText(value);
    },

    updateStarterTemplateField(field, value) {
      let next = Object.assign({}, this.templateWizardValues, { [field]: value });
      if (this.templateWizardTemplate && String(this.templateWizardTemplate.kind || '').trim() === 'hook') {
        next = normalizeHookConfig(next, this.hookEventsCatalog);
      }
      this.templateWizardValues = next;
    },

    templateWizardCanCreate() {
      if (!this.templateWizardTemplate) return false;
      const requiredFields = this.templateWizardRequiredFields();
      return requiredFields.every(field => templateValuePresent(this.templateWizardValues[field.field]));
    },

    buildStarterTemplateCreateBody(template, overrideValues) {
      const values = Object.assign({}, templateDefaults(template), overrideValues || {});
      const kind = String(template && template.kind || '').trim();
      if (kind === 'cron') {
        const body = {
          name: String(values.name || '').trim(),
          schedule: { kind: 'cron', expression: String(values.schedule || '').trim() },
          payload: { content: String(values.prompt || values.name || '').trim() },
          enabled: values.enabled !== false,
        };
        if (String(values.session_key || '').trim()) body.session_key = String(values.session_key).trim();
        if (String(values.model || '').trim()) body.model = String(values.model).trim();
        return body;
      }
      if (kind === 'wakeup') {
        const body = {
          name: String(values.name || '').trim(),
          schedule: String(values.schedule || '').trim(),
          message: String(values.message || '').trim(),
          enabled: values.enabled !== false,
        };
        if (String(values.channel || '').trim()) body.channel = String(values.channel).trim();
        if (String(values.session_key || '').trim()) body.session_key = String(values.session_key).trim();
        return body;
      }
      if (kind === 'watch') {
        const watchForm = defaultNewWatch();
        watchForm.name = String(values.name || '').trim();
        watchForm.interval = String(values.interval || '5m').trim() || '5m';
        watchForm.sourceKind = String(values.source_kind || 'http').trim() || 'http';
        watchForm.sourceUrl = String(values.source_url || '').trim();
        watchForm.sourcePath = String(values.source_path || '').trim();
        watchForm.calendarQuery = String(values.calendar_query || '').trim();
        watchForm.calendarLimit = Number(values.calendar_limit || 50);
        watchForm.sourceSessionKey = String(values.source_session_key || '').trim();
        watchForm.webhookId = String(values.webhook_id || '').trim();
        watchForm.webhookSenderId = String(values.webhook_sender_id || '').trim();
        watchForm.inboxLimit = Number(values.inbox_limit || 20);
        watchForm.mailboxFolder = String(values.mailbox_folder || 'INBOX').trim() || 'INBOX';
        watchForm.mailboxQuery = String(values.mailbox_query || '').trim();
        watchForm.mailboxLimit = Number(values.mailbox_limit || 20);
        watchForm.prompt = String(values.prompt || '').trim();
        watchForm.enabled = values.enabled !== false;
        watchForm.fireOnStart = values.fire_on_start === true;
        return {
          name: watchForm.name,
          interval: watchForm.interval,
          enabled: watchForm.enabled,
          fire_on_start: watchForm.fireOnStart,
          prompt: watchForm.prompt,
          source: buildCreateSource(watchForm),
        };
      }
      if (kind === 'hook') {
        const body = {
          name: String(values.name || '').trim(),
          trigger: String(values.trigger || 'run.completed').trim() || 'run.completed',
          kind: String(values.kind || 'http').trim() || 'http',
          phase: String(values.phase || 'post').trim() || 'post',
          retry_count: Number(values.retry_count || 0),
          timeout: Number(values.timeout_sec || 30),
          async: values.async === true,
          enabled: values.enabled !== false,
        };
        if (body.kind === 'http') body.url = String(values.url || '').trim();
        if (body.kind === 'command') body.command = String(values.command || '').trim();
        if (String(values.filter || '').trim()) body.filter = String(values.filter).trim();
        if (String(values.secret || '').trim()) body.secret = String(values.secret).trim();
        return body;
      }
      return null;
    },

    starterTemplateCreateRequest(template, overrideValues) {
      const kind = String(template && template.kind || '').trim();
      const body = this.buildStarterTemplateCreateBody(template, overrideValues);
      if (!body) return null;
      if (kind === 'cron') return { url: '/operator/cron/jobs', body, responseKey: 'job' };
      if (kind === 'wakeup') return { url: '/operator/wakeup/triggers', body, responseKey: 'trigger' };
      if (kind === 'watch') return { url: '/operator/watch/items', body, responseKey: 'item' };
      if (kind === 'hook') return { url: '/operator/hooks', body, responseKey: 'hook' };
      return null;
    },

    async createStarterTemplate(template, overrideValues) {
      const request = this.starterTemplateCreateRequest(template, overrideValues);
      if (!request) return null;
      const data = await api.post(request.url, request.body, { errorToast: false });
      return (data && data[request.responseKey]) ? data[request.responseKey] : null;
    },

    createdAutomationID(kind, created) {
      if (!created) return '';
      if (kind === 'cron') return created.id || created.ID || '';
      if (kind === 'wakeup') return created.id || created.ID || '';
      if (kind === 'watch') return created.id || created.ID || '';
      if (kind === 'hook') return created.id || created.ID || '';
      return '';
    },

    async refreshAfterTemplateCreate(kind, created) {
      const targetTab = (kind === 'cron' || kind === 'wakeup') ? 'schedules' : kind;
      this.switchTab(targetTab);
      if (targetTab === 'schedules') await this.loadSchedules();
      if (targetTab === 'watch') await this.loadWatch();
      if (targetTab === 'hooks') await this.loadHooks();
      const id = this.createdAutomationID(kind, created);
      if (id) await this.openAutomationDetail(kind, id);
    },

    async quickCreateStarterTemplate(template) {
      if (!template || !templateCanQuickCreate(template) || this.templateWizardCreating) return;
      this.templateWizardCreating = true;
      this.templateWizardTemplate = template;
      try {
        const created = await this.createStarterTemplate(template, templateDefaults(template));
        await this.refreshAfterTemplateCreate(String(template.kind || '').trim(), created);
        showToast(t('automationStarterCreated') || 'Starter workflow created', 'success');
      } catch (err) {
        showToast((err && err.message) || (t('automationStarterCreateFailed') || 'Failed to create starter workflow'), 'error');
      }
      this.templateWizardCreating = false;
      this.templateWizardTemplate = null;
    },

    async createStarterTemplateFromWizard() {
      if (!this.templateWizardTemplate || !this.templateWizardCanCreate() || this.templateWizardCreating) return;
      this.templateWizardCreating = true;
      try {
        const kind = String(this.templateWizardTemplate.kind || '').trim();
        const created = await this.createStarterTemplate(this.templateWizardTemplate, this.templateWizardValues);
        this.closeStarterTemplateWizard();
        await this.refreshAfterTemplateCreate(kind, created);
        showToast(t('automationStarterCreated') || 'Starter workflow created', 'success');
      } catch (err) {
        showToast((err && err.message) || (t('automationStarterCreateFailed') || 'Failed to create starter workflow'), 'error');
        this.templateWizardCreating = false;
      }
    },

    applyStarterTemplateFromWizard() {
      const template = this.templateWizardCurrentTemplate();
      if (!template) return;
      this.applyStarterTemplate(template, this.templateWizardValues);
    },

    applyStarterTemplate(template, overrideValues) {
      if (!template || !template.kind) return;
      const kind = String(template.kind).trim();
      const values = Object.assign({}, templateDefaults(template), overrideValues || {});
      if (kind === 'cron') {
        this.tab = 'schedules';
        this.schedFormType = 'cron';
        this.schedEditingId = null;
        this.schedCronForm = {
          name: values.name || '',
          schedule: values.schedule || '',
          sessionKey: values.session_key || '',
          model: values.model || '',
          prompt: values.prompt || '',
          enabled: values.enabled !== false,
        };
        this.schedShowForm = true;
        this.closeStarterTemplateWizard();
        showToast(t('automationCronTemplateLoaded') || 'Cron template loaded into the form', 'success');
        return;
      }
      if (kind === 'wakeup') {
        this.tab = 'schedules';
        this.schedFormType = 'wakeup';
        this.schedEditingId = null;
        this.schedWakeupForm = {
          name: values.name || '',
          schedule: values.schedule || '',
          channel: values.channel || '',
          message: values.message || '',
          sessionKey: values.session_key || '',
          enabled: values.enabled !== false,
        };
        this.schedShowForm = true;
        this.closeStarterTemplateWizard();
        showToast(t('automationWakeupTemplateLoaded') || 'Wakeup template loaded into the form', 'success');
        return;
      }
      if (kind === 'watch') {
        this.tab = 'watch';
        this.watchEditingId = '';
        this.watchForm = {
          name: values.name || '',
          interval: values.interval || '5m',
          sourceKind: values.source_kind || 'http',
          sourceUrl: values.source_url || '',
          sourcePath: values.source_path || '',
          calendarQuery: values.calendar_query || '',
          calendarLimit: Number(values.calendar_limit || 50),
          sourceSessionKey: values.source_session_key || '',
          webhookId: values.webhook_id || '',
          webhookSenderId: values.webhook_sender_id || '',
          inboxLimit: Number(values.inbox_limit || 20),
          mailboxFolder: values.mailbox_folder || 'INBOX',
          mailboxQuery: values.mailbox_query || '',
          mailboxLimit: Number(values.mailbox_limit || 20),
          prompt: values.prompt || '',
          enabled: values.enabled !== false,
          fireOnStart: values.fire_on_start === true,
        };
        this.watchShowForm = true;
        this.closeStarterTemplateWizard();
        showToast(t('automationWatchTemplateLoaded') || 'Watch template loaded into the form', 'success');
        return;
      }
      if (kind === 'hook') {
        this.tab = 'hooks';
        this.hookEditingId = null;
        this.hookForm = {
          name: values.name || '',
          trigger: values.trigger || 'run.completed',
          kind: values.kind || 'http',
          phase: values.phase || 'post',
          url: values.url || '',
          command: values.command || '',
          filter: values.filter || '',
          secret: '',
          retry_count: Number(values.retry_count || 3),
          timeout_sec: Number(values.timeout_sec || 30),
          async: values.async === true,
          enabled: values.enabled !== false,
        };
        this.hookNormalizeEventSelection();
        this.hookShowForm = true;
        this.closeStarterTemplateWizard();
        showToast(t('automationHookTemplateLoaded') || 'Hook template loaded into the form', 'success');
      }
    },
  };
}

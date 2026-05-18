export function buildAutomationWatchSection({
  api,
  showToast,
  t,
  defaultPageSize,
  isMounted,
  defaultNewWatch,
  buildCreateSource,
  watchItemID,
  watchToForm,
  sourceLabel,
}) {
  return {
    watchItems: [],
    watchLoading: false,
    watchError: false,
    watchUnavailable: false,
    watchCreating: false,
    watchShowForm: false,
    watchEditingId: '',
    watchSearch: '',
    watchPage: 1,
    watchPageSize: defaultPageSize,
    watchForm: defaultNewWatch(),

    get watchFiltered() {
      const q = String(this.watchSearch || '').trim().toLowerCase();
      if (!q) return this.watchItems;
      return this.watchItems.filter(item => {
        return String(item.name || '').toLowerCase().includes(q) || String(sourceLabel(item)).toLowerCase().includes(q);
      });
    },

    get watchTotalPages() {
      return Math.max(1, Math.ceil(this.watchFiltered.length / Number(this.watchPageSize)));
    },

    get watchPaginated() {
      const start = (this.watchPage - 1) * Number(this.watchPageSize);
      return this.watchFiltered.slice(start, start + Number(this.watchPageSize));
    },

    watchUsesUrl(kind) {
      return kind === 'http' || kind === 'feed' || kind === 'browser_snapshot';
    },

    watchUrlPlaceholder(kind) {
      if (kind === 'feed') return 'https://example.com/feed.xml';
      if (kind === 'browser_snapshot') return 'https://example.com/page';
      return 'https://example.com';
    },

    watchCanCreate() {
      if (!String(this.watchForm.name || '').trim()) return false;
      if (!String(this.watchForm.interval || '').trim()) return false;
      if (this.watchUsesUrl(this.watchForm.sourceKind)) {
        return String(this.watchForm.sourceUrl || '').trim() !== '';
      }
      if (this.watchForm.sourceKind === 'file') {
        return String(this.watchForm.sourcePath || '').trim() !== '';
      }
      if (this.watchForm.sourceKind === 'mailbox') {
        return String(this.watchForm.mailboxFolder || 'INBOX').trim() !== '';
      }
      if (this.watchForm.sourceKind === 'calendar') {
        return true;
      }
      if (this.watchForm.sourceKind === 'webhook') {
        return String(this.watchForm.sourceSessionKey || '').trim() !== '' ||
          (String(this.watchForm.webhookId || '').trim() !== '' && String(this.watchForm.webhookSenderId || '').trim() !== '');
      }
      if (this.watchForm.sourceKind === 'structured_app_inbox') {
        return String(this.watchForm.sourceSessionKey || '').trim() !== '';
      }
      return false;
    },

    watchToggleForm() {
      if (this.watchShowForm) {
        this.watchCancelForm();
        return;
      }
      this.watchForm = defaultNewWatch();
      this.watchEditingId = '';
      this.watchShowForm = true;
    },

    watchCancelForm() {
      this.watchForm = defaultNewWatch();
      this.watchEditingId = '';
      this.watchShowForm = false;
    },

    watchBeginEdit(item) {
      this.watchFetchAndEdit(item);
    },

    async watchFetchAndEdit(item) {
      const id = watchItemID(item);
      if (!id) return;
      try {
        if (item && item.source) {
          this.watchForm = watchToForm(item);
        } else {
          const data = await api.get('/operator/watch/items/' + encodeURIComponent(id));
          const fullItem = data && data.item ? data.item : item;
          this.watchForm = watchToForm(fullItem);
        }
        this.watchEditingId = id;
        this.watchShowForm = true;
      } catch (_) {}
    },

    async watchSave() {
      if (this.watchEditingId) {
        await this.watchUpdate();
      } else {
        await this.watchCreate();
      }
    },

    async watchCreate() {
      if (this.watchCreating) return;
      this.watchCreating = true;
      try {
        await api.post('/operator/watch/items', {
          name: this.watchForm.name,
          interval: this.watchForm.interval,
          enabled: this.watchForm.enabled,
          fire_on_start: this.watchForm.fireOnStart,
          prompt: this.watchForm.prompt,
          source: buildCreateSource(this.watchForm),
        });
        showToast(t('watchCreated') || 'Watch created', 'success');
        this.watchCancelForm();
        await this.loadWatch();
      } catch (_) {}
      this.watchCreating = false;
    },

    async watchUpdate() {
      if (this.watchCreating || !this.watchEditingId) return;
      this.watchCreating = true;
      try {
        await api.patch('/operator/watch/items/' + encodeURIComponent(this.watchEditingId), {
          name: this.watchForm.name,
          interval: this.watchForm.interval,
          enabled: this.watchForm.enabled,
          fire_on_start: this.watchForm.fireOnStart,
          prompt: this.watchForm.prompt,
          source: buildCreateSource(this.watchForm),
        });
        showToast(t('watchUpdated') || 'Watch updated', 'success');
        this.watchCancelForm();
        await this.loadWatch();
      } catch (_) {}
      this.watchCreating = false;
    },

    async watchTrigger(id) {
      try {
        await api.post('/operator/watch/items/' + encodeURIComponent(id) + '/run');
        showToast(t('watchTriggered') || 'Watch triggered', 'success');
        await this.loadWatch();
      } catch (_) {}
    },

    async watchToggleEnabled(item) {
      try {
        await api.patch('/operator/watch/items/' + encodeURIComponent(watchItemID(item)), { enabled: !item.enabled });
        showToast(item.enabled ? (t('watchDisabled') || 'Watch disabled') : (t('watchEnabledMsg') || 'Watch enabled'), 'success');
        await this.loadWatch();
      } catch (_) {}
    },

    async watchDelete(id) {
      if (!confirm(t('watchConfirmDelete') || 'Delete this watch item?')) return;
      try {
        await api.del('/operator/watch/items/' + encodeURIComponent(id));
        showToast(t('watchDeleted') || 'Watch deleted', 'success');
        await this.loadWatch();
      } catch (_) {}
    },

    async loadWatch() {
      this.watchLoading = true;
      this.watchError = false;
      this.watchUnavailable = false;
      try {
        const data = await api.get('/operator/automation/items?kinds=watch', { background: true });
        if (!isMounted()) return;
        const items = Array.isArray(data.items) ? data.items : [];
        const services = data.services || {};
        this.watchItems = items.filter(item => item.kind === 'watch');
        this.watchUnavailable = !(services.watch && services.watch.available);
      } catch (err) {
        if (!isMounted()) return;
        const status = err && err.status;
        if (status === 404 || status === 501 || status === 503) {
          this.watchUnavailable = true;
        } else {
          this.watchError = true;
        }
      }
      if (isMounted() && this.automationDetailVisibleForTab('watch') && this.automationDetailKind && this.automationDetailID) {
        this.loadAutomationDetail();
      }
      if (isMounted()) this.watchLoading = false;
    },
  };
}

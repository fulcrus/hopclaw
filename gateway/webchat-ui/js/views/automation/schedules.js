export function buildAutomationSchedulesSection({
  api,
  showToast,
  t,
  defaultPageSize,
  isMounted,
}) {
  return {
    schedCronJobs: [],
    schedWakeupTriggers: [],
    schedLoading: false,
    schedError: false,
    schedCronNotEnabled: false,
    schedWakeupNotEnabled: false,
    schedSearch: '',
    schedTypeFilter: '',
    schedPage: 1,
    schedPageSize: defaultPageSize,
    schedShowForm: false,
    schedFormType: 'cron',
    schedSaving: false,
    schedEnabling: false,
    schedEditingId: null,
    schedCronForm: { name: '', schedule: '', sessionKey: '', model: '', prompt: '', enabled: true },
    schedWakeupForm: { name: '', schedule: '', channel: '', message: '', sessionKey: '', enabled: true },

    get schedMerged() {
      const cron = this.schedCronJobs.map(j => ({
        ...j,
        _type: 'cron',
        _uid: 'cron-' + (j.id || j.ID),
        name: j.name || j.Name || j.id || j.ID || '-',
      }));
      const wakeup = this.schedWakeupTriggers.map(w => ({
        ...w,
        _type: 'wakeup',
        _uid: 'wakeup-' + w.id,
        name: w.name || w.id || '-',
      }));
      return cron.concat(wakeup);
    },

    get schedTotalCount() {
      return this.schedCronJobs.length + this.schedWakeupTriggers.length;
    },

    get schedFiltered() {
      let items = this.schedMerged;
      if (this.schedTypeFilter) {
        items = items.filter(i => i._type === this.schedTypeFilter);
      }
      const q = (this.schedSearch || '').toLowerCase().trim();
      if (q) {
        items = items.filter(i => (i.name || '').toLowerCase().includes(q));
      }
      return items;
    },

    get schedTotalPages() {
      return Math.max(1, Math.ceil(this.schedFiltered.length / Number(this.schedPageSize)));
    },

    get schedPaginated() {
      const start = (this.schedPage - 1) * Number(this.schedPageSize);
      return this.schedFiltered.slice(start, start + Number(this.schedPageSize));
    },

    schedOpenCreate(type) {
      this.schedFormType = type;
      this.schedEditingId = null;
      if (type === 'cron') {
        this.schedCronForm = { name: '', schedule: '', sessionKey: '', model: '', prompt: '', enabled: true };
      } else {
        this.schedWakeupForm = { name: '', schedule: '', channel: '', message: '', sessionKey: '', enabled: true };
      }
      this.schedShowForm = true;
    },

    async schedCreateCron() {
      if (!this.schedCronForm.name || !this.schedCronForm.schedule) return;
      this.schedSaving = true;
      try {
        const body = {
          name: this.schedCronForm.name,
          schedule: { kind: 'cron', expression: this.schedCronForm.schedule },
          payload: { content: this.schedCronForm.prompt || this.schedCronForm.name },
          enabled: this.schedCronForm.enabled,
        };
        if (this.schedCronForm.sessionKey) body.session_key = this.schedCronForm.sessionKey;
        if (this.schedCronForm.model) body.model = this.schedCronForm.model;
        await api.post('/operator/cron/jobs', body, { errorToast: false });
        showToast(t('cronCreated') || 'Job created', 'success');
        this.schedShowForm = false;
        this.loadSchedules();
      } catch (err) {
        showToast((err && err.message) || (t('cronCreateError') || 'Failed to create job'), 'error');
      }
      this.schedSaving = false;
    },

    async schedTriggerCron(id) {
      try {
        await api.post('/operator/cron/jobs/' + encodeURIComponent(id) + '/run');
        showToast(t('cronTriggered') || 'Job triggered', 'success');
        this.loadSchedules();
      } catch (_) {}
    },

    async schedDeleteCron(id) {
      if (!confirm(t('cronConfirmDelete') || 'Delete this cron job?')) return;
      try {
        await api.del('/operator/cron/jobs/' + encodeURIComponent(id));
        showToast(t('cronDeleted') || 'Job deleted', 'success');
        this.loadSchedules();
      } catch (_) {}
    },

    schedEditWakeup(trigger) {
      this.schedFormType = 'wakeup';
      this.schedEditingId = trigger.id;
      this.schedWakeupForm = {
        name: trigger.name || '',
        schedule: trigger.schedule || '',
        channel: trigger.channel || '',
        message: trigger.message || '',
        sessionKey: trigger.session_key || '',
        enabled: trigger.enabled !== false,
      };
      this.schedShowForm = true;
    },

    async schedSaveWakeup() {
      if (!this.schedWakeupForm.name || !this.schedWakeupForm.schedule || !this.schedWakeupForm.message) return;
      this.schedSaving = true;
      try {
        const body = {
          name: this.schedWakeupForm.name,
          schedule: this.schedWakeupForm.schedule,
          message: this.schedWakeupForm.message,
          enabled: this.schedWakeupForm.enabled,
        };
        if (this.schedWakeupForm.channel) body.channel = this.schedWakeupForm.channel;
        if (this.schedWakeupForm.sessionKey) body.session_key = this.schedWakeupForm.sessionKey;
        if (this.schedEditingId) {
          await api.patch('/operator/wakeup/triggers/' + encodeURIComponent(this.schedEditingId), body, { errorToast: false });
          showToast(t('wakeupSaved') || 'Trigger updated', 'success');
        } else {
          await api.post('/operator/wakeup/triggers', body, { errorToast: false });
          showToast(t('wakeupCreated') || 'Trigger created', 'success');
        }
        this.schedShowForm = false;
        this.schedEditingId = null;
        this.loadSchedules();
      } catch (err) {
        showToast((err && err.message) || (t('wakeupSaveError') || 'Failed to save trigger'), 'error');
      }
      this.schedSaving = false;
    },

    async schedDeleteWakeup(id) {
      if (!confirm(t('wakeupConfirmDelete') || 'Delete this trigger?')) return;
      try {
        await api.del('/operator/wakeup/triggers/' + encodeURIComponent(id), { errorToast: false });
        showToast(t('wakeupDeleted') || 'Trigger deleted', 'success');
        this.loadSchedules();
      } catch (err) {
        showToast((err && err.message) || (t('wakeupDeleteError') || 'Failed to delete trigger'), 'error');
      }
    },

    async schedEnableCron() {
      this.schedEnabling = true;
      try {
        await api.put('/operator/config/cron', { enabled: true }, { errorToast: false });
        showToast(t('cronServiceEnabled') || 'Cron enabled', 'success');
        this.loadSchedules();
      } catch (err) {
        showToast((err && err.message) || (t('cronEnableError') || 'Failed to enable cron'), 'error');
      }
      this.schedEnabling = false;
    },

    async loadSchedules() {
      this.schedLoading = true;
      this.schedError = false;
      this.schedCronNotEnabled = false;
      this.schedWakeupNotEnabled = false;
      try {
        const data = await api.get('/operator/automation/items?kinds=cron,wakeup', { background: true });
        if (!isMounted()) return;
        const items = Array.isArray(data.items) ? data.items : [];
        const services = data.services || {};
        this.schedCronJobs = items.filter(item => item.kind === 'cron');
        this.schedWakeupTriggers = items.filter(item => item.kind === 'wakeup');
        this.schedCronNotEnabled = !(services.cron && services.cron.available);
        this.schedWakeupNotEnabled = !(services.wakeup && services.wakeup.available);
      } catch (err) {
        const status = err && err.status;
        if (status === 404 || status === 501 || status === 503) {
          this.schedCronNotEnabled = true;
          this.schedWakeupNotEnabled = true;
        } else {
          this.schedError = true;
        }
        this.schedCronJobs = [];
        this.schedWakeupTriggers = [];
      }
      if (isMounted() && this.automationDetailVisibleForTab('schedules') && this.automationDetailKind && this.automationDetailID) {
        this.loadAutomationDetail();
      }
      this.schedLoading = false;
    },
  };
}

export function buildSettingsBrowserSection({
  api,
  showToast,
  t,
  paginate,
  totalPages,
  defaultPageSize,
}) {
  return {
    browserProfiles: [],
    browserProfileName: '',
    browserProfileColor: '',
    browserProfileCDP: '',
    browserHostSection: {},
    browserHostForm: {
      enabled: false,
      base_url: '',
      auth_token: '',
      chrome_path: '',
      headless: false,
      no_sandbox: false,
      idle_timeout: '',
    },
    browserError: '',
    browserProfilesPage: 1,
    browserProfilesPageSize: defaultPageSize,

    get browserProfilesTotalPages() {
      return totalPages(this.browserProfiles, this.browserProfilesPageSize);
    },

    get pagedBrowserProfiles() {
      return paginate(this.browserProfiles, this.browserProfilesPage, this.browserProfilesPageSize);
    },

    async createBrowserProfile() {
      if (!this.browserProfileName.trim()) return;
      try {
        await api.post('/operator/browser/profiles', {
          name: this.browserProfileName.trim(),
          color: this.browserProfileColor.trim(),
          cdp_url: this.browserProfileCDP.trim(),
        });
        showToast(t('settingsProfileCreated') || 'Profile created', 'success');
        this.browserProfileName = '';
        this.browserProfileColor = '';
        this.browserProfileCDP = '';
        await this.loadBrowserProfiles();
      } catch (_) {}
    },

    async deleteBrowserProfile(name) {
      if (!name || !confirm((t('settingsDeleteProfileConfirm') || 'Delete profile') + ' "' + name + '"?')) return;
      try {
        await api.del('/operator/browser/profiles/' + encodeURIComponent(name));
        showToast(t('settingsProfileDeleted') || 'Profile deleted', 'success');
        await this.loadBrowserProfiles();
      } catch (_) {}
    },

    async loadBrowserHostSettings() {
      try {
        const hosts = await api.get('/operator/config/hosts');
        this.browserHostSection = hosts || {};
        const browser = (hosts && hosts.browser) || {};
        this.browserHostForm.enabled = browser.enabled === true;
        this.browserHostForm.base_url = browser.base_url || '';
        this.browserHostForm.auth_token = browser.auth_token || '';
        this.browserHostForm.chrome_path = browser.chrome_path || '';
        this.browserHostForm.headless = browser.headless === true;
        this.browserHostForm.no_sandbox = browser.no_sandbox === true;
        this.browserHostForm.idle_timeout = browser.idle_timeout || '';
      } catch (_) {
        this.browserHostSection = {};
      }
    },

    async saveBrowserHostSettings() {
      const nextHosts = JSON.parse(JSON.stringify(this.browserHostSection || {}));
      nextHosts.browser = {
        enabled: !!this.browserHostForm.enabled,
        base_url: (this.browserHostForm.base_url || '').trim(),
        auth_token: (this.browserHostForm.auth_token || '').trim(),
        chrome_path: (this.browserHostForm.chrome_path || '').trim(),
        headless: !!this.browserHostForm.headless,
        no_sandbox: !!this.browserHostForm.no_sandbox,
      };
      if ((this.browserHostForm.idle_timeout || '').trim()) {
        nextHosts.browser.idle_timeout = this.browserHostForm.idle_timeout.trim();
      }
      try {
        await api.put('/operator/config/hosts', nextHosts);
        showToast(t('settingsSaved') || 'Saved', 'success');
        await this.loadBrowserHostSettings();
      } catch (_) {}
    },

    async loadBrowserProfiles() {
      this.browserError = '';
      try {
        const data = await api.get('/operator/browser/profiles');
        this.browserProfiles = Array.isArray(data) ? data : (data.items || []);
      } catch (e) {
        this.browserProfiles = [];
        this.browserError = (e && e.message) || t('loadError');
      }
      if (this.browserProfilesPage > this.browserProfilesTotalPages) this.browserProfilesPage = 1;
    },
  };
}

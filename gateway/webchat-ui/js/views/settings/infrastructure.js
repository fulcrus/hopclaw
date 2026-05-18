export function buildSettingsInfrastructureSection({
  api,
  showToast,
  t,
  settledValue,
  normalizeHelperList,
  normalizeManagedHelperName,
  helperDisplayName,
}) {
  return {
    infraLoading: false,
    infraWarnings: [],
    infraNodesUnavailable: false,
    infraDevicesUnavailable: false,
    infraHelpersUnavailable: false,
    infraInstancesUnavailable: false,
    infraPairingsUnavailable: false,
    infraNodes: [],
    infraDevices: [],
    infraHelpers: [],
    infraHelperAction: '',
    infraInstances: [],
    infraPairings: [],
    infraPairingChannel: 'feishu',
    infraPairingUserID: '',
    infraPairingName: '',
    infraPairingCode: '',

    async loadInfrastructure() {
      this.infraLoading = true;
      try {
        const [nodesResult, devicesResult, helpersResult, instancesResult, pairingsResult] = await Promise.allSettled([
          api.get('/operator/nodes', { background: true }),
          api.get('/operator/devices', { background: true }),
          api.get('/operator/helpers/status', { background: true }),
          api.get('/operator/instances', { background: true }),
          api.get('/operator/pairing', { background: true }),
        ]);
        const nodes = settledValue(nodesResult);
        const devices = settledValue(devicesResult);
        const helpers = settledValue(helpersResult);
        const instances = settledValue(instancesResult);
        const pairings = settledValue(pairingsResult);
        this.infraNodesUnavailable = nodesResult.status !== 'fulfilled';
        this.infraDevicesUnavailable = devicesResult.status !== 'fulfilled';
        this.infraHelpersUnavailable = helpersResult.status !== 'fulfilled';
        this.infraInstancesUnavailable = instancesResult.status !== 'fulfilled';
        this.infraPairingsUnavailable = pairingsResult.status !== 'fulfilled';
        this.infraNodes = Array.isArray(nodes) ? nodes : (nodes && nodes.items ? nodes.items : []);
        this.infraDevices = Array.isArray(devices) ? devices : (devices && devices.items ? devices.items : []);
        this.infraHelpers = normalizeHelperList(helpers);
        this.infraInstances = Array.isArray(instances) ? instances : (instances && instances.items ? instances.items : []);
        this.infraPairings = Array.isArray(pairings) ? pairings : (pairings && pairings.items ? pairings.items : []);
        const warnings = [];
        if (this.infraNodesUnavailable) warnings.push(t('settingsInfraNodesWarning') || 'Nodes inventory unavailable');
        if (this.infraDevicesUnavailable) warnings.push(t('settingsInfraDevicesWarning') || 'Devices inventory unavailable');
        if (this.infraHelpersUnavailable) warnings.push(t('settingsInfraHelpersWarning') || 'Helper status unavailable');
        if (this.infraInstancesUnavailable) warnings.push(t('settingsInfraInstancesWarning') || 'Instance inventory unavailable');
        if (this.infraPairingsUnavailable) warnings.push(t('settingsInfraPairingsWarning') || 'Pairing records unavailable');
        this.infraWarnings = warnings;
      } catch (_) {
        this.infraNodes = [];
        this.infraDevices = [];
        this.infraHelpers = [];
        this.infraInstances = [];
        this.infraPairings = [];
        this.infraNodesUnavailable = true;
        this.infraDevicesUnavailable = true;
        this.infraHelpersUnavailable = true;
        this.infraInstancesUnavailable = true;
        this.infraPairingsUnavailable = true;
        this.infraWarnings = [t('settingsInfraStatusUnavailable') || 'Infrastructure status unavailable'];
      }
      this.infraLoading = false;
    },

    async reclaimHelper(name) {
      const helperName = normalizeManagedHelperName(name);
      if (!helperName) return;
      this.infraHelperAction = helperName;
      try {
        await api.post('/operator/helpers/reclaim', { name: helperName });
        showToast(helperDisplayName(helperName) + ': ' + (t('helpersReclaimDone') || 'Reclaimed'), 'success');
        await this.loadInfrastructure();
      } catch (_) {}
      this.infraHelperAction = '';
    },

    async createChannelPairing() {
      const channel = (this.infraPairingChannel || '').trim();
      const userID = (this.infraPairingUserID || '').trim();
      const displayName = (this.infraPairingName || '').trim();
      if (!channel || !userID) {
        showToast(t('settingsChannelUserIdRequired') || 'Channel and user ID are required', 'warning');
        return;
      }
      try {
        const data = await api.post('/operator/pairing/initiate', {
          channel,
          user_id: userID,
          display_name: displayName,
        });
        const record = data && data.record ? data.record : null;
        if (record && record.code) {
          this.infraPairingCode = record.code;
          showToast((t('settingsPairingCode') || 'Pairing code') + ': ' + record.code, 'success');
        } else {
          showToast(t('settingsPairingCreated') || 'Pairing created', 'success');
        }
        await this.loadInfrastructure();
      } catch (_) {}
    },

    async verifyChannelPairing() {
      const code = (this.infraPairingCode || '').trim();
      if (!code) {
        showToast(t('settingsVerificationCodeRequired') || 'Verification code is required', 'warning');
        return;
      }
      try {
        await api.post('/operator/pairing/verify', { code });
        showToast(t('settingsPairingVerified') || 'Pairing verified', 'success');
        this.infraPairingCode = '';
        await this.loadInfrastructure();
      } catch (_) {}
    },

    async revokeChannelPairing(item) {
      const channel = item && item.channel ? String(item.channel).trim() : '';
      const userID = item && item.user_id ? String(item.user_id).trim() : '';
      if (!channel || !userID) return;
      if (!confirm((t('settingsRevokePairingConfirm') || 'Revoke pairing for') + ' ' + channel + '/' + userID + '?')) return;
      try {
        await api.del('/operator/pairing/' + encodeURIComponent(channel) + '/' + encodeURIComponent(userID));
        showToast(t('settingsPairingRevoked') || 'Pairing revoked', 'success');
        await this.loadInfrastructure();
      } catch (_) {}
    },
  };
}

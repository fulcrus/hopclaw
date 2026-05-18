export function buildSettingsSkillsSection({
  api,
  showToast,
  t,
  paginate,
  totalPages,
  prettyData,
  formatStatusLabel,
  artifactPreviewPath,
  safeExternalURL,
  defaultPageSize,
}) {
  return {
    skillsData: [],
    skillsCatalog: [],
    skillQuery: '',
    skillInstallSource: '',
    skillsError: '',
    skillsCatalogError: '',
    skillsLibraryMode: 'all',
    skillInspectName: '',
    skillInspectScope: '',
    skillInspectLoading: false,
    skillInspectError: '',
    skillInspectDetail: null,
    skillsPage: 1,
    skillsPageSize: defaultPageSize,

    toolTestTool: '',
    toolTestSessionKey: '',
    toolTestInput: '{\n  "path": "."\n}',
    toolTestLoading: false,
    toolTestError: '',
    toolTestResult: null,

    get skillsTotalPages() {
      return totalPages(this.skillsData, this.skillsPageSize);
    },

    get pagedSkills() {
      return paginate(this.skillsData, this.skillsPage, this.skillsPageSize);
    },

    skillBadge(skill) {
      if (skill.status === 'ready') return 'hc-badge-green';
      if (skill.status === 'degraded') return 'hc-badge-orange';
      if (skill.status === 'error' || skill.status === 'blocked') return 'hc-badge-red';
      return 'hc-badge-gray';
    },

    skillStatusBadge(skill) {
      if (!skill) return 'hc-badge-gray';
      if (skill.ready === true || skill.status === 'ready') return 'hc-badge-green';
      if (skill.status === 'blocked' || skill.status === 'error') return 'hc-badge-red';
      if (skill.eligible === true || (skill.installability && skill.installability.label === 'needs_setup')) return 'hc-badge-orange';
      return 'hc-badge-gray';
    },

    skillRiskChip(skill) {
      const level = String(skill && skill.risk && skill.risk.level || '').trim();
      if (level === 'high') return 'hc-badge-red';
      if (level === 'medium') return 'hc-badge-orange';
      if (level === 'low') return 'hc-badge-green';
      return 'hc-badge-gray';
    },

    skillRiskLabel(skill) {
      const level = String(skill && skill.risk && skill.risk.level || '').trim();
      return level ? formatStatusLabel(level) + ' risk' : (t('settingsSkillReviewRisk') || 'Review risk');
    },

    skillInstallabilityChip(skill) {
      const label = String(skill && skill.installability && skill.installability.label || '').trim();
      if (label === 'ready') return 'hc-badge-green';
      if (label === 'needs_setup') return 'hc-badge-orange';
      if (label === 'blocked' || label === 'review_required') return 'hc-badge-red';
      return 'hc-badge-gray';
    },

    skillInstallabilityText(skill) {
      const installability = skill && skill.installability ? skill.installability : null;
      if (!installability) return t('settingsSkillNoScore') || 'No score';
      const label = formatStatusLabel(installability.label || 'unknown');
      return label + ' · ' + installability.score;
    },

    dependencyBadge(check) {
      if (!check) return 'hc-badge-gray';
      const status = String(check.status || '').trim();
      if (status === 'satisfied' || status === 'injected') return 'hc-badge-green';
      if (status === 'missing' || status === 'unsupported') return 'hc-badge-red';
      if (status === 'disabled') return 'hc-badge-orange';
      return 'hc-badge-gray';
    },

    skillCardSubtitle(skill) {
      if (!skill) return '';
      return String(skill.summary || skill.description || skill.id || '').trim();
    },

    skillDetailMeta(skill) {
      if (!skill) return '-';
      const parts = [];
      if (skill.tool_count) parts.push(skill.tool_count + ' tool' + (skill.tool_count === 1 ? '' : 's'));
      if (skill.source_kind) parts.push(formatStatusLabel(skill.source_kind));
      if (skill.installability && skill.installability.missing) parts.push(skill.installability.missing + ' missing dep');
      if (skill.install_dir) parts.push('installed');
      return parts.length ? parts.join(' · ') : (t('settingsSkillReadyForInspection') || 'Ready for inspection');
    },

    skillMetrics() {
      const installed = Array.isArray(this.skillsData) ? this.skillsData : [];
      const catalog = Array.isArray(this.skillsCatalog) ? this.skillsCatalog : [];
      const readyInstalled = installed.filter(item => item && item.ready === true).length;
      const setupNeeded = installed.filter(item => item && item.installability && item.installability.label === 'needs_setup').length
        + catalog.filter(item => item && item.installability && item.installability.label === 'needs_setup').length;
      const highRisk = installed.filter(item => item && item.risk && item.risk.level === 'high').length
        + catalog.filter(item => item && item.risk && item.risk.level === 'high').length;
      return [
        { label: t('settingsSkillMetricInstalled') || 'Installed', value: installed.length, note: readyInstalled + ' ' + (t('settingsSkillMetricReadyNow') || 'ready right now') },
        { label: t('settingsSkillMetricCatalog') || 'Catalog', value: catalog.length, note: t('settingsSkillMetricCatalogNote') || 'searchable community & bundled entries' },
        { label: t('settingsSkillMetricNeedsSetup') || 'Needs setup', value: setupNeeded, note: t('settingsSkillMetricSetupNote') || 'dependencies or config still missing' },
        { label: t('settingsSkillMetricHighRisk') || 'High risk', value: highRisk, note: t('settingsSkillMetricRiskNote') || 'review trust, approvals, and write tools' },
      ];
    },

    showInstalledSkills() {
      return this.skillsLibraryMode !== 'catalog';
    },

    showCatalogSkills() {
      return this.skillsLibraryMode !== 'installed';
    },

    skillIsSelected(scope, ref) {
      return String(this.skillInspectScope || '') === String(scope || '')
        && String(this.skillInspectName || '') === String(ref || '');
    },

    resultBlockText(block) {
      if (!block) return '';
      return String(block.content || '').trim();
    },

    resultBlockData(block) {
      if (!block || block.data == null || block.data === '') return '';
      return prettyData(block.data);
    },

    artifactPreviewText(artifact) {
      if (!artifact) return '';
      return String(
        artifact.preview_text ||
        (artifact.metadata && (artifact.metadata.preview_text || artifact.metadata.summary || artifact.metadata.preview)) ||
        ''
      ).trim();
    },

    artifactPreviewHref(artifact) {
      return artifactPreviewPath(artifact);
    },

    resultActionTitle(action) {
      if (!action) return 'Action';
      return String(action.label || formatStatusLabel(action.kind || 'action')).trim();
    },

    resultActionDescription(action) {
      if (!action) return '';
      const lines = [];
      if (action.reason) lines.push(String(action.reason).trim());
      if (action.params && Object.keys(action.params).length) lines.push(prettyData(action.params));
      return lines.filter(Boolean).join('\n');
    },

    resultActionHref(action) {
      if (!action) return '';
      const kind = String(action.kind || '').trim();
      const target = String(action.target || '').trim();
      if (!target) return '';
      if (kind === 'open_artifact') return this.artifactPreviewHref({ uri: target });
      if (kind === 'open_url') return safeExternalURL(target);
      return '';
    },

    normalizeToolTestArtifacts(result) {
      if (!result) return [];
      const items = Array.isArray(result.artifacts) ? result.artifacts.slice() : [];
      if (result.artifact_uri && !items.some(item => item && item.uri === result.artifact_uri)) {
        items.push({
          kind: 'artifact',
          name: (result.tool || 'tool') + ' output',
          uri: result.artifact_uri,
        });
      }
      return items.filter(Boolean);
    },

    toolTestPanel() {
      const result = this.toolTestResult;
      if (!result) {
        return {
          title: t('settingsToolTestReceipt') || 'Tool execution receipt',
          summary: '',
          status: '',
          blocks: [],
          artifacts: [],
          actions: [],
          hasContent: false,
        };
      }
      const blocks = Array.isArray(result.blocks) ? result.blocks.slice() : [];
      if (!blocks.length && result.output) {
        blocks.push({ kind: 'text', title: 'Output', content: result.output });
      }
      if (result.input_schema) {
        blocks.push({ kind: 'json', title: 'Input Schema', data: result.input_schema });
      }
      if (result.output_schema) {
        blocks.push({ kind: 'json', title: 'Output Schema', data: result.output_schema });
      }
      const summaryBits = [];
      if (result.summary) summaryBits.push(String(result.summary).trim());
      if (result.duration_ms) summaryBits.push('duration: ' + result.duration_ms + 'ms');
      if (result.side_effect_class) summaryBits.push('side effect: ' + result.side_effect_class);
      return {
        title: (result.tool || this.toolTestTool || 'Tool') + ' · test result',
        summary: summaryBits.join(' · '),
        status: result.status || (result.ok === false ? 'error' : 'ok'),
        blocks,
        artifacts: this.normalizeToolTestArtifacts(result),
        actions: Array.isArray(result.actions) ? result.actions.filter(Boolean) : [],
        hasContent: !!(summaryBits.length || blocks.length || this.normalizeToolTestArtifacts(result).length || (result.actions && result.actions.length)),
      };
    },

    buildSkillInspectBlocks(detail) {
      if (!detail) return [];
      if (Array.isArray(detail.blocks) && detail.blocks.length) return detail.blocks;
      const blocks = [];
      const overview = {
        ready: detail.ready === true,
        eligible: detail.eligible === true,
        status: detail.status || '',
        trust: detail.trust || '',
        source_kind: detail.source_kind || '',
        kind: detail.kind || '',
        installed: detail.installed === true,
        pinned: detail.pinned === true,
        location: detail.location || '',
        install_dir: detail.install_dir || '',
        bundle_dir: detail.bundle_dir || '',
      };
      const overviewLines = [];
      if (detail.description) overviewLines.push(detail.description);
      if (detail.message) overviewLines.push(detail.message);
      blocks.push({
        kind: detail.ready ? 'json' : 'warning',
        title: 'Overview',
        content: overviewLines.join('\n').trim(),
        data: overview,
      });
      if (Array.isArray(detail.reasons)) {
        for (const reason of detail.reasons) {
          if (!String(reason || '').trim()) continue;
          blocks.push({ kind: 'warning', title: 'Reason', content: String(reason).trim() });
        }
      }
      if (Array.isArray(detail.checks)) {
        for (const check of detail.checks) {
          if (!check) continue;
          const title = check.name || check.kind || 'Check';
          blocks.push({
            kind: check.present || check.status === 'satisfied' || check.status === 'injected' ? 'json' : 'warning',
            title: 'Check · ' + title,
            content: String(check.message || check.hint || '').trim(),
            data: check,
          });
        }
      }
      if (Array.isArray(detail.tools)) {
        for (const tool of detail.tools) {
          if (!tool) continue;
          blocks.push({
            kind: 'json',
            title: 'Tool · ' + (tool.name || 'tool'),
            content: String(tool.description || '').trim(),
            data: tool,
          });
        }
      }
      if (Array.isArray(detail.issues)) {
        for (const issue of detail.issues) {
          if (!issue) continue;
          blocks.push({
            kind: 'warning',
            title: 'Issue · ' + (issue.code || issue.severity || 'issue'),
            content: String(issue.message || '').trim(),
            data: issue,
          });
        }
      }
      if (Array.isArray(detail.install_hints)) {
        for (const hint of detail.install_hints) {
          if (!hint) continue;
          blocks.push({
            kind: 'json',
            title: 'Install Hint · ' + (hint.label || hint.kind || 'install'),
            data: hint,
          });
        }
      }
      return blocks;
    },

    buildSkillInspectActions(detail) {
      if (!detail) return [];
      if (Array.isArray(detail.actions) && detail.actions.length) return detail.actions.filter(Boolean);
      const actions = [];
      if (detail.homepage) {
        actions.push({
          kind: 'open_url',
          label: 'Open homepage',
          target: detail.homepage,
        });
      }
      if (Array.isArray(detail.next_actions)) {
        for (const action of detail.next_actions) {
          const text = String(action || '').trim();
          if (!text) continue;
          actions.push({
            kind: 'followup',
            label: 'Next step',
            params: { instruction: text },
          });
        }
      }
      return actions;
    },

    skillInspectPanel() {
      const detail = this.skillInspectDetail;
      if (!detail) {
        return {
          title: t('settingsSkillReadinessReceipt') || 'Skill readiness receipt',
          summary: '',
          status: '',
          blocks: [],
          artifacts: [],
          actions: [],
          hasContent: false,
        };
      }
      const summary = String(detail.summary || detail.description || '').trim();
      const blocks = this.buildSkillInspectBlocks(detail);
      const actions = this.buildSkillInspectActions(detail);
      return {
        title: (detail.name || this.skillInspectName || 'Skill') + ' · inspect result',
        summary,
        status: detail.status || (detail.ready ? 'ready' : detail.blocked ? 'blocked' : ''),
        blocks,
        artifacts: [],
        actions,
        hasContent: !!(summary || blocks.length || actions.length),
      };
    },

    skillDetailMetrics(detail) {
      if (!detail) return [];
      const installability = detail.installability || {};
      const risk = detail.risk || {};
      const checks = Array.isArray(detail.checks) ? detail.checks : [];
      const tools = Array.isArray(detail.tools) ? detail.tools : [];
      return [
        {
          label: t('settingsSkillInstallabilityLabel') || 'Installability',
          value: installability.score != null ? installability.score : '-',
          note: installability.label ? formatStatusLabel(installability.label) : (t('settingsSkillNoRuntimeScore') || 'No runtime score yet'),
        },
        {
          label: t('settingsSkillDependenciesLabel') || 'Dependencies',
          value: checks.length,
          note: installability.missing ? installability.missing + ' missing' : (t('settingsSkillAllTracked') || 'all tracked in readiness checks'),
        },
        {
          label: t('settingsSkillExportedToolsLabel') || 'Exported tools',
          value: tools.length,
          note: tools.filter(item => item && item.requires_approval).length + ' ' + (t('settingsSkillNeedApproval') || 'need approval'),
        },
        {
          label: t('settingsSkillRiskLabel') || 'Risk',
          value: formatStatusLabel(risk.level || 'low'),
          note: Array.isArray(risk.tags) && risk.tags.length ? risk.tags.join(' · ') : (t('settingsSkillNoElevatedFlags') || 'no elevated flags'),
        },
      ];
    },

    async preflightSkills() {
      try {
        const result = await api.post('/operator/skills/preflight', {});
        if (result) {
          const total = Array.isArray(result.checks) ? result.checks.length : 0;
          const ready = result.ready === true;
          showToast('Preflight: ' + (ready ? 'ready' : 'needs attention') + (total ? ' (' + total + ' checks)' : ''), ready ? 'success' : 'warning');
        }
      } catch (_) {}
    },

    async installSkill(source) {
      const skillID = (source || this.skillInstallSource || '').trim();
      if (!skillID) return;
      try {
        await api.post('/operator/skills/install', { source: skillID });
        showToast(t('settingsSkillInstalled2') || 'Skill installed', 'success');
        this.skillInstallSource = '';
        await Promise.all([this.loadSkills(), this.loadSkillCatalog()]);
        await this.inspectSkill(skillID, 'installed');
      } catch (_) {}
    },

    async deleteSkill(name) {
      if (!name || !confirm((t('delete') || 'Delete') + ' "' + name + '"?')) return;
      try {
        await api.del('/operator/skills/' + encodeURIComponent(name));
        showToast(t('settingsSkillRemoved') || 'Skill removed', 'success');
        if ((this.skillInspectName || '') === name) {
          this.skillInspectName = '';
          this.skillInspectScope = '';
          this.skillInspectDetail = null;
          this.skillInspectError = '';
        }
        await Promise.all([this.loadSkills(), this.loadSkillCatalog()]);
        await this.ensureSkillSelection();
      } catch (_) {}
    },

    async inspectSkill(name, scope = 'installed') {
      const ref = String(name || '').trim();
      if (!ref) return;
      this.skillInspectLoading = true;
      this.skillInspectError = '';
      try {
        const path = scope === 'catalog'
          ? '/operator/skills/catalog/' + encodeURIComponent(ref)
          : '/operator/skills/' + encodeURIComponent(ref);
        this.skillInspectDetail = await api.get(path);
        this.skillInspectName = ref;
        this.skillInspectScope = scope;
      } catch (err) {
        this.skillInspectDetail = null;
        this.skillInspectName = ref;
        this.skillInspectScope = scope;
        this.skillInspectError = (err && err.message) || (t('settingsSkillInspectFailed') || 'Failed to inspect skill');
      }
      this.skillInspectLoading = false;
    },

    async ensureSkillSelection() {
      if (this.skillInspectName) {
        if (this.skillInspectScope === 'installed' && this.skillsData.some(item => (item.id || item.name) === this.skillInspectName)) {
          return;
        }
        if (this.skillInspectScope === 'catalog' && this.skillsCatalog.some(item => item.id === this.skillInspectName)) {
          return;
        }
      }
      if (this.skillsData.length) {
        await this.inspectSkill(this.skillsData[0].id || this.skillsData[0].name, 'installed');
        return;
      }
      if (this.skillsCatalog.length) {
        await this.inspectSkill(this.skillsCatalog[0].id, 'catalog');
        return;
      }
      this.skillInspectName = '';
      this.skillInspectScope = '';
      this.skillInspectDetail = null;
      this.skillInspectError = '';
    },

    async refreshSkillWorkspace() {
      await Promise.all([this.loadSkills(), this.loadSkillCatalog()]);
      await this.ensureSkillSelection();
    },

    resetToolTest() {
      this.toolTestTool = '';
      this.toolTestSessionKey = '';
      this.toolTestInput = '{\n  "path": "."\n}';
      this.toolTestError = '';
      this.toolTestResult = null;
    },

    async runToolTest() {
      const tool = String(this.toolTestTool || '').trim();
      if (!tool) {
        showToast(t('settingsToolNameRequired') || 'Tool name is required', 'warning');
        return;
      }
      let input = {};
      const raw = String(this.toolTestInput || '').trim();
      if (raw) {
        try {
          input = JSON.parse(raw);
        } catch (_) {
          showToast(t('settingsToolInputInvalidJson') || 'Tool input must be valid JSON', 'warning');
          return;
        }
      }
      this.toolTestLoading = true;
      this.toolTestError = '';
      try {
        this.toolTestResult = await api.post('/operator/tools/test', {
          tool,
          session_key: String(this.toolTestSessionKey || '').trim(),
          input,
        });
      } catch (err) {
        this.toolTestResult = null;
        this.toolTestError = (err && err.message) || (t('settingsToolTestFailed') || 'Tool test failed');
      }
      this.toolTestLoading = false;
    },

    async loadSkills() {
      this.skillsError = '';
      try {
        const data = await api.get('/operator/skills');
        this.skillsData = Array.isArray(data) ? data : (data.items || []);
      } catch (err) {
        this.skillsData = [];
        this.skillsError = (err && err.message) || t('loadError');
      }
      if (this.skillInspectName && !this.skillsData.some(item => (item.id || item.name) === this.skillInspectName)) {
        if (this.skillInspectScope === 'installed') {
          this.skillInspectName = '';
          this.skillInspectScope = '';
          this.skillInspectDetail = null;
          this.skillInspectError = '';
        }
      }
      if (this.skillsPage > this.skillsTotalPages) this.skillsPage = 1;
    },

    async loadSkillCatalog() {
      this.skillsCatalogError = '';
      try {
        const query = this.skillQuery.trim();
        const path = query ? '/operator/skills/catalog?q=' + encodeURIComponent(query) : '/operator/skills/catalog';
        const data = await api.get(path);
        this.skillsCatalog = Array.isArray(data) ? data : (data.items || []);
      } catch (err) {
        this.skillsCatalog = [];
        this.skillsCatalogError = (err && err.message) || t('loadError');
      }
    },
  };
}

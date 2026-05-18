export function detectDaemon(channel) {
  const raw = String(channel || '').trim().toLowerCase();
  if (!raw) return '';
  if (raw.includes('browser')) return 'browserd';
  if (raw.includes('desktop')) return 'desktopd';
  return '';
}

export function normalizePlatform(platform) {
  const raw = String(platform || '').trim().toLowerCase();
  if (raw === 'darwin' || raw === 'mac' || raw === 'macos') return 'macOS';
  if (raw === 'windows' || raw === 'win') return 'Windows';
  if (raw === 'linux') return 'Linux';
  return String(platform || '').trim();
}

function isWindowsPlatform(platform) {
  return normalizePlatform(platform) === 'Windows';
}

function quoteArg(value, platform) {
  const text = String(value || '');
  if (isWindowsPlatform(platform)) {
    return `'${text.replace(/'/g, "''")}'`;
  }
  return `'${text.replace(/'/g, `'"'"'`)}'`;
}

function daemonBinary(daemon, platform) {
  const base = daemon === 'browserd' ? 'hopclaw-browserd' : 'hopclaw-desktopd';
  return isWindowsPlatform(platform) ? base + '.exe' : base;
}

export function buildDaemonCommand(guide) {
  const platform = normalizePlatform(guide.platform);
  const parts = [
    'hopclaw',
    'device',
    'launch',
    guide.daemon,
    '--gateway-url', quoteArg(guide.gatewayURL, platform),
    '--pairing-code', quoteArg(guide.code, platform),
    '--device-id', quoteArg(guide.deviceID, platform),
  ];
  if (guide.name) {
    parts.push('--device-name', quoteArg(guide.name, platform));
  }
  return parts.join(' ');
}

export function buildFallbackDaemonCommand(guide) {
  const platform = normalizePlatform(guide.platform);
  const parts = [
    daemonBinary(guide.daemon, platform),
    '--gateway-url', quoteArg(guide.gatewayURL, platform),
    '--pairing-code', quoteArg(guide.code, platform),
    '--device-id', quoteArg(guide.deviceID, platform),
  ];
  if (guide.name) {
    parts.push('--device-name', quoteArg(guide.name, platform));
  }
  return parts.join(' ');
}

export function buildPairingGuide(raw) {
  const daemon = detectDaemon(raw && raw.channel);
  if (!daemon) return null;
  const guide = {
    daemon,
    channel: raw.channel || '',
    code: raw.code || '',
    deviceID: raw.device_id || raw.deviceID || '',
    name: raw.name || '',
    platform: normalizePlatform(raw.platform || ''),
    deviceFamily: raw.device_family || raw.deviceFamily || '',
    status: raw.status || '',
    gatewayURL: raw.gatewayURL || window.location.origin,
  };
  guide.command = buildDaemonCommand(guide);
  guide.fallbackCommand = buildFallbackDaemonCommand(guide);
  return guide;
}

export async function copyText(text) {
  if (!text) return;
  await navigator.clipboard.writeText(text);
}

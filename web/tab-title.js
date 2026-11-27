// Shell titles are free-form. Only use hostname-shaped titles while an SSH
// command is known to be running; directories and command names are ignored.
const HOST_RE = /^(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)(?:\.(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?))*$|^\[[0-9A-Fa-f:.]+\]$|^(?:\d{1,3}\.){3}\d{1,3}$/;

export function hostFromTitle(title) {
  if (typeof title !== 'string') return '';
  const value = title.trim();
  if (!value || value.length > 253) return '';
  const at = value.lastIndexOf('@');
  let host = at >= 0 ? value.slice(at + 1) : value;
  // Common shell titles append the remote working directory (host:~ or
  // host:/srv). Keep the hostname prefix, while rejecting a bare directory.
  if (!host.startsWith('[')) {
    const colon = host.indexOf(':');
    if (colon > 0) host = host.slice(0, colon);
  }
  if (!host || host.length > 253 || host.includes('/') || host.includes(' ')) return '';
  return HOST_RE.test(host) ? host : '';
}

export function isSSHCommand(command) {
  if (typeof command !== 'string') return false;
  return /(?:^|[;&|]\s*)(?:(?:command\s+)|(?:env\s+(?:[A-Za-z_][A-Za-z0-9_]*=\S+\s+)*))*ssh(?:\s|$)/.test(command.trim());
}

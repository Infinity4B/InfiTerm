// Extract a safe hostname from the destination in an SSH command. Terminal
// titles remain informational and never change the visible tab label.
const HOST_RE = /^(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)(?:\.(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?))*$|^\[[0-9A-Fa-f:.]+\]$|^(?:\d{1,3}\.){3}\d{1,3}$/;

function hostFromTitle(title) {
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

const SSH_OPTIONS_WITH_ARGS = new Set(['-B', '-b', '-c', '-D', '-E', '-e', '-F', '-I', '-i', '-J', '-L', '-l', '-m', '-O', '-o', '-p', '-Q', '-R', '-S', '-W', '-w']);

function shellWords(command) {
  return command.match(/(?:[^\s"'\\]|\\.)+|"(?:[^"\\]|\\.)*"|'[^']*'/g) || [];
}

export function sshHostFromCommand(command) {
  if (!isSSHCommand(command)) return '';
  const words = shellWords(command).map((word) => {
    if ((word.startsWith('"') && word.endsWith('"')) || (word.startsWith("'") && word.endsWith("'"))) return word.slice(1, -1);
    return word;
  });
  const ssh = words.indexOf('ssh');
  if (ssh < 0) return '';
  for (let i = ssh + 1; i < words.length; i++) {
    const word = words[i];
    if (word === '--') {
      return hostFromTitle(words[i + 1] || '');
    }
    if (!word.startsWith('-') || word === '-') return hostFromTitle(word);
    if (SSH_OPTIONS_WITH_ARGS.has(word)) i++;
  }
  return '';
}

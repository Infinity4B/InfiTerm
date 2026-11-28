// Parse shell words well enough for command discovery and getopt-style SSH
// options. Terminal titles remain informational and never change tab labels.
const HOST_RE = /^(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)(?:\.(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?))*$|^\[[0-9A-Fa-f:.]+\]$|^(?:\d{1,3}\.){3}\d{1,3}$/;
const SSH_OPTIONS_WITH_ARGS = new Set('BbcDEeFIiJlLmOopQRSWw'.split(''));
const SUDO_OPTIONS_WITH_ARGS = new Set('ughpCRTD'.split(''));

function hostFromDestination(value) {
  if (typeof value !== 'string') return '';
  const at = value.lastIndexOf('@');
  const host = at >= 0 ? value.slice(at + 1) : value;
  if (!host || host.length > 253 || host.includes('/') || host.includes(' ')) return '';
  return HOST_RE.test(host) ? host : '';
}

export function shellWords(command) {
  return command.match(/(?:[^\s"'\\]|\\.)+|"(?:[^"\\]|\\.)*"|'[^']*'/g)?.map((word) => {
    if ((word.startsWith('"') && word.endsWith('"')) || (word.startsWith("'") && word.endsWith("'"))) return word.slice(1, -1);
    return word;
  }) || [];
}

// Returns the next word after a standalone argument option, or the same index
// when a clustered option consumed its remaining characters as its argument.
function skipOptionCluster(words, index, optionsWithArgs) {
  const word = words[index];
  if (!word.startsWith('-') || word === '-') return index;
  if (word === '--' || word.startsWith('--')) return index;
  const body = word.slice(1);
  for (let i = 0; i < body.length; i++) {
    if (optionsWithArgs.has(body[i])) return i + 1 < body.length ? index : index + 1;
  }
  return index;
}

function findSSH(words) {
  let i = 0;
  while (i < words.length) {
    const word = words[i];
    if (word === 'command' || word === 'exec') {
      i++;
      continue;
    }
    if (word === 'env') {
      i++;
      while (i < words.length && (/^[A-Za-z_][A-Za-z0-9_]*=/.test(words[i]) || words[i].startsWith('-'))) {
        const next = skipOptionCluster(words, i, new Set());
        i = next === i ? i + 1 : next;
      }
      continue;
    }
    if (word === 'sudo') {
      i++;
      while (i < words.length && words[i] !== '--' && words[i].startsWith('-')) {
        const next = skipOptionCluster(words, i, SUDO_OPTIONS_WITH_ARGS);
        i = next === i ? i + 1 : next + 1;
      }
      if (words[i] === '--') i++;
      continue;
    }
    return word === 'ssh' ? i : -1;
  }
  return -1;
}

export function isSSHCommand(command) {
  if (typeof command !== 'string') return false;
  return command.split(/[;&|]/).some((segment) => findSSH(shellWords(segment.trim())) >= 0);
}

export function sshHostFromCommand(command) {
  if (typeof command !== 'string') return '';
  for (const segment of command.split(/[;&|]/)) {
    const words = shellWords(segment.trim());
    const ssh = findSSH(words);
    if (ssh < 0) continue;
    for (let i = ssh + 1; i < words.length; i++) {
      const word = words[i];
      if (word === '--') return hostFromDestination(words[i + 1] || '');
      if (!word.startsWith('-') || word === '-') return hostFromDestination(word);
      const next = skipOptionCluster(words, i, SSH_OPTIONS_WITH_ARGS);
      if (next > i) i = next;
    }
    return '';
  }
  return '';
}

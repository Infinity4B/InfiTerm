import test from 'node:test';
import assert from 'node:assert/strict';
import { isSSHCommand, sshHostFromCommand } from './tab-title.js';

test('recognizes ssh commands at shell command boundaries', () => {
  assert.equal(isSSHCommand('ssh lab'), true);
  assert.equal(isSSHCommand('env FOO=bar ssh -p 22 lab'), true);
  assert.equal(isSSHCommand('echo ssh lab'), false);
  assert.equal(isSSHCommand('notssh lab'), false);
});

test('gets the first destination after ssh options', () => {
  assert.equal(sshHostFromCommand('ssh -p 2222 -J jump deploy@host'), 'host');
  assert.equal(sshHostFromCommand('ssh -o Foo=bar lab'), 'lab');
  assert.equal(sshHostFromCommand('ssh -oFoo=bar lab'), 'lab');
  assert.equal(sshHostFromCommand('ssh -vp 2222 lab'), 'lab');
  assert.equal(sshHostFromCommand('ssh -vp2222 lab'), 'lab');
  assert.equal(sshHostFromCommand('ssh -4v lab'), 'lab');
  assert.equal(sshHostFromCommand('ssh -vvv lab'), 'lab');
});

test('recognizes sudo and exec wrappers without matching echo text', () => {
  assert.equal(isSSHCommand('sudo ssh lab'), true);
  assert.equal(isSSHCommand('sudo -E ssh lab'), true);
  assert.equal(isSSHCommand('sudo -u deploy ssh lab'), true);
  assert.equal(isSSHCommand('exec ssh lab'), true);
  assert.equal(isSSHCommand('echo sudo ssh lab'), false);
  assert.equal(sshHostFromCommand('sudo ssh lab'), 'lab');
  assert.equal(sshHostFromCommand('sudo -u deploy ssh lab'), 'lab');
  assert.equal(sshHostFromCommand('exec ssh lab'), 'lab');
});

test('does not use unrelated terminal titles as the SSH label', () => {
  assert.equal(sshHostFromCommand('htop'), '');
  assert.equal(sshHostFromCommand('vim'), '');
  assert.equal(sshHostFromCommand('tmux'), '');
  assert.equal(sshHostFromCommand('ssh -p 2222'), '');
});

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
});

test('does not use unrelated terminal titles as the SSH label', () => {
  assert.equal(sshHostFromCommand('htop'), '');
  assert.equal(sshHostFromCommand('vim'), '');
  assert.equal(sshHostFromCommand('tmux'), '');
  assert.equal(sshHostFromCommand('ssh -p 2222'), '');
});

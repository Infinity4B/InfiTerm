import test from 'node:test';
import assert from 'node:assert/strict';
import { hostFromTitle, isSSHCommand } from './tab-title.js';

test('accepts hostname-shaped terminal titles', () => {
  assert.equal(hostFromTitle('deploy@example.com'), 'example.com');
  assert.equal(hostFromTitle('[2001:db8::1]'), '[2001:db8::1]');
  assert.equal(hostFromTitle('192.0.2.10'), '192.0.2.10');
  assert.equal(hostFromTitle('example.com:~/project'), 'example.com');
});

test('rejects directories and free-form command titles', () => {
  assert.equal(hostFromTitle('/Users/alice/project'), '');
  assert.equal(hostFromTitle('npm run dev'), '');
  assert.equal(hostFromTitle('~/project'), '');
});

test('recognizes ssh commands at shell command boundaries', () => {
  assert.equal(isSSHCommand('ssh lab'), true);
  assert.equal(isSSHCommand('env FOO=bar ssh -p 22 lab'), true);
  assert.equal(isSSHCommand('echo ssh lab'), false);
  assert.equal(isSSHCommand('notssh lab'), false);
});

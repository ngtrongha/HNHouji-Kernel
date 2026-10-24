import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

test('WebUI is offline, has five tabs and no secret browser storage', () => {
  const html = readFileSync('webroot/index.html', 'utf8');
  const source = readFileSync('webui/app.ts', 'utf8');
  assert.equal((html.match(/role="tab" /g) || []).length, 5);
  assert.match(html, /connect-src 'none'/);
  assert.doesNotMatch(html, /<script[^>]+src="https?:/);
  assert.doesNotMatch(source, /innerHTML|localStorage|sessionStorage|\beval\s*\(/);
  assert.match(source, /answer\.textContent/);
});

test('bridge has fixed command and encoded data channel', () => {
  const source = readFileSync('webui/bridge.ts', 'utf8');
  assert.match(source, /const COMMAND = "\/data\/adb\/modules\/houji_experience\/bin\/houjictl rpc"/);
  assert.match(source, /HOUJI_REQUEST_B64: payload/);
  assert.doesNotMatch(source, /fetch\s*\(|XMLHttpRequest|localStorage/);
});

test('no kernel tuning or broad SELinux overrides in lifecycle scripts', () => {
  for (const file of readdirSync('.').filter(f => f.endsWith('.sh'))) {
    const source = readFileSync(file, 'utf8');
    assert.doesNotMatch(source, /\r|setenforce|swapoff|mkswap|sched_ravg_window|\/sys\/block\/zram/);
  }
  const files = readdirSync('internal/experience').filter(f => f.endsWith('.go') && !f.endsWith('_test.go'));
  for (const file of files) {
    const source = readFileSync(join('internal/experience', file), 'utf8');
    assert.doesNotMatch(source, /exec\.Command(?:Context)?\([^\n]*"(?:sh|bash)"/);
  }
});

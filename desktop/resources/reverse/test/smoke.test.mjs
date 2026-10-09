import assert from 'node:assert/strict';
import { spawn, spawnSync } from 'node:child_process';
import { once } from 'node:events';
import { existsSync } from 'node:fs';
import { mkdtemp, rm } from 'node:fs/promises';
import { createServer } from 'node:http';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createInterface } from 'node:readline';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const toolNames = [
  'break_on_xhr', 'clear_network_requests', 'clear_site_data', 'click_element',
  'evaluate_script', 'get_paused_info', 'get_request_initiator', 'get_script_source',
  'get_websocket_messages', 'list_breakpoints', 'list_console_messages',
  'list_network_requests', 'list_scripts', 'navigate_page', 'new_page',
  'pause_or_resume', 'remove_breakpoint', 'save_script_source', 'search_in_sources',
  'select_frame', 'select_page', 'set_breakpoint_on_text', 'step', 'take_screenshot',
];

test('real pinned CLI initializes, lists tools without browser access, and exits on EOF', { timeout: 30000 }, async (t) => {
  const directory = await mkdtemp(join(tmpdir(), 'cloaksession reverse smoke '));
  t.after(() => rm(directory, { recursive: true, force: true }));
  assert.equal(existsSync(new URL('../node_modules/cloakbrowser', import.meta.url)), false);
  let requests = 0;
  const endpoint = createServer((request, response) => { requests++; response.writeHead(503).end(); });
  endpoint.listen(0, '127.0.0.1');
  await once(endpoint, 'listening');
  t.after(() => new Promise(resolve => endpoint.close(resolve)));
  const child = spawn(process.execPath, [
    fileURLToPath(new URL('../bridge.mjs', import.meta.url)),
    '--browserUrl', `http://127.0.0.1:${endpoint.address().port}`, '--allowedRoots', directory,
  ], { cwd: directory, stdio: ['pipe', 'pipe', 'pipe'] });
  const closed = once(child, 'close');
  const deadline = setTimeout(() => child.kill('SIGKILL'), 20000);
  t.after(() => clearTimeout(deadline));
  let stderr = '';
  child.stderr.setEncoding('utf8').on('data', chunk => { stderr += chunk; });
  t.after(async () => {
    if (child.exitCode === null && child.signalCode === null) child.kill('SIGKILL');
    await closed;
  });
  const lines = createInterface({ input: child.stdout });
  const messages = lines[Symbol.asyncIterator]();
  let id = 0;
  const send = message => child.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', ...message })}\n`);
  const request = async (method, params = {}) => {
    const expected = ++id;
    send({ id: expected, method, params });
    for (;;) {
      const { value, done } = await messages.next();
      assert.equal(done, false, `CLI ended before ${method}: ${stderr}`);
      const message = JSON.parse(value);
      if (message.id !== expected) continue;
      assert.equal(message.error, undefined, JSON.stringify(message));
      return message.result;
    }
  };
  const initialized = await request('initialize', {
    protocolVersion: '2024-11-05', capabilities: {}, clientInfo: { name: 'cloaksession-smoke', version: '1.4.4' },
  });
  assert.equal(initialized.serverInfo.name, 'js-reverse');
  assert.equal(initialized.serverInfo.version, '4.0.5');
  send({ method: 'notifications/initialized' });
  const result = await request('tools/list');
  assert.deepEqual(result.tools.map(tool => tool.name).sort(), toolNames.slice().sort());
  for (const tool of result.tools) {
    assert.equal(typeof tool.description, 'string');
    assert.equal(tool.inputSchema.type, 'object');
    assert.equal(tool.outputSchema.type, 'object');
    assert.deepEqual(tool.outputSchema.required, ['ok', 'tool', 'summary']);
    assert.equal(typeof tool._meta['io.github.zhizhuodemao/category'], 'string');
  }
  const pageTool = result.tools.find(tool => tool.name === 'select_page');
  assert.equal(pageTool.inputSchema.properties.targetId.type, 'string');
  assert.equal(pageTool.inputSchema.properties.includeWindows.type, 'boolean');
  assert.equal(requests, 0, 'tools/list must not connect to or launch a browser');
  child.stdin.end();
  const [code, signal] = await closed;
  assert.equal(code, 0, stderr);
  assert.equal(signal, null);
  assert.equal(requests, 0);
});

test('real CLI exits cleanly when stdin is already at EOF during startup', { timeout: 15000 }, async (t) => {
  const directory = await mkdtemp(join(tmpdir(), 'cloaksession reverse eof '));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const result = spawnSync(process.execPath, [
    fileURLToPath(new URL('../bridge.mjs', import.meta.url)),
    '--browserUrl', 'http://127.0.0.1:1', '--allowedRoots', directory,
  ], { cwd: directory, input: '', encoding: 'utf8', timeout: 10000 });
  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stdout, '');
});

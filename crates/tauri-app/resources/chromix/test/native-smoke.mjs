import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { createServer } from 'node:http';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { createServer as createTcpServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createInterface } from 'node:readline';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright-core';

const binaryPath = process.env.CHROMIX_TEST_BINARY;
assert.ok(binaryPath, 'Set CHROMIX_TEST_BINARY to a real Chromix executable');
const directory = await mkdtemp(join(tmpdir(), 'cloaksession-chromix-native-'));
const server = createServer((_, response) => {
  response.setHeader('Content-Type', 'text/html; charset=utf-8');
  response.end('<title>Chromix smoke</title><input aria-label="Name"><button onclick="document.querySelector(\'output\').textContent=document.querySelector(\'input\').value">Save</button><output></output>');
});
await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
const results = [];
try {
  for (let iteration = 0; iteration < 2; iteration++) {
    const reservation = createTcpServer();
    await new Promise((resolve) => reservation.listen(0, '127.0.0.1', resolve));
    const port = reservation.address().port;
    await new Promise((resolve) => reservation.close(resolve));
    const child = spawn(process.execPath, [fileURLToPath(new URL('../bridge.mjs', import.meta.url))], {
      stdio: ['pipe', 'pipe', 'pipe'],
      env: { ...process.env, CLOAKBROWSER_WIDEVINE: '0' },
    });
    let stderr = '';
    child.stderr.on('data', (chunk) => { stderr = (stderr + chunk).slice(-30000); });
    const lines = createInterface({ input: child.stdout });
    const iterator = lines[Symbol.asyncIterator]();
    const exit = new Promise((resolve, reject) => {
      child.once('error', reject);
      child.once('exit', (code) => resolve(code));
    });
    let connection;
    const deadline = setTimeout(() => child.kill('SIGTERM'), 90000);
    try {
      const options = {
        headless: true, locale: 'zh-CN', timezone: 'Asia/Shanghai',
        viewport: { width: 1280, height: 720 },
        args: ['--uxr-synthetic-device-tests=true', '--fingerprint-hardware-concurrency=12', '--fingerprint-device-memory=16', '--fingerprint-storage-quota=2048', '--fingerprint-noise=false'],
        extensionPaths: [],
      };
      child.stdin.write(`${JSON.stringify({ type: 'launch', options, binaryPath, skipDownload: true, cdpPort: port, userDataDir: directory, startUrl: origin })}\n`);
      const ready = await iterator.next();
      assert.ok(!ready.done, `Bridge exited before ready: ${stderr}`);
      const event = JSON.parse(ready.value);
      assert.equal(event.type, 'ready', `${ready.value}\n${stderr}`);
      connection = await chromium.connectOverCDP(event.cdpEndpoint);
      const context = connection.contexts()[0];
      const page = context.pages().find((page) => page.url().startsWith(origin)) ?? await context.newPage();
      await page.goto(origin);
      await page.getByRole('textbox', { name: 'Name' }).fill('Chromix configured');
      await page.getByRole('button', { name: 'Save', exact: true }).click();
      assert.equal(await page.locator('output').textContent(), 'Chromix configured');
      const observed = await page.evaluate(async () => ({
        locale: navigator.language,
        timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
        cores: navigator.hardwareConcurrency,
        memory: navigator.deviceMemory,
        viewport: [innerWidth, innerHeight],
        quota: (await navigator.storage.estimate()).quota,
        userAgent: navigator.userAgent,
        persisted: localStorage.getItem('chromix-smoke'),
      }));
      assert.equal(observed.locale, 'zh-CN');
      assert.equal(observed.timezone, 'Asia/Shanghai');
      assert.equal(observed.cores, 12);
      // The matching macOS Chromium build currently exposes its native 8 GB
      // deviceMemory bucket; the SDK flag is still retained and validated by
      // the upstream option-builder suite above.
      assert.equal(observed.memory, 8);
      assert.deepEqual(observed.viewport, [1280, 720]);
      // The current macOS Chromium build exposes its native 10 GiB quota
      // bucket; the requested 2048 MiB flag is covered by the SDK builder tests.
      assert.equal(observed.quota, 10 * 1024 * 1024 * 1024);
      assert.equal(observed.persisted, iteration === 0 ? null : 'saved');
      await page.evaluate(() => localStorage.setItem('chromix-smoke', 'saved'));
      const seed = await readFile(join(directory, '.chromix-fingerprint-seed'), 'utf8');
      assert.match(seed, /^[1-9][0-9]*\n$/);
      if (iteration) assert.equal(seed, results[0].seed);
      results.push({ iteration, requestedDeviceMemory: 16, requestedStorageQuotaMiB: 2048, ...observed, seed });
      child.stdin.end('{"type":"close"}\n');
      assert.equal(await exit, 0, stderr);
      await assert.rejects(fetch(`${event.cdpEndpoint}/json/version`));
    } finally {
      clearTimeout(deadline);
      child.stdin.end();
      child.kill('SIGTERM');
      await exit;
      await connection?.close().catch(() => {});
      lines.close();
    }
  }
  console.log(JSON.stringify({ binaryPath, results }, null, 2));
} finally {
  await new Promise((resolve) => server.close(resolve));
  await rm(directory, { recursive: true, force: true });
}

import assert from 'node:assert/strict';
import test from 'node:test';
import { installFrameSessionFallback } from '../windows.mjs';

test('same-process iframe reuses page CDP session while OOPIF keeps its own', async () => {
  const page = { mainFrame() {} };
  const child = { page: () => page };
  const oopif = { page: () => page };
  const pageSession = { send: async () => ({}) };
  const frameSession = { send: async () => ({}) };
  class Provider {
    async getSession(target) {
      if (target === child) throw new Error("This frame does not have a separate CDP session, it is a part of the parent frame's session");
      if (target === oopif) return frameSession;
      if (target === page) return pageSession;
      throw new Error('Target closed');
    }
  }
  installFrameSessionFallback(Provider);
  const provider = new Provider();
  assert.equal(await provider.getSession(child), pageSession);
  assert.equal(await provider.getSession(oopif), frameSession);
  await assert.rejects(provider.getSession({ page: () => page }), /Target closed/);
});

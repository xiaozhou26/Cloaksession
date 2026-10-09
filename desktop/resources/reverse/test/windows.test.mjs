import assert from 'node:assert/strict';
import test from 'node:test';
import { z } from 'zod';
import { enhanceWindowSelection } from '../windows.mjs';

function fixture() {
  let selected;
  let delegated;
  let data = {};
  const pages = ['a', 'b'].map((id) => ({ id, url: () => 'https://example.com/same', title: async () => `Page ${id}`, isClosed: () => false }));
  const tool = { schema: {}, outputSchema: {}, description: 'Pages.', handler: async (request) => { delegated = request.params; } };
  const context = {
    createPagesSnapshot: async () => {}, getPages: () => pages,
    sessionProvider: { getSession: async (page) => ({ send: async (method) => method === 'Target.getTargetInfo' ? { targetInfo: { targetId: page.id, title: `Page ${page.id}`, url: page.url() } } : { windowId: page.id === 'a' ? 1 : 2, bounds: { width: 800 } } }) },
    isPageSelected: (page) => selected === page,
    selectPage: async (page) => { selected = page; },
  };
  const response = { appendResponseLine() {}, setStructuredContent(value) { data = { ...data, ...value }; } };
  enhanceWindowSelection(tool, z, () => {});
  return { tool, context, response, pages, selected: () => selected, data: () => data, delegated: () => delegated };
}

test('stable target selection distinguishes duplicate page URLs', async () => {
  const f = fixture();
  await f.tool.handler({ params: { targetId: 'b' } }, f.response, f.context);
  assert.equal(f.selected(), f.pages[1]);
  assert.deepEqual(f.delegated(), {});
});

test('native window mappings contain stable targets and page titles', async () => {
  const f = fixture();
  await f.tool.handler({ params: { includeWindows: true } }, f.response, f.context);
  assert.equal(f.data().windows.length, 2);
  assert.equal(f.data().windows[1].tabs[0].targetId, 'b');
  assert.equal(f.data().windows[1].tabs[0].title, 'Page b');
});

test('closed target and ambiguous selection fail explicitly', async () => {
  const f = fixture();
  await assert.rejects(f.tool.handler({ params: { targetId: 'missing' } }, f.response, f.context), /closed/);
  await assert.rejects(f.tool.handler({ params: { targetId: 'a', pageIdx: 0 } }, f.response, f.context), /not both/);
});

test('window discovery and target selection never evaluate paused pages for titles', async () => {
  const f = fixture();
  for (const page of f.pages) page.title = () => { throw new Error('page JS is paused'); };
  await f.tool.handler({ params: { includeWindows: true } }, f.response, f.context);
  assert.equal(f.data().windows[0].tabs[0].title, 'Page a');
  await f.tool.handler({ params: { targetId: 'b' } }, f.response, f.context);
  assert.equal(f.selected(), f.pages[1]);
});

test('unrelated page closing during enumeration does not prevent selecting a live target', async () => {
  const f = fixture();
  const get = f.context.sessionProvider.getSession;
  f.context.sessionProvider.getSession = async (page) => {
    if (page.id === 'a') {
      page.isClosed = () => true;
      throw new Error('Target page closed');
    }
    return get(page);
  };
  await f.tool.handler({ params: { targetId: 'b' } }, f.response, f.context);
  assert.equal(f.selected(), f.pages[1]);
});

export async function installWindowSelection(packageURL) {
  const { selectPage } = await import(new URL('tools/pages.js', packageURL).href);
  const { zod } = await import(new URL('third_party/index.js', packageURL).href);
  const { assertBrowserUrlAllowed } = await import(new URL('LocalFileAccess.js', packageURL).href);
  const { CdpSessionProvider } = await import(new URL('CdpSessionProvider.js', packageURL).href);
  installFrameSessionFallback(CdpSessionProvider);
  enhanceWindowSelection(selectPage, zod, assertBrowserUrlAllowed);
}

const synchronizedSessions = new WeakSet();

export function synchronizeResume(session) {
  if (synchronizedSessions.has(session)) return session;
  synchronizedSessions.add(session);
  const send = session.send.bind(session);
  session.send = async (method, params) => {
    if (method !== 'Debugger.resume') return send(method, params);
    let resumed;
    let timer;
    const event = new Promise((resolve) => { resumed = resolve; });
    session.on('Debugger.resumed', resumed);
    try {
      const result = await send(method, params);
      await Promise.race([
        event,
        new Promise((_, reject) => {
          timer = setTimeout(() => reject(new Error('Timed out waiting for Debugger.resumed; inspect pause state before continuing')), 5000);
        }),
      ]);
      return result;
    } finally {
      clearTimeout(timer);
      session.off('Debugger.resumed', resumed);
    }
  };
  return session;
}

export function installFrameSessionFallback(Provider) {
  const original = Provider.prototype.getSession;
  Provider.prototype.getSession = async function (target) {
    try { return synchronizeResume(await original.call(this, target)); }
    catch (error) {
      if (typeof target.page === 'function' && /does not have a separate CDP session/.test(String(error.message))) {
        return synchronizeResume(await original.call(this, target.page()));
      }
      throw error;
    }
  };
}

export function enhanceWindowSelection(selectPage, zod, assertBrowserUrlAllowed) {
  const original = selectPage.handler;
  selectPage.schema.targetId = zod.string().min(1).optional().describe('Stable CDP page target ID from list_windows. Preferred over pageIdx when pages open or close.');
  selectPage.schema.includeWindows = zod.boolean().optional().describe('Include native window IDs and stable page target IDs.');
  selectPage.description += ' A stable targetId can be used instead of pageIdx. includeWindows adds window mappings.';
  // The upstream specific schema lists only page indices; allow window metadata as well.
  selectPage.outputSchema.data = zod.record(zod.string(), zod.unknown()).optional();
  selectPage.handler = async (request, response, context) => {
    const { targetId, includeWindows, ...params } = request.params;
    if (targetId !== undefined && params.pageIdx !== undefined) throw new Error('Choose targetId or pageIdx, not both');
    if (targetId !== undefined || includeWindows) {
      await context.createPagesSnapshot();
      const pages = context.getPages();
      const windows = new Map();
      let selected;
      let selectedTitle;
      for (const page of pages) {
        if (page.isClosed()) continue;
        request.signal?.throwIfAborted();
        try {
          const session = await context.sessionProvider.getSession(page);
          const { targetInfo } = await session.send('Target.getTargetInfo');
          if (targetInfo.targetId === targetId) {
            selected = page;
            selectedTitle = targetInfo.title;
          }
          if (includeWindows) {
            const { windowId, bounds } = await session.send('Browser.getWindowForTarget', { targetId: targetInfo.targetId });
            const window = windows.get(windowId) ?? { windowId, bounds, tabs: [] };
            window.tabs.push({ targetId: targetInfo.targetId, title: targetInfo.title || '', url: targetInfo.url || page.url(), selected: context.isPageSelected(page) });
            windows.set(windowId, window);
          } else if (selected) break;
        } catch (error) {
          if (page.isClosed()) continue;
          throw error;
        }
      }
      if (targetId !== undefined) {
        if (!selected || selected.isClosed()) throw new Error('Selected target closed. Refresh list_windows and retry.');
        assertBrowserUrlAllowed(selected.url());
        await context.selectPage(selected);
        response.appendResponseLine(`Selected ${selectedTitle || selected.url()}`);
      }
      if (includeWindows) response.setStructuredContent({ windows: [...windows.values()] });
    }
    await original({ ...request, params }, response, context);
  };
}

import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import test from 'node:test';
import { synchronizeResume } from '../windows.mjs';

test('resume waits for state event after the command acknowledgement', async () => {
  const session = new EventEmitter();
  let paused = true;
  session.on('Debugger.resumed', () => { paused = false; });
  session.send = async () => { setTimeout(() => session.emit('Debugger.resumed'), 20); return {}; };
  synchronizeResume(session);
  await session.send('Debugger.resume');
  assert.equal(paused, false);
  assert.equal(session.listenerCount('Debugger.resumed'), 1);
});

test('resume accepts event before acknowledgement and preserves a subsequent pause', async () => {
  const session = new EventEmitter();
  let paused = true;
  session.on('Debugger.resumed', () => { paused = false; });
  session.on('Debugger.paused', () => { paused = true; });
  session.send = async () => { session.emit('Debugger.resumed'); session.emit('Debugger.paused'); return { accepted: true }; };
  synchronizeResume(session);
  assert.deepEqual(await session.send('Debugger.resume'), { accepted: true });
  assert.equal(paused, true);
  assert.equal(session.listenerCount('Debugger.resumed'), 1);
});

test('resume failure removes temporary listeners', async () => {
  const session = new EventEmitter();
  session.send = async () => { throw new Error('closed'); };
  synchronizeResume(session);
  await assert.rejects(session.send('Debugger.resume'), /closed/);
  assert.equal(session.listenerCount('Debugger.resumed'), 0);
});

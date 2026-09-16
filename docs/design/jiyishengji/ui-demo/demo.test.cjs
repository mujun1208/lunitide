// Run from the repository root: node docs/design/jiyishengji/ui-demo/demo.test.cjs
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { JSDOM } = require(require.resolve('jsdom', { paths: [path.resolve(__dirname, '../../../../web')] }));
const file = path.join(__dirname, 'index.html');

function open(t) {
  assert.ok(fs.existsSync(file), 'The interactive UI demo is missing');
  const dom = new JSDOM(fs.readFileSync(file, 'utf8'), {
    runScripts: 'dangerously',
    pretendToBeVisual: true,
    beforeParse(window) { window.scrollTo = () => {}; },
  });
  t.after(() => dom.window.close());
  const d = dom.window.document;
  function click(selector) {
    const element = d.querySelector(selector);
    assert.ok(element, `Missing element: ${selector}`);
    element.click();
  }
  function input(selector, value) {
    const element = d.querySelector(selector);
    assert.ok(element, `Missing element: ${selector}`);
    element.value = value;
    element.dispatchEvent(new dom.window.Event('input', { bubbles: true }));
  }
  return { d, w: dom.window, click, input };
}

test('The demo opens on a Lunitide-style home with no persistent player', t => {
  const { d } = open(t);
  assert.equal(d.querySelector('#main').dataset.page, 'home');
  assert.match(d.querySelector('.product-brand').textContent, /Lunitide/);
  assert.match(d.querySelector('#main').textContent, /今天想聊什么/);
  assert.equal(d.querySelector('#mini-player').hidden, true);
});

test('Media is a first-class office destination while activity stays out of primary navigation', t => {
  const { d } = open(t);
  assert.ok(d.querySelector('[data-nav="media"]'));
  assert.equal(d.querySelector('[data-nav="activity"]'), null);
  assert.ok(d.querySelector('#activity-trigger'));
  assert.ok(d.querySelector('[data-nav="settings"]'));
});

test('Compact layouts keep a usable navigation launcher', t => {
  const { d, click } = open(t);
  assert.ok(d.querySelector('#mobile-menu'));
  click('#mobile-menu');
  assert.match(d.querySelector('#drawer-body').textContent, /媒体中心/);
  click('[data-mobile-page="media"]');
  assert.equal(d.querySelector('#main').dataset.page, 'media');
  assert.equal(d.querySelector('#drawer').hidden, true);
});

test('Settings exposes exactly two fused smart capabilities', t => {
  const { d, click } = open(t);
  click('[data-nav="settings"]');
  assert.equal(d.querySelectorAll('.capability-card').length, 2);
  assert.match(d.querySelector('#main h1').textContent, /智能能力/);
  assert.match(d.querySelector('#main').textContent, /自动记忆/);
  assert.match(d.querySelector('#main').textContent, /文字识别/);
  assert.doesNotMatch(d.querySelector('#main').textContent, /模型辅助提取|历史与整理|待审阅/);
  assert.equal(d.querySelector('#memory-list'), null, 'overview must not render memory data');
  assert.equal(d.querySelector('#ocr-default'), null, 'overview must not render OCR diagnostics');
});

test('Memory uses one compact list and keeps configuration in a drawer', t => {
  const { d, click } = open(t);
  click('[data-nav="settings"]');
  click('[data-capability="memory"]');
  assert.equal(d.querySelector('#main').dataset.page, 'memory');
  assert.equal(d.querySelectorAll('[data-memory-row]').length, 4);
  assert.equal(d.querySelector('.tabs'), null);
  assert.equal(d.querySelector('[data-mode="auto"]'), null);
  click('#memory-settings');
  assert.equal(d.querySelectorAll('[data-mode]').length, 3);
  assert.match(d.querySelector('#drawer-body').textContent, /个人记忆/);
  assert.match(d.querySelector('#drawer-body').textContent, /项目记忆/);
  assert.doesNotMatch(d.querySelector('#drawer-body').textContent, /导出|清空|隐私与数据/);
  assert.equal(d.querySelector('.filter-button'), null);
  assert.equal(d.querySelectorAll('#drawer').length, 1);
  click('#drawer-close');
  click('[data-action="memory-advanced"]');
  assert.match(d.querySelector('#drawer-body').textContent, /最近变更/);
  assert.match(d.querySelector('#drawer-body').textContent, /导入、导出与清理/);
  assert.equal(d.querySelectorAll('#drawer').length, 1, 'settings, item and advanced views reuse one drawer');
});

test('Memory search filters rows and renders correction input as text', t => {
  const { d, click, input } = open(t);
  click('[data-nav="settings"]');
  click('[data-capability="memory"]');
  input('#memory-search', '简体中文');
  assert.equal(d.querySelectorAll('[data-memory-row]').length, 1);
  click('[data-memory-menu="m1"]');
  click('[data-action="edit-memory"]');
  input('#edit-text', '<img src=x onerror=alert(1)>');
  click('[data-confirm="edit-memory"]');
  assert.match(d.querySelector('#memory-list').textContent, /<img src=x/);
  assert.equal(d.querySelector('#memory-list img'), null);
});

test('Only the explicit valuable example is auto-saved once; greeting and weather are ignored', t => {
  const { d, click } = open(t);
  click('[data-example="weather"]');
  click('[data-example="hello"]');
  click('[data-example="valuable"]');
  click('[data-example="valuable"]');
  click('[data-nav="settings"]');
  click('[data-capability="memory"]');
  assert.equal(d.querySelectorAll('[data-memory-row]').length, 5);
});

test('The personal memory scope switch actually blocks personal auto-save', t => {
  const { d, click } = open(t);
  click('[data-nav="settings"]');
  click('[data-capability="memory"]');
  click('#memory-settings');
  click('[data-switch="personal"]');
  click('#drawer-close');
  click('[data-nav="home"]');
  click('[data-example="valuable"]');
  click('[data-nav="settings"]');
  click('[data-capability="memory"]');
  assert.equal(d.querySelectorAll('[data-memory-row]').length, 4);
});

test('OCR is an automatic status, does not fake Windows readiness, and hard-disables Paddle install', t => {
  const { d, click } = open(t);
  click('[data-nav="settings"]');
  click('[data-capability="ocr"]');
  assert.match(d.querySelector('#ocr-default').textContent, /文字识别 · 自动/);
  assert.match(d.querySelector('#ocr-default').textContent, /未连接 WinRT 探针/);
  assert.match(d.querySelector('#enhance-card').textContent, /复杂文档增强/);
  assert.match(d.querySelector('#enhance-state').textContent, /当前不可安装/);
  assert.equal(d.querySelector('#enhance-card button[disabled]').disabled, true);
  assert.equal(d.querySelector('[data-action="install-enhance"]'), null);
  click('[data-action="ocr-advanced"]');
  assert.match(d.querySelector('#drawer-body').textContent, /旧 PP-OCR 登记/);
});

test('Media starts empty, requires actual playback before MiniPlayer, and waits for a stop terminal', async t => {
  const { d, click } = open(t);
  click('[data-nav="media"]');
  assert.equal(d.querySelector('.media-stage'), null);
  assert.match(d.querySelector('#main').textContent, /无播放会话/);
  click('[data-action="start-media-demo"]');
  assert.ok(d.querySelector('.media-stage'));
  assert.equal(d.querySelector('#mini-player').hidden, true);
  click('[data-nav="home"]');
  assert.equal(d.querySelector('#mini-player').hidden, true, 'idle/load-only session must not create MiniPlayer');
  click('[data-nav="media"]');
  click('#media-play');
  assert.equal(d.querySelector('#mini-player').hidden, true, 'full player replaces mini player on media page');
  click('[data-nav="home"]');
  assert.equal(d.querySelector('#mini-player').hidden, false);
  assert.match(d.querySelector('#mini-player').textContent, /潮汐之间/);
  assert.match(d.querySelector('#mini-player').textContent, /1:24 \/ 4:00/);
  click('#mini-close');
  assert.equal(d.querySelector('#mini-player').hidden, false, 'player remains visible while stop is not terminal');
  assert.match(d.querySelector('#mini-meta').textContent, /正在停止/);
  await new Promise(resolve => d.defaultView.setTimeout(resolve, 220));
  assert.equal(d.querySelector('#mini-player').hidden, true);
  assert.match(d.querySelector('#toast').textContent, /停止已确认/);
});

test('Music and video are distinct media surfaces', t => {
  const { d, click } = open(t);
  click('[data-nav="media"]');
  click('[data-media-kind="video"]');
  assert.equal(d.querySelector('.video-theatre'), null);
  click('[data-action="start-media-demo"]');
  assert.ok(d.querySelector('.video-theatre'));
  assert.match(d.querySelector('#main').textContent, /视频空间/);
  assert.equal(d.querySelector('.media-stage'), null);
  const videoPlay = d.querySelector('#video-play');
  assert.match(videoPlay.getAttribute('aria-label'), /开始/);
  videoPlay.click();
  assert.match(d.querySelector('#video-play').getAttribute('aria-label'), /暂停/);
  click('[data-nav="home"]');
  assert.equal(d.querySelector('#mini-player').hidden, false);
  assert.match(d.querySelector('#mini-player').textContent, /海岸线 · 演示影片/);
});

test('Activity opens from the quiet status button and distinguishes sent from confirmed', t => {
  const { d, click } = open(t);
  click('#activity-trigger');
  assert.match(d.querySelector('#drawer-body').textContent, /已发送，待核验/);
  assert.match(d.querySelector('#drawer-body').textContent, /已确认/);
  assert.equal(d.querySelector('#drawer').hidden, false);
});

test('Theme switch, escape focus return, and offline policy remain intact', t => {
  const { d, click, w } = open(t);
  assert.equal(d.documentElement.dataset.theme, 'dark');
  click('#theme-toggle');
  assert.equal(d.documentElement.dataset.theme, 'light');
  const trigger = d.querySelector('#activity-trigger');
  trigger.focus();
  trigger.click();
  d.dispatchEvent(new w.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
  assert.equal(d.querySelector('#drawer').hidden, true);
  assert.equal(d.activeElement, trigger);
  assert.match(d.querySelector('meta[http-equiv="Content-Security-Policy"]').content, /connect-src 'none'/);
});

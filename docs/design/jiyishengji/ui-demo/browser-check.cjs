// Requires Playwright and installed Edge. No browser/package installation is performed.
// Supply NODE_PATH when Playwright comes from a shared runtime.
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const path = require('node:path');
const { pathToFileURL } = require('node:url');

(async () => {
  const browser = await chromium.launch({ headless: true, channel: 'msedge' });
  try {
    const page = await browser.newPage({ viewport: { width: 1600, height: 1000 } });
    const errors = [], externalRequests = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('request', request => { if (/^https?:/.test(request.url())) externalRequests.push(request.url()); });
    await page.goto(pathToFileURL(path.join(__dirname, 'index.html')).href);

    const darkColors = await page.evaluate(() => ({
      body: getComputedStyle(document.body).backgroundColor,
      sidebar: getComputedStyle(document.querySelector('.sidebar')).backgroundColor,
    }));
    assert.equal(darkColors.body, 'rgb(0, 0, 0)');
    assert.equal(darkColors.sidebar, 'rgb(3, 4, 6)');
    assert.equal(await page.locator('.home-card').count(), 3);
    assert.equal(await page.locator('.home-center h1').count(), 1);
    assert.equal(await page.locator('#mini-player').isHidden(), true);
    await page.screenshot({ path: path.join(__dirname, 'preview-home-dark.png') });

    await page.click('#theme-toggle');
    await page.screenshot({ path: path.join(__dirname, 'preview-home-light.png') });
    await page.click('#theme-toggle');

    await page.click('[data-nav="settings"]');
    assert.equal(await page.locator('.capability-card').count(), 2);
    await page.screenshot({ path: path.join(__dirname, 'preview-settings.png') });

    await page.click('[data-capability="memory"]');
    assert.equal(await page.locator('[data-memory-row]').count(), 4);
    assert.equal(await page.locator('.tabs').count(), 0);
    await page.click('#memory-settings');
    assert.equal(await page.locator('[data-mode]').count(), 3);
    await page.keyboard.press('Escape');
    assert.equal(await page.locator('#memory-settings').evaluate(element => element === document.activeElement), true);
    await page.screenshot({ path: path.join(__dirname, 'preview-memory-dark.png') });

    await page.evaluate(() => document.querySelector('[data-nav="settings"]').click());
    await page.click('[data-capability="ocr"]');
    assert.match(await page.locator('#ocr-default').innerText(), /文字识别 · 自动/);
    assert.match(await page.locator('#ocr-default').innerText(), /未连接 WinRT 探针/);
    assert.match(await page.locator('#enhance-state').innerText(), /当前不可安装/);
    assert.equal(await page.locator('#enhance-card button[disabled]').count(), 1);
    assert.equal(await page.locator('[data-action="install-enhance"]').count(), 0);
    await page.click('[data-action="ocr-advanced"]');
    assert.match(await page.locator('#drawer-body').innerText(), /旧 PP-OCR 登记/);
    await page.keyboard.press('Escape');
    await page.screenshot({ path: path.join(__dirname, 'preview-ocr.png') });

    await page.click('[data-nav="media"]');
    assert.equal(await page.locator('.media-stage').count(), 0);
    assert.match(await page.locator('#main').innerText(), /无播放会话/);
    await page.click('[data-action="start-media-demo"]');
    assert.equal(await page.locator('.media-stage').count(), 1);
    await page.click('[data-nav="home"]');
    assert.equal(await page.locator('#mini-player').isHidden(), true, 'load-only media must not create MiniPlayer');
    await page.click('[data-nav="media"]');
    await page.screenshot({ path: path.join(__dirname, 'preview-media.png') });
    await page.click('#media-play');
    await page.click('[data-nav="home"]');
    assert.equal(await page.locator('#mini-player').isVisible(), true);
    assert.match(await page.locator('#mini-player').innerText(), /1:24 \/ 4:00/);
    await page.click('#mini-play');
    assert.equal(await page.locator('#mini-player').isVisible(), true, 'paused media session remains resumable');
    await page.screenshot({ path: path.join(__dirname, 'preview-mini-player.png') });
    await page.click('#mini-close');
    assert.equal(await page.locator('#mini-player').isVisible(), true, 'stop must remain visible until terminal');
    assert.match(await page.locator('#mini-meta').innerText(), /正在停止/);
    await page.locator('#mini-player').waitFor({ state: 'hidden' });
    assert.match(await page.locator('#toast').innerText(), /停止已确认/);

    await page.click('[data-nav="media"]');
    await page.click('[data-media-kind="video"]');
    assert.equal(await page.locator('.video-theatre').count(), 0);
    await page.click('[data-action="start-media-demo"]');
    assert.equal(await page.locator('.video-theatre').count(), 1);
    const initialVideoLabel = await page.locator('#video-play').getAttribute('aria-label');
    assert.match(initialVideoLabel, /开始/);
    await page.click('#video-play');
    assert.match(await page.locator('#video-play').getAttribute('aria-label'), /暂停/);
    await page.screenshot({ path: path.join(__dirname, 'preview-video.png') });

    await page.click('#activity-trigger');
    assert.match(await page.locator('#drawer-body').innerText(), /已发送，待核验/);
    assert.match(await page.locator('#drawer-body').innerText(), /已确认/);
    await page.keyboard.press('Escape');

    for (const width of [1600, 1024, 820, 620, 390, 320]) {
      await page.setViewportSize({ width, height: width <= 620 ? 844 : 900 });
      for (const name of ['home','settings','memory','ocr','media']) {
        await page.evaluate(target => {
          if (target === 'memory' || target === 'ocr') {
            document.querySelector('[data-nav="settings"]').click();
            document.querySelector('[data-capability="' + target + '"]').click();
          } else document.querySelector('[data-nav="' + target + '"]').click();
        }, name);
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true, name + ' overflows at ' + width + 'px');
      }
    }

    await page.setViewportSize({ width: 390, height: 844 });
    await page.evaluate(() => document.querySelector('[data-nav="home"]').click());
    assert.equal(await page.locator('#mobile-menu').isVisible(), true);
    await page.click('#mobile-menu');
    await page.click('[data-mobile-page="settings"]');
    assert.equal(await page.locator('#main').getAttribute('data-page'), 'settings');
    await page.click('#mobile-menu');
    await page.click('[data-mobile-page="home"]');
    await page.screenshot({ path: path.join(__dirname, 'preview-mobile.png') });
    await page.evaluate(() => document.querySelector('[data-nav="media"]').click());
    await page.click('[data-media-kind="music"]');
    await page.click('[data-action="start-media-demo"]');
    await page.click('#media-play');
    await page.evaluate(() => document.querySelector('[data-nav="home"]').click());
    const miniBox = await page.locator('#mini-player').boundingBox();
    assert.ok(miniBox.x >= 0 && miniBox.x + miniBox.width <= 390);
    assert.ok(miniBox.y >= 0 && miniBox.y + miniBox.height <= 844);

    const unnamedIconButtons = await page.evaluate(() => Array.from(document.querySelectorAll('button')).filter(button => {
      const visible = button.getClientRects().length > 0;
      const onlyIcon = visible && button.querySelector('svg') && !button.textContent.trim();
      return onlyIcon && !button.getAttribute('aria-label') && !button.getAttribute('title');
    }).length);
    assert.equal(unnamedIconButtons, 0);
    assert.deepEqual(errors, []);
    assert.deepEqual(externalRequests, []);
    console.log('PASS: Edge rendered 5 destinations at 6 widths; pure-black brand shell, simple settings, memory/OCR flows, media/video/mini-player, focus return, accessible icon labels, no JS errors or external requests.');
  } finally {
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });

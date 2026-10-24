import { test, expect } from '@playwright/test';
import { pathToFileURL } from 'node:url';
import { resolve } from 'node:path';

const url = pathToFileURL(resolve('webroot/index.html')).href;
async function mockBridge(page) {
  await page.addInitScript(() => {
    window.requests = [];
    const config = { enabled: true, kernel_tuning:true, default_profile:'daily', apps: {}, protected: {} };
    let ai = { configured: false, enabled: false };
    let learning = {enabled:true,adaptive:true,daily_ai:false,hour:21,timezone:'Asia/Ho_Chi_Minh',charging_only:false};
    const report = { source: 'local-rules', metrics: { MemTotal_kib: 8000000, MemAvailable_kib: 3000000, zram_original_bytes: 1200000000, zram_allocated_bytes: 480000000, battery_percent: 80, battery_celsius: 35 }, advice: ['Một mẫu chưa đủ kết luận pin chờ.'], ntsync: false, legacy_conflict: false };
    window.ksu = { exec(command, options, callback) {
      const encoded = JSON.parse(options).env.HOUJI_REQUEST_B64;
      const req = JSON.parse(new TextDecoder().decode(Uint8Array.from(atob(encoded), c => c.charCodeAt(0))));
      window.requests.push({ command, req });
      if(req.action==='export'&&window.exportFailure){setTimeout(()=>window[callback](1,JSON.stringify({ok:false,error:'cannot access Downloads/HNHouji; unlock shared storage and try again'}),''),0);return;}
      let data = {};
      switch (req.action) {
        case 'learning_status': data={config:learning,history:{days:{'2026-10-25':{observed_seconds:600,active_seconds:300,samples:10,hours:Array.from({length:24},(_,h)=>({active_seconds:h===20?300:0}))}},through:'2026-10-25T13:00:00Z'},apps:[{id:'app-01',package:'com.example.game',seconds:300,days:1,sessions:2}],daily:{}};break;
        case 'learning_preview': data={apps:[{id:'app-01',seconds:300}],observed_seconds:600};break;
        case 'learning_save': learning=req.learning;break;
        case 'learning_analyze': data={at:'2026-10-25T13:00:00Z',legend:[{id:'app-01',package:'com.example.game'}],result:{source:'api',answer:'app-01: <img src=x onerror="window.pwned=true"> xem xét daily',usage:{total_tokens:100},finish_reason:'stop',model:'actual-model',elapsed_ms:20}};break;
        case 'status': data = { runtime: { profile: window.kernelMismatch?'observe':config.enabled?'daily':'observe', reason: window.kernelMismatch?'kernel-build-mismatch':config.enabled?'daily-default':'disabled' }, kernel_sync:{ready:!window.kernelMismatch,message:window.kernelMismatch?'Build ID không khớp; chỉ chẩn đoán.':'Release, IKCONFIG và build ID khớp.'}, kernel_tuning:{knobs:[{label:'ZRAM: đọc từng trang swap',current:config.kernel_tuning?'0':'3',target:'0',before:'3',state:window.kernelMismatch?'waiting':config.kernel_tuning?'applied':'waiting',detail:'Chỉ dùng với ZRAM.'}]}, watcher_running: true, config, report, ai }; break;
        case 'kernel_tuning_set': config.kernel_tuning=req.enabled; break;
        case 'packages': data = ['com.example.chat', 'com.example.game']; break;
        case 'ai_status': data = ai; break;
        case 'ai_preview': data = report; break;
        case 'ai_save': ai = { ...req.ai, key: undefined, configured: true, has_key: true }; data = ai; break;
        case 'ai_test': data = { source: 'api', answer: 'Kết nối hoạt động.', usage: {}, elapsed_ms: 12 }; break;
        case 'ai_ask': data = window.truncatedAI ? {source:'local-rules',ai_error:'response-truncated',report,usage:{completion_tokens:512},elapsed_ms:20,finish_reason:'length',model:'routed-model'} : ai.enabled ? { source: 'api', answer: '<img src=x onerror="window.pwned=true"> $(reboot)', usage: {}, elapsed_ms: 15 } : { source: 'local-rules', ai_error: 'not-configured', report }; break;
        case 'ai_disable': ai.enabled = false; break;
        case 'ai_clear': ai = { configured: false, enabled: false }; break;
        case 'app_set': config.apps[req.package] = req.profile; break;
        case 'protect_set': config.protected[req.package] = req.protected; break;
        case 'enable': config.enabled = true; break;
        case 'disable': config.enabled = false; break;
        case 'doctor': data = { checks: { compatibility: 'supported' } }; break;
        case 'export': data = { path: '/storage/emulated/0/Download/HNHouji/HNHouji-report-20261024-130000-abcdef123456.json' }; break;
      }
      setTimeout(() => window[callback](0, JSON.stringify({ ok: true, data }), ''), 0);
    } };
  });
}

test('works without native bridge and never invents metrics', async ({ page }) => {
  await page.goto(url);
  await expect(page.locator('#bridge-warning')).toBeVisible();
  await expect(page.locator('#ram-value')).toHaveText('—');
  await expect(page.locator('#profile-name')).toHaveText('Chưa kết nối');
});

test('habits opt in to scheduling, show coverage and keep AI suggestions for review', async ({page})=>{
  await mockBridge(page);await page.goto(url);
  await page.getByRole('tab',{name:'Thói quen',exact:true}).click();
  await expect(page.locator('#learning-enabled')).toBeChecked();
  await expect(page.locator('#learning-daily')).not.toBeChecked();
  await expect(page.locator('#learning-coverage')).toContainText('10 mẫu');
  await expect(page.locator('#learning-hours meter')).toHaveCount(24);
  await expect(page.locator('#learning-preview')).not.toContainText('com.example');
  await expect(page.locator('#learning-apps')).toContainText('com.example.game');
  expect(await page.evaluate(()=>window.requests.some(x=>x.req.action==='learning_analyze'))).toBeFalsy();
  await page.locator('#learning-daily').check();await page.locator('#learning-hour').fill('22');
  await page.getByRole('button',{name:'Lưu cài đặt thói quen',exact:true}).click();
  await expect.poll(()=>page.evaluate(()=>window.requests.some(x=>x.req.action==='learning_save'&&x.req.learning.daily_ai&&x.req.learning.hour===22))).toBeTruthy();
  await page.locator('#learning-analyze').click();await expect(page.locator('#learning-answer')).toContainText('xem xét daily');
  await expect(page.locator('#learning-answer img')).toHaveCount(0);
  await expect(page.locator('#learning-legend')).toContainText('app-01: com.example.game');
  expect(await page.evaluate(()=>window.requests.some(x=>x.req.action==='app_set'))).toBeFalsy();
  expect(await page.evaluate(()=>!!window.pwned)).toBeFalsy();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBeTruthy();
  await page.screenshot({path:'dist/screenshots/habits-mobile.png',fullPage:true});
});

test('AI truncation explains fallback and exposes model and finish status', async ({page})=>{
  await mockBridge(page);await page.goto(url);await page.evaluate(()=>{window.truncatedAI=true;});
  await page.getByRole('tab',{name:'Trợ lý AI',exact:true}).click();await page.locator('#ai-ask').click();
  await expect(page.locator('#ai-answer')).toContainText('hết giới hạn token');
  await expect(page.locator('#ai-answer')).toContainText('Kết thúc: length');
  await expect(page.locator('#ai-answer')).toContainText('routed-model');
  expect(await page.evaluate(()=>window.requests.filter(x=>x.req.action==='ai_ask').length)).toBe(1);
});

test('kernel preset shows values, toggles separately and explains build mismatch', async ({page})=>{
  await mockBridge(page);await page.goto(url);
  await expect(page.locator('#kernel-tuning')).toBeChecked();
  await expect(page.locator('#kernel-knobs')).toContainText('Module đã áp dụng');
  await expect(page.locator('#kernel-knobs')).toContainText('Hiện tại: 0 · Mục tiêu: 0 · Ban đầu: 3');
  await page.locator('#kernel-tuning').uncheck();
  await expect.poll(()=>page.evaluate(()=>window.requests.some(x=>x.req.action==='kernel_tuning_set'&&x.req.enabled===false))).toBeTruthy();
  await expect(page.locator('#kernel-knobs')).toContainText('Hiện tại: 3');
  await expect(page.locator('#profile-name')).toHaveText('Hằng ngày');
  await page.evaluate(()=>{window.kernelMismatch=true;});
  await page.locator('#enable').click();
  await expect(page.locator('#profile-name')).toHaveText('Quan sát');
  await expect(page.locator('#profile-detail')).toContainText('Build ID không khớp');
  await expect(page.locator('#kernel-sync')).toContainText('chỉ chẩn đoán');
});

test('mobile tabs, app rules, optional AI and safe output', async ({ page }) => {
  await mockBridge(page);
  const errors = []; page.on('pageerror', error => errors.push(error.message));
  await page.goto(url);
  await expect(page.locator('#profile-name')).toHaveText('Hằng ngày');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBeTruthy();
  await page.getByRole('tab', { name: 'Ứng dụng', exact: true }).click();
  await page.locator('#load-apps').click();
  await page.getByRole('combobox', { name: 'Profile com.example.game' }).selectOption('native-game');
  await expect.poll(() => page.evaluate(() => window.requests.some(x => x.req.action === 'app_set' && x.req.profile === 'native-game'))).toBeTruthy();
  await page.getByRole('tab', { name: 'Trợ lý AI', exact: true }).click();
  await page.locator('#ai-ask').click();
  await expect(page.locator('#ai-answer')).toContainText('Báo cáo cơ bản');
  await page.locator('#api-url').fill('https://example.test/v1');
  await page.locator('#api-model').fill("model'$(id)");
  await page.locator('#api-key').fill("secret'$(reboot);");
  await page.getByRole('button', { name: 'Lưu cấu hình', exact: true }).click();
  await expect(page.locator('#api-key')).toHaveValue('');
  expect(await page.evaluate(() => window.requests.filter(x => x.req.action === 'ai_save').every(x => !x.command.includes('secret') && x.req.ai.key === "secret'$(reboot);"))).toBeTruthy();
  expect(await page.evaluate(() => window.requests.filter(x => ['ai_test', 'ai_ask'].includes(x.req.action)).length)).toBe(1);
  await page.locator('#ai-ask').click();
  await expect(page.locator('#ai-answer')).toContainText('<img src=x');
  await expect(page.locator('#ai-answer img')).toHaveCount(0);
  expect(await page.evaluate(() => !!window.pwned)).toBeFalsy();
  expect(await page.evaluate(() => localStorage.length)).toBe(0);
  await page.screenshot({ path: 'dist/screenshots/ai-mobile.png', fullPage: true });
  expect(errors).toEqual([]);
});

test('overview light and dark layouts and external guide isolation', async ({ page }) => {
  await mockBridge(page);
  await page.goto(url);
  await expect(page.locator('#battery-value')).toHaveText('80%');
  await page.screenshot({ path: 'dist/screenshots/overview-mobile.png', fullPage: true });
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.screenshot({ path: 'dist/screenshots/overview-dark.png', fullPage: true });
  await page.setViewportSize({ width: 1000, height: 900 });
  await page.screenshot({ path: 'dist/screenshots/overview-desktop.png', fullPage: true });
  await page.getByRole('tab', { name: 'Tiện ích', exact: true }).click();
  await page.locator('[data-guide="localsend"]').click();
  expect(page.url()).toBe(url);
  expect(await page.evaluate(() => window.requests.some(x => x.req.action === 'guide_open' && x.req.guide === 'localsend'))).toBeTruthy();
});

test('backgrounded WebUI does not poll', async ({ page }) => {
  await mockBridge(page); await page.goto(url);
  await expect(page.locator('#battery-value')).toHaveText('80%');
  await page.evaluate(() => { Object.defineProperty(document, 'hidden', { configurable: true, value: true }); document.dispatchEvent(new Event('visibilitychange')); });
  const count = await page.evaluate(() => window.requests.length);
  await page.waitForTimeout(5400);
  expect(await page.evaluate(() => window.requests.length)).toBe(count);
});

test('export tells user where to retrieve the public report and surfaces failure', async ({page})=>{
  await mockBridge(page);await page.goto(url);
  await page.getByRole('tab',{name:'Chẩn đoán',exact:true}).click();
  await page.locator('#export').click();
  await expect(page.locator('#notification')).toContainText('Bộ nhớ trong → Download → HNHouji');
  await expect(page.locator('#notification')).toContainText('/storage/emulated/0/Download/HNHouji/');
  await expect(page.locator('#notification')).not.toContainText('/data/adb');
  await page.evaluate(()=>{window.exportFailure=true;});
  await page.locator('#export').click();
  await expect(page.locator('#notification')).toHaveClass(/error/);
  await expect(page.locator('#notification')).toContainText('cannot access Downloads');
  await expect(page.locator('#notification')).not.toContainText('Đã lưu');
});

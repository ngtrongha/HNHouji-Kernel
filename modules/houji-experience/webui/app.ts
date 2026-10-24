import { available, rpc, type RPC } from "./bridge";

const $ = <T extends HTMLElement = HTMLElement>(id: string): T => {
  const node = document.getElementById(id);
  if (!node) throw new Error(`Missing UI element: ${id}`);
  return node as T;
};
const input = (id: string) => $<HTMLInputElement>(id);
const select = (id: string) => $<HTMLSelectElement>(id);
const text = (id: string, value: string) => { $(id).textContent = value; };
const pretty = (data: unknown) => JSON.stringify(data, null, 2);
let tab = "overview";
let latest: any = null;
let packages: string[] = [];
let refreshing = false;
let timer: ReturnType<typeof setTimeout> | undefined;
const labels: Record<string, string> = { observe: "Quan sát", daily: "Hằng ngày", "native-game": "Game Android", "windows-game": "Game Windows", linux: "Linux", sleep: "Màn hình nghỉ" };
const reasons: Record<string, string> = { "daily-default": "Hằng ngày mặc định: áp dụng preset kernel có hỗ trợ và các quy tắc ứng dụng đã chọn.", disabled: "Tự động đang tắt; không áp dụng tinh chỉnh.", "rules-local": "Hoạt động bằng quy tắc cục bộ, không gọi AI.", "integration-observe": "Theo dõi ứng dụng; cấu hình đồ họa/container nằm trong ứng dụng đó.", "game-mode-unsupported": "GameManager chưa hỗ trợ performance cho ứng dụng này.", "legacy-conflict": "Helper cũ xuất hiện. Xử lý trong Chẩn đoán rồi reboot.", "restore-conflict": "Có thay đổi bên ngoài hoặc trạng thái chưa khôi phục được. Xem Chẩn đoán.", "reboot-required": "Reboot sau khi xử lý helper cũ trước khi bật tự động.", "owner-user-only": "Chỉ quản lý người dùng chính (user 0).", "watcher-offline": "Watcher chưa chạy. Reboot sau cài đặt và kiểm tra Manager.", "not-started": "Chưa khởi động watcher." };

function notify(message: string, error = false) { const box = $("notification"); box.textContent = message; box.classList.toggle("error", error); box.hidden = false; }
function failure(error: unknown) { notify(error instanceof Error ? error.message : "Không thể hoàn thành thao tác.", true); }
function metric(value: unknown, suffix: string, divisor = 1, digits = 0): string { return typeof value === "number" && Number.isFinite(value) ? `${(value / divisor).toLocaleString("vi-VN", { maximumFractionDigits: digits })}${suffix}` : "—"; }
function fillAdvice(report: any) {
  const list = $("local-advice"); list.replaceChildren();
  for (const advice of report.advice || []) { const li = document.createElement("li"); li.textContent = String(advice); list.append(li); }
}

function render(status: any) {
  latest = status;
  const metrics = status.report?.metrics || {};
  text("profile-name", labels[status.runtime?.profile] || "Quan sát");
  const reason = status.runtime?.reason || "";
  let detail = (reason.startsWith("kernel-") ? (reason === "kernel-setting-failed" ? "Tinh chỉnh kernel bị lỗi hoặc có thay đổi bên ngoài; đã dừng thử lại. Xem Chẩn đoán, xử lý rồi bật lại." : status.kernel_sync?.message) : reasons[reason]) || reason || "Chưa có trạng thái";
  if (reason === "daily-default" && !status.config?.kernel_tuning) detail = "Hằng ngày · preset kernel đang tắt; quy tắc ứng dụng vẫn hoạt động.";
  text("profile-detail", detail + (status.runtime?.package ? ` · ${status.runtime.package}` : ""));
  text("kernel-sync", status.kernel_sync?.message || "Chưa có kết quả xác minh kernel.");
  input("kernel-tuning").checked = !!status.config?.kernel_tuning;
  const knobs = $("kernel-knobs"); knobs.replaceChildren();
  const states: Record<string,string> = { available:"Chờ áp dụng", waiting:"Chưa áp dụng", unsupported:"Bỏ qua · thiếu điều kiện hỗ trợ", applied:"Module đã áp dụng", "already-set":"Đã có sẵn · không ghi", "pending-restore":"Chờ hoàn tác", conflict:"Xung đột · giữ thay đổi bên ngoài" };
  for (const knob of status.kernel_tuning?.knobs || []) {
    const row = document.createElement("div"); row.className = "app-row";
    const title = document.createElement("strong"); title.textContent = knob.label;
    const value = document.createElement("p"); value.textContent = `${states[knob.state] || knob.state} · Hiện tại: ${knob.current || "—"} · Mục tiêu: ${knob.target}${knob.before ? ` · Ban đầu: ${knob.before}` : ""}`;
    const detail = document.createElement("small"); detail.textContent = knob.detail;
    const content = document.createElement("div"); content.append(title,value,detail); row.append(content); knobs.append(row);
  }
  text("watcher-state", status.watcher_running ? "Watcher đang chạy" : "Watcher chưa chạy");
  text("ram-value", metric(metrics.MemAvailable_kib, " GB", 1024 * 1024, 2));
  text("ram-total", `Tổng: ${metric(metrics.MemTotal_kib, " GB", 1024 * 1024, 2)}`);
  text("zram-value", metric(metrics.zram_allocated_bytes, " MB", 1024 * 1024));
  text("zram-detail", `Dữ liệu gốc: ${metric(metrics.zram_original_bytes, " MB", 1024 * 1024)}`);
  text("temperature-value", metric(metrics.battery_celsius, " °C", 1, 1));
  text("battery-value", metric(metrics.battery_percent, "%"));
  fillAdvice(status.report || {});
	if(status.restore_conflicts>0 || (!status.config?.enabled && status.journal_entries>0)) notify("Còn thay đổi chưa khôi phục. Mở Chẩn đoán để xem pending_restore; giá trị bên ngoài được giữ nguyên.",true);
  text("ai-state", status.ai?.configured ? (status.ai.enabled ? "Đã cấu hình · gọi theo yêu cầu" : "Đã cấu hình · đang tắt") : "Chưa cấu hình · core vẫn hoạt động");
}

async function refresh() {
  if (!available() || document.hidden || refreshing) return;
  refreshing = true;
  try { render(await rpc({ action: "status" })); } catch (error) { failure(error); }
  finally { refreshing = false; }
}
function schedule() { if (timer) clearTimeout(timer); if (document.hidden || !available()) return; timer = setTimeout(async () => { await refresh(); schedule(); }, 5000); }

async function act(request: RPC, message?: string) { const data = await rpc(request); if (message) notify(message); await refresh(); return data; }
function bind(id: string, callback: () => Promise<void>) {
  $(id).addEventListener("click", async () => { const button = $<HTMLButtonElement>(id); button.disabled = true; try { await callback(); } catch (error) { failure(error); } finally { button.disabled = false; } });
}

function showTab(id: string) {
  if (!["overview", "apps", "ai", "tools", "diagnostics"].includes(id)) return;
  tab = id;
  for (const button of document.querySelectorAll<HTMLButtonElement>("[data-tab]")) { const selected = button.dataset.tab === id; button.setAttribute("aria-selected", String(selected)); button.tabIndex = selected ? 0 : -1; }
  for (const section of document.querySelectorAll<HTMLElement>("main > section")) section.hidden = section.id !== id;
  if (id === "ai") void loadAI().catch(failure);
}

async function loadAI() {
  const status = await rpc<any>({ action: "ai_status" });
  // Only populate on entering the page, never overwrite a form on background refresh.
  if (status.configured) {
    select("api-style").value = status.style; select("api-auth").value = status.auth;
    input("api-url").value = status.base_url; input("api-model").value = status.model;
    input("api-limit").value = String(status.max_output_tokens); input("api-timeout").value = String(status.timeout_seconds);
    input("api-enabled").checked = status.enabled;
    input("api-key").placeholder = status.has_key ? "Đã lưu. Để trống để giữ khóa cùng endpoint." : "Nhập token nếu endpoint yêu cầu";
  }
  input("api-key").value = "";
  input("api-key").disabled = select("api-auth").value === "none";
  text("ai-preview", pretty(await rpc({ action: "ai_preview" })));
}

function renderApps() {
  const query = input("app-search").value.toLowerCase();
  const filtered = packages.filter(p => p.toLowerCase().includes(query));
  const container = $("app-list"); container.replaceChildren();
  const count = document.createElement("p"); count.className = "footnote"; count.textContent = `${filtered.length} package · hiển thị tối đa 80 kết quả; tìm để thu hẹp.`; container.append(count);
  for (const pkg of filtered.slice(0, 80)) {
    const row = document.createElement("div"); row.className = "app-row";
    const name = document.createElement("div"); name.className = "app-name"; name.textContent = pkg;
    const controls = document.createElement("div"); controls.className = "app-controls";
    const profile = document.createElement("select"); profile.setAttribute("aria-label", `Profile ${pkg}`);
    for (const [value, title] of [["", "Không có quy tắc"], ["daily", "Hằng ngày"], ["native-game", "Game Android"], ["windows-game", "Game Windows"], ["linux", "Linux"]]) { const option = document.createElement("option"); option.value = value; option.textContent = title; profile.append(option); }
    profile.value = latest?.config?.apps?.[pkg] || "";
    profile.addEventListener("change", async () => { profile.disabled = true; try { await act({ action: profile.value ? "app_set" : "app_remove", package: pkg, profile: profile.value }, "Đã lưu quy tắc ứng dụng."); } catch (error) { failure(error); profile.value = latest?.config?.apps?.[pkg] || ""; } finally { profile.disabled = false; } });
    const label = document.createElement("label"); label.className = "check";
    const protect = document.createElement("input"); protect.type = "checkbox"; protect.checked = !!latest?.config?.protected?.[pkg];
    protect.addEventListener("change", async () => { protect.disabled = true; try { await act({ action: "protect_set", package: pkg, protected: protect.checked }, "Đã lưu lựa chọn thông báo."); } catch (error) { failure(error); protect.checked = !!latest?.config?.protected?.[pkg]; } finally { protect.disabled = false; } });
    label.append(protect, document.createTextNode("Bảo vệ thông báo")); controls.append(profile, label); row.append(name, controls); container.append(row);
  }
}

for (const button of document.querySelectorAll<HTMLButtonElement>("[data-tab]")) {
  button.addEventListener("click", () => showTab(button.dataset.tab!));
  button.addEventListener("keydown", (event) => { if (event.key !== "ArrowRight" && event.key !== "ArrowLeft") return; const tabs = [...document.querySelectorAll<HTMLButtonElement>("[data-tab]")]; const delta = event.key === "ArrowRight" ? 1 : -1; const next = tabs[(tabs.indexOf(button) + delta + tabs.length) % tabs.length]; next.click(); next.focus(); event.preventDefault(); });
}
for (const button of document.querySelectorAll<HTMLButtonElement>("[data-open]")) button.addEventListener("click", () => showTab(button.dataset.open!));
for (const button of document.querySelectorAll<HTMLButtonElement>("[data-guide]")) button.addEventListener("click", () => { void rpc({ action: "guide_open", guide: button.dataset.guide }).catch(failure); });

bind("enable", async () => { await act({ action: "enable" }, "Đã bật quy tắc tự động. Profile cập nhật sau hai mẫu foreground."); });
input("kernel-tuning").addEventListener("change", async () => {
  const checkbox = input("kernel-tuning"); checkbox.disabled = true;
  try { await act({action:"kernel_tuning_set",enabled:checkbox.checked}, checkbox.checked ? "Đã bật preset kernel; áp dụng khi xác minh đạt và tự động đang bật." : "Đã tắt preset kernel và yêu cầu hoàn tác."); }
  catch(error) { failure(error); await refresh(); } finally { checkbox.disabled = false; }
});
for (const id of ["disable", "restore"]) bind(id, async () => { await act({ action: "disable" }, "Đã tắt và hoàn tác các thay đổi thuộc module."); });
bind("migrate", async () => { await act({ action: "migrate_legacy" }, "Đã lưu trạng thái helper. Reboot trước khi bật tự động."); });
bind("resolve-conflicts", async () => { await act({action:"resolve_conflicts"},"Đã giữ giá trị bên ngoài của các mục xung đột. Tự động đang tắt; kiểm tra lại trước khi bật.");text("doctor-result",pretty(await rpc({action:"doctor"}))); });
bind("load-apps", async () => { packages = (await rpc<string[]>({ action: "packages" })).sort(); await refresh(); renderApps(); });
input("app-search").addEventListener("input", renderApps);
bind("doctor", async () => { text("doctor-result", pretty(await rpc({ action: "doctor" }))); });
bind("export", async () => { const result = await rpc<{ path: string }>({ action: "export" }); notify(`Đã lưu báo cáo sạch trong Downloads/HNHouji. Mở Trình quản lý tệp → Bộ nhớ trong → Download → HNHouji. Tệp: ${result.path}`); });

$("api-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const save = $("api-form").querySelector<HTMLButtonElement>('button[type="submit"]')!; save.disabled = true;
  try {
    await act({ action: "ai_save", ai: { enabled: input("api-enabled").checked, style: select("api-style").value, auth: select("api-auth").value, base_url: input("api-url").value.trim(), model: input("api-model").value.trim(), key: input("api-key").value, max_output_tokens: Number(input("api-limit").value), timeout_seconds: Number(input("api-timeout").value) } }, "Đã lưu cấu hình. Chưa gọi API; bấm kiểm tra khi cần.");
  } catch (error) { failure(error); } finally { input("api-key").value = ""; save.disabled = false; }
});
select("api-auth").addEventListener("change", () => { input("api-key").disabled = select("api-auth").value === "none"; if (input("api-key").disabled) input("api-key").value = ""; });

function showAnswer(result: any) {
  const answer = $("ai-answer"); answer.hidden = false;
  // Never render provider output as HTML, Markdown HTML, JS or shell commands.
  answer.textContent = result.source === "api" ? `${result.answer}\n\n${result.elapsed_ms} ms · ${pretty(result.usage)}` : `Báo cáo cơ bản (${result.ai_error || "không dùng AI"})\n\n${(result.report?.advice || []).join("\n\n")}`;
}
bind("api-test", async () => { showAnswer(await rpc({ action: "ai_test" })); });
bind("ai-ask", async () => { text("ai-preview", pretty(await rpc({ action: "ai_preview" }))); showAnswer(await rpc({ action: "ai_ask", prompt: $<HTMLTextAreaElement>("ai-prompt").value })); });
bind("api-disable", async () => { await act({ action: "ai_disable" }, "AI đã tắt. Core không bị ảnh hưởng."); await loadAI(); });
bind("api-clear", async () => { await act({ action: "ai_clear" }, "Đã xóa cấu hình và khóa API."); $<HTMLFormElement>("api-form").reset(); input("api-key").value = ""; });

document.addEventListener("visibilitychange", () => { if (document.hidden) { if (timer) clearTimeout(timer); input("api-key").value = ""; } else { void refresh(); schedule(); } });
window.addEventListener("pagehide", () => { if (timer) clearTimeout(timer); input("api-key").value = ""; });
if (!available()) { $("bridge-warning").hidden = false; text("watcher-state", "Không có bridge"); }
else { void refresh().then(schedule); }

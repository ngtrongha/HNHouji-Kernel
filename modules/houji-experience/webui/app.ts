import { available, rpc, type RPC } from "./bridge";
import { markdown } from "./answer";

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
let goal="daily";
let aiJob:any=null;
let aiPolling=false;
let aiPollTimer:ReturnType<typeof setTimeout>|undefined;
let aiClock:ReturnType<typeof setInterval>|undefined;
let aiStarting=false;
let renderedJob="";
let conversation:{role:string;content:string}[]=[];
let lastApplication:any={};
const labels: Record<string, string> = { observe: "Quan sát", daily: "Hằng ngày", "native-game": "Game Android", "windows-game": "Game Windows", linux: "Linux", sleep: "Màn hình nghỉ" };
const reasons: Record<string, string> = { "daily-default": "Hằng ngày mặc định: áp dụng preset kernel có hỗ trợ và các quy tắc ứng dụng đã chọn.", disabled: "Tự động đang tắt; không áp dụng tinh chỉnh.", "rules-local": "Hoạt động bằng quy tắc cục bộ, không gọi AI.", "integration-observe": "Theo dõi ứng dụng; cấu hình đồ họa/container nằm trong ứng dụng đó.", "game-mode-unsupported": "GameManager chưa hỗ trợ performance cho ứng dụng này.", "legacy-conflict": "Helper cũ xuất hiện. Xử lý trong Chẩn đoán rồi reboot.", "restore-conflict": "Có thay đổi bên ngoài hoặc trạng thái chưa khôi phục được. Xem Chẩn đoán.", "reboot-required": "Reboot sau khi xử lý helper cũ trước khi bật tự động.", "owner-user-only": "Chỉ quản lý người dùng chính (user 0).", "watcher-offline": "Watcher chưa chạy. Reboot sau cài đặt và kiểm tra Manager.", "not-started": "Chưa khởi động watcher." };
reasons["adaptive-learned-game"] = "Đã đủ lịch sử trong khung giờ này; chọn Game Android cho ứng dụng hỗ trợ. Có thể đặt quy tắc riêng trong Ứng dụng.";
reasons["adaptive-pressure"] = "Đã đo được áp lực tài nguyên; trở về Hằng ngày và hoàn tác game mode do module quản lý.";

function notify(message: string, error = false) { const box = $("notification"); box.textContent = message; box.classList.toggle("error", error); box.hidden = false; }
function failure(error: unknown) {
  const code = error instanceof Error ? error.message : "Không thể hoàn thành thao tác.";
  const messages:Record<string,string> = {"learning-needs-more-samples":"Chưa đủ dữ liệu đã lưu. Để module quan sát thêm ít nhất vài phút rồi cập nhật thống kê.","ai-disabled-or-not-configured":"Hãy cấu hình và bật API trong Trợ lý AI trước khi phân tích.","learning-disabled":"Ghi nhận hoặc module đang tắt; bật lại trước khi phân tích.","learning-changed-during-analysis":"Cài đặt đã đổi khi đang phân tích; kết quả không được lưu.","unknown timezone":"Múi giờ không hợp lệ. Ví dụ: Asia/Ho_Chi_Minh.","learning-history-invalid":"Lịch sử không hợp lệ. Xóa lịch sử để bắt đầu quan sát lại."};
  notify(messages[code] || code, true);
}
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
  text("ai-state", status.ai?.configured ? (status.ai.enabled ? "Đã cấu hình · xem lịch ở Thói quen" : "Đã cấu hình · đang tắt") : "Chưa cấu hình · core vẫn hoạt động");
}

async function refresh() {
  if (!available() || document.hidden || refreshing || aiJob || aiStarting) return;
  refreshing = true;
  try { render(await rpc({ action: "status" })); } catch (error) { failure(error); }
  finally { refreshing = false; }
}
function schedule() { if (timer) clearTimeout(timer); if (document.hidden || !available()) return; timer = setTimeout(async () => { await refresh(); schedule(); }, 5000); }

async function act(request: RPC, message?: string) { const data = await rpc(request); if (message) notify(message); await refresh(); return data; }
function bind(id: string, callback: () => Promise<void>) {
  $(id).addEventListener("click", async () => { const button = $<HTMLButtonElement>(id); button.disabled = true; try { await callback(); } catch (error) { failure(error); } finally { button.disabled = id==="ai-undo"?lastApplication.status!=="applied":["ai-ask","api-test","learning-analyze","ai-new-chat"].includes(id) && (!!aiJob || aiStarting); } });
}

function showTab(id: string) {
  if (!["overview", "apps", "habits", "ai", "tools", "diagnostics"].includes(id)) return;
  tab = id;
  for (const button of document.querySelectorAll<HTMLButtonElement>("[data-tab]")) { const selected = button.dataset.tab === id; button.setAttribute("aria-selected", String(selected)); button.tabIndex = selected ? 0 : -1; }
  for (const section of document.querySelectorAll<HTMLElement>("main > section")) section.hidden = section.id !== id;
  if (id === "ai") void loadAI().catch(failure);
  if (id === "habits") void loadLearning().catch(failure);
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
  $<HTMLDetailsElement>("api-settings").open = !status.configured;
  const evidence=await rpc<any>({action:"ai_preview"});text("ai-preview",pretty(evidence));
  text("ai-data-summary",`Mẫu hiện tại + ${evidence.days_with_samples || 0} ngày có mẫu · ${evidence.usage?.samples || 0} mẫu lịch sử · Khoảng thiếu dữ liệu được giữ là chưa biết.`);
  await loadApplication();
  await resumeAI();
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

function showAnswer(result: any, target = "ai-answer") {
  const answer = $(target); answer.hidden = false;
  answer.className="ai-response";answer.replaceChildren();
  // Never render provider output as HTML, Markdown HTML, JS or shell commands.
  const errors: Record<string,string> = {
    "response-truncated":"API đã hết giới hạn token; phần trả lời dở dang được bỏ qua. Có thể tăng giới hạn token hoặc chọn model khác rồi thử thủ công.",
    "response-incomplete":"API trả lời chưa hoàn tất; đang dùng báo cáo cục bộ.",
    "response-empty":"API không có nội dung trả lời cuối; model reasoning có thể đã dùng hết ngân sách token.",
    "response-filtered":"Nhà cung cấp đã lọc phản hồi; đang dùng báo cáo cục bộ.",
    "response-unusable":"Phản hồi API không hoàn tất ở dạng văn bản hỗ trợ; đang dùng báo cáo cục bộ."
    ,"ai-worker-unavailable":"Không khởi động được tác vụ AI. Kiểm tra module trong KernelSU và reboot sau khi cập nhật."
    ,"ai-job-interrupted":"Tác vụ đã bị gián đoạn hoặc quá thời gian chờ; không tự gửi lại API."
    ,"ai-job-busy":"Đang có một tác vụ AI. Mở lại Trợ lý AI để kiểm tra hoặc hủy trước khi gửi yêu cầu khác."
  };
  const content = result.source === "api" ? result.answer : `Báo cáo cơ bản · ${errors[result.ai_error] || result.ai_error || "không dùng AI"}\n\n${(result.report?.advice || []).join("\n\n")}`;
  const head=document.createElement("div");head.className="response-heading";const title=document.createElement("strong");title.textContent=result.source==="api"?"Phân tích AI":"Nhận xét cục bộ";const model=document.createElement("span");model.textContent=result.model || (result.source==="api"?"Model không cung cấp tên":"Quy tắc cục bộ");head.append(title,model);answer.append(head);
  const body=document.createElement("div");body.className="response-body";markdown(body,content);answer.append(body);
  renderProposal(answer,result);
  if(result.app_legend?.length){const details=document.createElement("details");const summary=document.createElement("summary");summary.textContent="Đối chiếu ứng dụng · Chỉ trên máy";const list=document.createElement("ul");for(const item of result.app_legend){const li=document.createElement("li");li.textContent=`${item.id} · ${item.package}`;list.append(li);}details.append(summary,list);answer.append(details);}
  if(result.usage){const details=document.createElement("details");const summary=document.createElement("summary");const usage=result.usage;summary.textContent=`${((result.elapsed_ms || 0)/1000).toLocaleString("vi-VN",{maximumFractionDigits:1})} giây · ${usage.total_tokens ?? ((usage.prompt_tokens ?? usage.input_tokens ?? 0)+(usage.completion_tokens ?? usage.output_tokens ?? 0))} token`;const info=document.createElement("p");info.textContent=`Model: ${result.model || "không cung cấp"} · Kết thúc: ${result.finish_reason || "không cung cấp"} · Đầu vào: ${usage.prompt_tokens ?? usage.input_tokens ?? "—"} · Đầu ra: ${usage.completion_tokens ?? usage.output_tokens ?? "—"}${usage.reasoning_tokens!==undefined?` · Reasoning: ${usage.reasoning_tokens}`:""}`;details.append(summary,info);answer.append(details);}
}

function proposalMessage(code:string):string {
  if(code==="proposal-pressure")return "Đang có áp lực bộ nhớ hoặc trạng thái nhiệt cao; chưa tự tăng chế độ game/bật preset.";
  if(code==="proposal-pressure-unknown")return "Chưa đọc đủ PSI/trạng thái nhiệt để tự tăng chế độ game/bật preset.";
  if(code==="proposal-kernel-unsupported")return "Preset chưa có mục hỗ trợ trên thiết bị hiện tại; chưa bật.";
  const messages:Record<string,string>={"proposal-format-invalid":"AI chưa trả đề xuất đúng cấu trúc; chưa thay đổi cài đặt.","proposal-no-valid-actions":"Không có đề xuất hợp lệ để tự áp dụng.","proposal-settings-changed":"Cài đặt đã đổi sau lúc AI phân tích. Phân tích lại để lấy đề xuất mới.","proposal-not-current":"Đề xuất đã được thay thế hoặc xóa. Xem kết quả mới nhất.","proposal-expired":"Đề xuất đã hết hạn hoặc thiết bị đã reboot. Phân tích lại trước khi áp dụng.","proposal-auto-needs-history":"Chưa đủ 2 ngày có mẫu, 30 mẫu và 30 phút quan sát để tự áp dụng.","proposal-auto-disabled":"Tự áp dụng theo lịch đang tắt.","proposal-manual-rule":"Giữ quy tắc ứng dụng bạn tự đặt. Bạn có thể xem và áp dụng thủ công.","proposal-kernel-mismatch":"Kernel chưa xác minh khớp; chưa áp dụng đề xuất.","proposal-game-unsupported":"Ứng dụng không hỗ trợ GameManager performance.","proposal-module-disabled":"Module đang tắt; chưa áp dụng đề xuất.","proposal-runtime-needs-review":"Có lỗi tinh chỉnh cần kiểm tra trong Chẩn đoán trước khi áp dụng.","proposal-restore-conflict":"Có xung đột hoàn tác cần kiểm tra.","proposal-undo-conflict":"Cài đặt đã được đổi bên ngoài; giữ nguyên thay đổi đó và không ghi đè để hoàn tác.","proposal-application-needs-review":"Lượt trước chưa xác định hoàn tất; kiểm tra cấu hình trước khi áp dụng tiếp."};
  return messages[code] || `Chưa áp dụng: ${code}`;
}
async function loadApplication(){
  lastApplication=await rpc<any>({action:"ai_application_status"});
  const states:Record<string,string>={applied:"Đã lưu cài đặt",undone:"Đã hoàn tác",failed:"Áp dụng chưa thành công",pending:"Đang xác minh", "not-committed":"Chưa lưu cài đặt","needs-review":"Cần kiểm tra trạng thái",acknowledged:"Đã giữ cấu hình hiện tại","undo-pending":"Đang xác minh hoàn tác"};
  text("ai-last-application",lastApplication.status?`${states[lastApplication.status] || lastApplication.status} · ${lastApplication.automatic?"Theo lịch":"Bạn bấm áp dụng"} · ${lastApplication.at} · ${(lastApplication.changes || []).length} thay đổi. Watcher áp dụng cấu hình theo trạng thái thiết bị.`:"Chưa áp dụng đề xuất nào.");
  $<HTMLButtonElement>("ai-undo").disabled=lastApplication.status!=="applied";
  $("ai-acknowledge").hidden=lastApplication.status!=="needs-review";
}
function renderProposal(container:HTMLElement,result:any){
  const plan=result.proposal;
  if(!plan){if(result.source==="api"&&(result.proposal_error||result.proposal_note)){const note=document.createElement("p");note.className="footnote";note.textContent=result.proposal_note==="no-changes"?"AI không đề xuất thay đổi cài đặt trong lượt này.":proposalMessage(result.proposal_error);container.append(note);}return;}
  const section=document.createElement("div");section.className="proposal-card";
  const title=document.createElement("h4");title.textContent="Các thay đổi được đề xuất";section.append(title);
  const names:Record<string,string>={...labels,automatic:"Tự chọn",on:"Bật",off:"Tắt"};
  for(const change of plan.changes || []){const row=document.createElement("div");row.className="proposal-change";const label=document.createElement("strong");label.textContent=change.kind==="app_profile"?`${change.app_id} · ${change.package}`:"Preset kernel Hằng ngày";const values=document.createElement("p");values.textContent=`${names[change.before] || change.before} → ${names[change.after] || change.after}`;const reason=document.createElement("p");reason.textContent=change.reason;row.append(label,values,reason);section.append(row);}
  const status=document.createElement("p");status.className="footnote";
  const receipt=lastApplication.proposal_id===plan.id?lastApplication:result.application;
  status.textContent=receipt?.status==="applied"?"Đã lưu cấu hình. Watcher áp dụng khi đủ điều kiện; có thể hoàn tác lượt gần nhất.":receipt?.status==="undone"?"Đề xuất này đã được hoàn tác. Phân tích lại để lấy đề xuất mới.":receipt?.error?proposalMessage(receipt.error):"Chỉ các thay đổi trong bảng này được áp dụng sau khi kiểm tra lại kernel và cấu hình.";
  const button=document.createElement("button");button.className="primary proposal-apply";button.textContent="Áp dụng đề xuất";button.disabled=receipt?.proposal_id===plan.id&&["applied","undone"].includes(receipt.status);
  button.addEventListener("click",async()=>{button.disabled=true;button.textContent="Đang kiểm tra & áp dụng…";try{const applied=await rpc<any>({action:"ai_proposal_apply",proposal_id:plan.id});status.textContent=applied.status==="applied"?"Đã lưu cấu hình. Watcher áp dụng theo trạng thái thiết bị.":"Đề xuất đã được xử lý; xem lượt áp dụng gần nhất.";await loadApplication();await refresh();button.textContent="Đã xử lý đề xuất";}catch(error){const code=error instanceof Error?error.message:"proposal-apply-failed";status.textContent=proposalMessage(code);button.disabled=false;button.textContent="Áp dụng đề xuất";}});
  section.append(status,button);container.append(section);
}
bind("ai-undo",async()=>{try{await rpc({action:"ai_application_undo"});await loadApplication();await refresh();notify("Đã hoàn tác cấu hình AI của lượt gần nhất. Giữ các thiết lập khác.");}catch(error){failure(new Error(proposalMessage(error instanceof Error?error.message:"proposal-undo-failed")));}});
bind("ai-acknowledge",async()=>{await rpc({action:"ai_application_acknowledge"});await loadApplication();notify("Đã giữ cấu hình hiện tại và bỏ quyền hoàn tác lượt chưa xác minh. Chưa áp dụng thay đổi mới.");});
function busyAI(busy:boolean) {for(const id of ["ai-ask","api-test","learning-analyze","ai-new-chat"]) $<HTMLButtonElement>(id).disabled=busy;$("ai-progress").hidden=!busy;$("ai").setAttribute("aria-busy",String(busy));$("habits").setAttribute("aria-busy",String(busy));if(aiClock)clearInterval(aiClock);if(busy){updateAIClock();aiClock=setInterval(updateAIClock,1000);}}
function updateAIClock(){const seconds=aiJob?.started?Math.max(0,Math.floor((Date.now()-Date.parse(aiJob.started))/1000)):0;text("ai-progress-title",aiJob?.state==="cancelling"?"Đang hủy yêu cầu…":aiJob?.state==="queued"?"Đang chuẩn bị dữ liệu…":"Đang chờ AI phân tích…");text("ai-progress-detail",`${seconds} giây đã chờ · Bạn có thể chuyển trang; kết quả sẽ được giữ trong phiên tác vụ.`);}
function finishAI(job:any){
  if(aiPollTimer)clearTimeout(aiPollTimer);aiJob=null;renderedJob=job.id;busyAI(false);
  if(job.state==="cancelled"){notify("Đã hủy yêu cầu. Không tự gọi lại API.");return;}
  if(job.state==="failed"){showAnswer({source:"local-rules",ai_error:job.error || "Phân tích không hoàn tất."});failure(new Error(job.error || "Phân tích không hoàn tất."));return;}
  const result=job.result;
  if(job.mode==="daily"){showDaily(result);if(result?.result)showAnswer({...result.result,app_legend:result.legend});notify("Đã có phân tích thống kê. Bạn có thể xem lại trong Thói quen.");}
  else if(result){showAnswer(result);if(job.mode==="ask"&&result.source==="api"){conversation.push({role:"assistant",content:String(result.answer).slice(0,12000)});notify("Đã có kết quả trong Trợ lý AI. Bạn có thể hỏi tiếp.");}}
  void loadApplication().catch(failure);
}
async function pollAI(){if(aiPolling||!aiJob||document.hidden)return;aiPolling=true;try{const job=await rpc<any>({action:"ai_job_status",job_id:aiJob.id});aiJob=job;if(["completed","failed","cancelled"].includes(job.state)){finishAI(job);return;}updateAIClock();}catch(error){aiJob=null;busyAI(false);failure(new Error("Không đọc được trạng thái tác vụ. Mở lại Trợ lý AI để kiểm tra; chưa gửi thêm yêu cầu API."));}finally{aiPolling=false;if(aiJob&&!document.hidden)aiPollTimer=setTimeout(()=>void pollAI(),1200);}}
async function resumeAI(){if(aiJob){if(!aiPolling){if(aiPollTimer)clearTimeout(aiPollTimer);void pollAI();}return;}const job=await rpc<any>({action:"ai_job_status"});if(["queued","running","cancelling"].includes(job.state)){aiJob=job;busyAI(true);void pollAI();}else if(["completed","failed"].includes(job.state)&&job.id!==renderedJob){finishAI(job);}}
async function startAI(mode:string){if(aiStarting||aiJob)return;aiStarting=true;busyAI(true);await new Promise<void>(resolve=>requestAnimationFrame(()=>requestAnimationFrame(()=>resolve())));try{const question=$<HTMLTextAreaElement>("ai-prompt").value.trim() || "Phân tích bằng chứng hiện có theo mục tiêu đã chọn và đề xuất bước tiếp theo cụ thể.";const messages=conversation.slice(-6);while(new TextEncoder().encode(JSON.stringify(messages)).length>30000)messages.shift();const job=await rpc<any>({action:"ai_job_start",mode,prompt:mode==="ask"?question:"",goal,messages:mode==="ask"?messages:[]});aiJob=job;if(mode==="ask"){if(!$("ai-answer").hidden){const old=$("ai-answer").cloneNode(true) as HTMLElement;old.removeAttribute("id");$("ai-thread").append(old);}while($("ai-thread").children.length>4)$("ai-thread").firstElementChild?.remove();const bubble=document.createElement("p");bubble.className="user-message";bubble.textContent=question;$("ai-thread").append(bubble);$("ai-answer").hidden=true;conversation.push({role:"user",content:question});}if(["failed","cancelled","completed"].includes(job.state))finishAI(job);else void pollAI();}catch(error){busyAI(false);failure(error);}finally{aiStarting=false;}}
bind("api-test",async()=>startAI("test"));
bind("ai-ask",async()=>startAI("ask"));
bind("ai-cancel",async()=>{if(!aiJob)return;const job=await rpc<any>({action:"ai_job_cancel",job_id:aiJob.id});aiJob=job;if(job.state==="cancelled")finishAI(job);else updateAIClock();});
bind("ai-new-chat",async()=>{await rpc({action:"ai_job_dismiss"});conversation=[];$("ai-thread").replaceChildren();$("ai-answer").hidden=true;notify("Đã bắt đầu cuộc trao đổi mới. Không gọi API.");});
for(const button of document.querySelectorAll<HTMLButtonElement>("[data-goal]"))button.addEventListener("click",()=>{goal=button.dataset.goal!;for(const b of document.querySelectorAll<HTMLButtonElement>("[data-goal]"))b.setAttribute("aria-pressed",String(b===button));});
for(const button of document.querySelectorAll<HTMLButtonElement>("[data-question]"))button.addEventListener("click",()=>{$<HTMLTextAreaElement>("ai-prompt").value=button.dataset.question!;$<HTMLTextAreaElement>("ai-prompt").focus();});
for(const button of document.querySelectorAll<HTMLButtonElement>("[data-token]"))button.addEventListener("click",()=>{input("api-limit").value=button.dataset.token!;});
bind("api-disable", async () => { await act({ action: "ai_disable" }, "AI đã tắt. Core không bị ảnh hưởng."); await loadAI(); });
bind("api-clear", async () => { await act({ action: "ai_clear" }, "Đã xóa cấu hình và khóa API."); $<HTMLFormElement>("api-form").reset(); input("api-key").value = ""; });

function showDaily(daily: any) {
  if (!daily?.result) { text("learning-answer", "Chưa có phân tích hoặc đề xuất để áp dụng."); $("learning-legend").hidden = true; return; }
  showAnswer(daily.result, "learning-answer");
  text("learning-legend", `Phân tích lúc ${daily.at} · ${daily.automatic ? "Theo lịch" : "Thủ công"}\nBảng đối chiếu tại thời điểm phân tích, chỉ trên máy:\n${(daily.legend || []).map((x:any) => `${x.id}: ${x.package}`).join("\n")}`);
  $("learning-legend").hidden = false;
}
async function loadLearning() {
  const status = await rpc<any>({ action: "learning_status" });
  if (status.error) throw new Error(status.error);
  const c = status.config;
  input("learning-enabled").checked = c.enabled; input("learning-adaptive").checked = c.adaptive;
  input("learning-daily").checked = c.daily_ai; input("learning-charging").checked = c.charging_only;
  input("learning-auto-apply").checked=!!c.auto_apply;
  input("learning-hour").value = String(c.hour); input("learning-zone").value = c.timezone;
  const days: any[] = Object.values(status.history?.days || {});
  const total = (key:string) => days.reduce((n,d) => n + (d[key] || 0), 0);
  text("learning-coverage", `${days.length} ngày · Đã quan sát ${metric(total("observed_seconds"), " phút",60)} · Màn hình hoạt động ${metric(total("active_seconds"), " phút",60)} · ${total("samples")} mẫu. Lưu gần nhất: ${status.history?.through || "chưa có"}.`);
  const cpu = days.reduce((x,d) => { const m=d.metrics?.cpu_busy_percent; if(m){x.sum+=m.sum;x.count+=m.count;x.max=Math.max(x.max,m.max);} return x; }, {sum:0,count:0,max:0});
  const heavy = days.reduce((n,d)=>n+(d.hours||[]).reduce((s:number,h:any)=>s+(h.heavy_samples||0),0),0);
  text("learning-intensity", `CPU toàn máy: ${cpu.count ? `${metric(cpu.sum/cpu.count,"%",1,1)} trung bình / ${metric(cpu.max,"%",1,1)} cao nhất (${cpu.count} mẫu)` : "chưa đủ hai lần đọc CPU"}. ${heavy} mẫu tải nặng/áp lực. AI thử gần nhất: ${status.last_attempt?.at || "chưa gọi"}.`);
  const hours = Array.from({length:24}, (_,hour) => days.reduce((n,d) => n + (d.hours?.[hour]?.active_seconds || 0), 0));
  const chart = $("learning-hours"); chart.replaceChildren();
  hours.forEach((seconds,hour) => { const row = document.createElement("label"); row.className = "habit-hour"; const meter = document.createElement("meter"); meter.min = 0; meter.max = Math.max(60,...hours); meter.value = seconds; meter.setAttribute("aria-label", `${hour} giờ: ${Math.round(seconds/60)} phút`); row.append(document.createTextNode(`${String(hour).padStart(2,"0")}h`),meter,document.createTextNode(`${Math.round(seconds/60)}′`)); chart.append(row); });
  const apps = $("learning-apps"); apps.replaceChildren();
  for (const item of status.apps || []) { const row = document.createElement("p"); row.className = "app-name"; row.textContent = `${item.id} · ${item.package} · ${Math.round(item.seconds/60)} phút / ${item.days} ngày / ${item.sessions} phiên · ${labels[item.profile] || "Theo quy tắc tự động"}`; apps.append(row); }
  text("learning-preview", pretty(await rpc({action:"learning_preview"})));await loadApplication(); showDaily(status.daily);
}
$("learning-form").addEventListener("submit", async event => {
  event.preventDefault(); const button = $("learning-form").querySelector<HTMLButtonElement>('button[type="submit"]')!; button.disabled = true;
  try { await rpc({ action:"learning_save", learning:{ enabled:input("learning-enabled").checked,adaptive:input("learning-adaptive").checked,daily_ai:input("learning-daily").checked,auto_apply:input("learning-auto-apply").checked,hour:Number(input("learning-hour").value),charging_only:input("learning-charging").checked,timezone:input("learning-zone").value.trim() } }); notify(input("learning-auto-apply").checked?"Đã bật tự áp dụng đề xuất hợp lệ sau lượt AI theo lịch, khi đủ dữ liệu và điều kiện kiểm tra.":"Đã lưu. Đề xuất AI chỉ được áp dụng khi bạn bấm nút."); await loadLearning(); } catch(error) { failure(error); } finally { button.disabled = false; }
});
bind("learning-refresh", loadLearning);
bind("learning-reset", async () => { await rpc({action:"learning_reset"}); await loadLearning(); notify("Đã xóa lịch sử và phân tích. Bắt đầu quan sát lại; không tự thử thêm API hôm nay."); });
bind("learning-analyze", async () => { showTab("ai");await startAI("daily"); });

document.addEventListener("visibilitychange", () => { if (document.hidden) { if (timer) clearTimeout(timer);if(aiPollTimer)clearTimeout(aiPollTimer); input("api-key").value = ""; } else { void refresh();if(aiJob)void pollAI(); schedule(); } });
window.addEventListener("pagehide", () => { if (timer) clearTimeout(timer); input("api-key").value = ""; });
if (!available()) { $("bridge-warning").hidden = false; text("watcher-state", "Không có bridge"); }
else { void refresh().then(schedule); }

# HNHouji Experience 0.6.1-preview

Module KernelSU độc lập cho Xiaomi 14 **houji**, arm64, GKI 6.1 và user 0.
WebUI mở trong Manager; CLI là phương án dự phòng. Không cần metamodule,
điều chỉnh một số tham số kernel runtime bằng allowlist, không thay source,
defconfig, pin/driver và không đóng gói model AI.

**Preview chưa được chứng nhận trên thiết bị.** Test máy phát triển và mock
Android không thay thế thử nghiệm reboot, thông báo, game và pin chờ trên houji.

## Sửa lỗi 0.6.1-preview

- HTTP 404 không còn tự động bị kết luận là nhà cung cấp từ chối JSON Schema. Chỉ phân loại từ JSON lỗi đầy đủ, tối đa 64 KiB; không lưu/hiện raw response, thông tin tài khoản hoặc khóa API. Lỗi endpoint/model, thiếu provider theo tham số và chính sách được phân biệt khi có bằng chứng; thiếu bằng chứng hiện chưa xác định.
- **Kiểm tra kết nối & JSON** dùng cùng định dạng schema và yêu cầu hỗ trợ tham số như phân tích, với câu hỏi không chứa dữ liệu thiết bị. Chỉ xác nhận thành công khi phản hồi hoàn tất, JSON hợp lệ và actions rỗng; không tạo đề xuất/receipt hoặc áp dụng cài đặt. Thành công không bảo đảm router miễn phí luôn có provider cho mọi phân tích sau đó.
- Chat Completions qua OpenRouter dùng `max_tokens` khi có ngân sách tường minh; các endpoint Chat khác giữ `max_completion_tokens`, Responses giữ `max_output_tokens`. Giá trị 0 vẫn bỏ tham số. Không tự đổi model, hạ schema, sửa quyền riêng tư hoặc retry khi không tìm được provider.
- `openrouter/free` có hỗ trợ lọc Structured Outputs theo [tài liệu OpenRouter](https://openrouter.ai/docs/guides/routing/routers/free-router); lỗi ảnh chụp chỉ xác nhận HTTP 404. Nút kiểm tra văn bản cũ thành công chưa đủ xác định nguyên nhân của yêu cầu phân tích.

## Mới trong 0.6.0-preview

- Mỗi control kernel có công tắc và bộ mức đóng: swap readahead **Gốc/0/1/2/3**, TCP **Gốc/CUBIC/BBR** theo thuật toán có sẵn, F2FS `/data` **Gốc/20**. Gốc hoàn tác entry module sở hữu, không đoán stock. Page-cluster là số mũ số trang: 0/1/2/3 tương ứng 1/2/4/8 trang; không cấu hình lại ZRAM.
- AI dùng action `kernel_level` theo schema capability. Khi đã bật tự áp dụng theo lịch, AI được đổi mức đặt tay chỉ trên control đang bật khi master bật; không tự bật lại mục đã tắt. Giữ điều kiện hai ngày, 30 mẫu, 1.800 giây, identity/pressure/stale-plan/undo. Profile app thủ công vẫn được bảo vệ. Hợp đồng boolean cũ còn được xử lý; không nhận path, shell hoặc số tùy ý.
- Trong **Tiện ích**, Android do bạn điều khiển, AI chỉ đọc: Wi-Fi/Bluetooth scanning riêng, vị trí nền 1/2/4 giờ, ngưỡng Battery Saver 15/20/30%, độ sáng 90/80/70/60% khi Saver, blur framework, animation 0,5×/1× và peak refresh 60/90/120 Hz theo primary display/minimum hiện tại. Mặc định giữ OEM; thiếu capability hiện chưa hỗ trợ. Không thay SurfaceFlinger props hoặc thêm keeper theo dõi chạm. Scanning/vị trí có đánh đổi với dịch vụ nền.
- Battery check chỉ đọc driver; dòng hiển thị là dòng pin, không phải dòng charger định mức. Thiếu chỉ số không biến thành 0 hoặc phần trăm độ chai.
- Pin chờ tách hai nguồn: khoảng screen-off liên tiếp watcher quan sát và snapshot Android `dumpsys batterystats --checkin`. Snapshot chạy worker theo yêu cầu, cache năm phút, timeout tám giây, tối đa 2 MiB; không retry/daemon/reset thống kê và không lưu UID/package/raw output. Mốc Android không được gọi mặc định là từ lần rút sạc. Screen-off không đồng nghĩa deep sleep; mẫu phần trăm có độ lượng tử 1%.
- Preset **Giữ gốc**, **Kernel hiện có**, **Cá nhân** phải xem trước rồi áp dụng. Backup JSON phiên bản 1 chứa cấu hình app/kernel/Android/lịch, xuất vào **Download/HNHouji**. File có tên package trong quy tắc app bạn chọn: chỉ chia sẻ khi cần. Không có API key, endpoint, kết quả AI, journal hoặc receipt. Restore giữ quyền gọi AI/tự áp dụng hiện tại, kiểm tra lại capability, báo app đã gỡ trong preview và từ chối preview sai boot/quá hạn mười phút/đã lỗi thời.
- Cấu hình nội bộ chuyển atomic từ phiên bản 1 sang 2, giữ lựa chọn cũ. Mức chưa lưu vẫn là 0/BBR/20. Module cũ không hiểu version 2; không downgrade bằng cách sửa version thủ công. Thông báo boot mặc định tắt và chỉ báo watcher khởi động.
- Kết quả AI vừa hoàn tất không bị snapshot Thói quen cũ ghi đè khi đổi tab. Test trì hoãn RPC tái hiện được lỗi cũ; mock giữ daily result giống backend.

### RPC bổ sung

Dùng kênh JSON/base64 sẵn có, không truyền lệnh shell trong dữ liệu:

| Action | Dữ liệu |
| --- | --- |
| `kernel_level_set` | `control`, `level` thuộc enum |
| `kernel_control_set` | `control`, `enabled` boolean |
| `system_setting_set` | `id`, `choice` theo capability |
| `system_settings_status` | Đọc lựa chọn, mức hỗ trợ và giá trị thực |
| `boot_notify_set` | `enabled` boolean |
| `battery_check` | Chỉ đọc chỉ số pin |
| `battery_snapshot_start` / `battery_snapshot_status` | `force` boolean / `job_id` backend trả |
| `settings_backup` / `settings_backup_export` | Đọc cấu hình portable / xuất file chia sẻ không có secret |
| `settings_preview` | `mode`: stock/kernel/custom/save_custom/import; import có `backup` typed |
| `settings_apply` | `token` của preview còn hạn, đúng boot/baseline |

AI thêm `{ "kind":"kernel_level", "control":"swap_readahead", "level":"1", "reason":"..." }`. Mỗi control tối đa một action trong cả nhóm level/boolean. Đổi mức giữ giá trị journal ban đầu; undo không ghi đè field đã đổi bên ngoài. Receipt AI cũ được giữ, không nhập từ backup.

Đóng gói bằng workflow Houji Experience, ghép với kernel thành công `40315390286`; không cần build lại kernel. Kiểm thử mock không thay thế thử native worker, boot, màn hình, sạc, thông báo và phép đo pin trên houji.

## Cài đặt

1. Dùng ZIP module `HNHouji-Experience-…-for-<kernel>-<hash>.zip` đi cùng đúng
   bản kernel trong cùng workflow build/release. Bản build module riêng vẫn có
   thể dùng để kiểm thử, không khóa vào một Image cụ thể.
2. Cài ZIP trong KSU/KernelSU-Next Manager. Không flash bằng recovery/AnyKernel3.
   Trên GitHub Actions, chọn artifact tên đầy đủ `HNHouji-Experience-<version>-…zip`
   hoặc link **Tải ZIP module để cài trực tiếp** trong Summary. File này upload
   trực tiếp (`archive: false`), không có ZIP bọc ngoài. Với các lượt build cũ,
   giải nén `HNHouji-Experience-preview.zip` một lần rồi chọn ZIP module bên trong.
   Không giải nén tiếp ZIP module. Nếu ID/tên/phiên bản đều `unknown` và báo
   `specified file not found in archive`, kiểm tra đã chọn đúng ZIP có
   `module.prop` ngay thư mục gốc. Gói `build-files-extract-first` dùng cho kiểm tra/
   collector, không cài trực tiếp.
3. Nếu có helper `/data/adb/service.d/houji-notification-helper.sh`, installer
   sao lưu theo SHA-256 rồi bỏ tên thực thi đó. Không đoán giá trị gốc của
   những thiết lập cũ đã lưu trong Android.
4. Reboot. Mở **WebUI** của module trong Manager.
5. Profile **Hằng ngày** mặc định tự hoạt động sau reboot khi kernel xác minh đạt.
   Danh sách app mặc định trống; chỉ chọn nếu cần profile riêng/bảo vệ thông báo.
   Có thể bấm **Tắt & hoàn tác**; lựa chọn tắt sẽ được giữ qua reboot/cập nhật.
6. Có thể bỏ qua AI. Báo cáo cơ bản và quy tắc cục bộ vẫn hoạt động.

Nếu Manager không cung cấp `ksu.exec` với `options.env`, dùng CLI; không có
giao diện dữ liệu giả thay cho kết nối thật. Trạng thái watcher offline cần
kiểm tra reboot, quyền root và log lifecycle của Manager.

## Profile và khả năng thực tế

- `daily`: áp dụng preset kernel runtime được mô tả bên dưới.
  Đây là profile mặc định cho mọi ứng dụng chưa có quy tắc riêng. Cập nhật từ
  cấu hình trống của 0.1.0/0.1.1 sẽ bật mặc định này một lần; cấu hình app có sẵn
  và các lựa chọn tắt từ 0.1.2 trở đi được giữ nguyên. Nếu vừa xử lý helper cũ,
  tạm quan sát tới lần reboot kế tiếp rồi mới chạy tự động.
- `native-game`: chọn GameManager `performance` nếu game/OEM công bố hỗ trợ;
  khôi phục mode ban đầu khi thoát. Không ép FPS, độ phân giải hay xung nhịp.
- `windows-game`, `linux`: nhận diện và quan sát; đồ họa/container cấu hình
  trong Winlator/giả lập/DroidSpaces. Không tự thay thư viện GPU hoặc khởi động
  container, VPN hay ứng dụng ngoài.
- Bảo vệ thông báo: thêm riêng package vào ngoại lệ Doze; nối MILLET nếu
  thiết lập OEM đó tồn tại và đọc được. Không bảo đảm mọi app nhận thông báo:
  còn phụ thuộc quyền, autostart, push service, kết nối và chính app đó.
- Foreground ổn định qua hai mẫu, chu kỳ 3 giây khi thức và 20 giây khi ngủ;
  không xác định/khóa màn hình thì bỏ override game. Chỉ user 0 được quản lý.
- `/dev/ntsync` chỉ xác nhận device node, chưa chứng minh runner dùng nó.
  ARR cần xác minh framework/HAL. Module không backport NTSync hoặc ARR.

## Tinh chỉnh kernel Hằng ngày (từ 0.1.2)

Module ghi các node proc/sysfs có hỗ trợ, lưu giá trị gốc vào journal trước
khi ghi, đọc lại xác nhận và hoàn tác khi tắt/gỡ. Không ghi lại node đã có
đúng giá trị. Preset áp dụng xuyên các profile ứng dụng và lúc nghỉ, không
giữ CPU thức.

| Tham số | Mục tiêu | Điều kiện và tác động |
| --- | --- | --- |
| `vm.page-cluster` | `0` | Chỉ khi swap đang hoạt động và toàn bộ là ZRAM. Tắt swap readahead: giảm đọc/giải nén đón, đổi lại có thể tăng page fault tiếp theo. Không resize/reset ZRAM. |
| `net.ipv4.tcp_congestion_control` | `bbr` | Chỉ khi kernel cung cấp BBR. Tác động lên kết nối TCP mới, không đổi qdisc và không bảo đảm giảm ping. |
| F2FS `/data` `min_fsync_blocks` | `20` | Đối chiếu major:minor từ mountinfo với sysfs, chỉ chọn `/data` F2FS đang mount read/write. Ngưỡng chính sách fsync theo helper hiện có của dự án, không tắt fsync/barrier hay ép GC. Hiệu quả cần đo. |

Mục thiếu điều kiện bị bỏ qua; điều kiện không còn thì hoàn tác phần đang sở
hữu. WebUI hiện giá trị hiện tại, mục tiêu, bản gốc và trạng thái từng mục.
Công tắc preset kernel hoạt động riêng với quy tắc ứng dụng. Nếu bên ngoài đổi
giá trị, module giữ thay đổi đó và dừng; lỗi ghi sẽ hoàn tác phần đã áp dụng
và dừng thử lại trong watcher tới khi bạn đổi cấu hình/bật lại. Không có thuật
toán tự học hay bằng chứng preset cải thiện mọi workload; cần đo lặp lại.

Giữ nguyên HZ, WALT, CPU/GPU governor/xung, thermal, LMKD, PSI, watermark,
swappiness, THP/MGLRU, bộ nén/kích thước ZRAM và driver vendor. AI chỉ có thể đề
xuất mức trong allowlist 0.6.0 qua bộ áp dụng kiểm tra; không tự tạo giá trị mới.

## Đồng bộ cấu hình và bản kernel

Trước khi áp dụng quy tắc, module đối chiếu:

- Kernel release thuộc baseline `6.1.177-android14-*` của r11.
- IKCONFIG thực từ `/proc/config.gz`: HZ 250, page size 4K, ZRAM/ZSMALLOC dạng
  module; SHA-256 của toàn bộ cấu hình phải khớp metadata.
- GNU build ID thực từ `/sys/kernel/notes` phải khớp metadata. Tên uname khác
  metadata chỉ được chấp nhận khi build ID/toàn bộ hash cấu hình đều khớp và
  cấu hình đó bật `CONFIG_KSU_SUSFS_SPOOF_UNAME=y`. Báo cáo giữ cả tên uname và
  release trong metadata, không sửa hay tắt tính năng SUSFS.

Workflow kernel xuất `houji-kernel.json` từ chính `Image` và `vmlinux` vừa build.
AnyKernel3 cài sidecar vào `/data/adb/houji-kernel/manifest.json`. Sidecar ghi
thêm SHA-256 Image và revision của recipe repository; module không đọc lại
boot partition để xác nhận hash Image. Đây là đối chiếu nhận dạng artifact,
không phải chữ ký xác thực hoặc chứng nhận KMI/boot.

Khi build kernel **android14-6.1 / 4K** cho houji, workflow tự chạy tiếp job
`build-houji-experience` qua workflow module dùng chung. Job tải chính xác tên
artifact từ job kernel của cùng run, kiểm tra hash Image rồi build backend,
WebUI và ZIP. Không dùng artifact từ run/release mới nhất bên ngoài.

ZIP chứa `kernel.json` khóa release/config/build ID/hash Image của bản kernel
đó. Từ 0.1.3, bản paired xác minh trực tiếp bằng metadata nhúng này, không
phụ thuộc việc recovery ghi được sidecar vào `/data/adb`. Metadata nhúng lỗi
không được chuyển ngầm sang generic. Sidecar của build cũ không thay thế nhận
dạng từ metadata nhúng và kernel đang chạy. Nếu cài nhầm ZIP, chỉ chẩn đoán. Tên ZIP gồm biến
thể kernel và phần đầu hash Image; artifact có tên `<kernel>-AnyKernel3-HoujiExperience`.
Mỗi biến thể/root có ZIP riêng, không ghi đè nhau.

Release/Pre-Release/Nightly lấy cả kernel và module trong cùng run, kiểm tra
metadata nhúng/checksum trước khi publish. Thiếu module hoặc sai cặp làm bước
release thất bại. Module vẫn cài bằng KSU Manager, không tự cài khi flash kernel.
Các họ kernel khác và page size 16K không chạy job module houji này.

**ZIP generic không nhúng metadata vẫn cần sidecar đã cài**, kể cả release nhìn
giống nhau. Với ZIP paired đúng bản kernel, không cần flash lại chỉ vì thiếu
sidecar; cập nhật module rồi reboot. Không tự tạo manifest từ runtime để vượt
kiểm tra. Thiếu config/build ID, metadata sai hoặc kernel khác baseline: hoàn
tác các entry đang sở hữu, chuyển Quan sát; chẩn đoán/AI/báo cáo vẫn dùng được. AnyKernel3 mới bỏ qua cài
helper cũ khi đã có module hoặc cấu hình module để tránh cùng ghi tham số.

## AI tùy chọn, cấu hình sau cài đặt

Trong **Trợ lý AI**, nhập kiểu API, base URL, model, xác thực và khóa/token.
Hỗ trợ `chat_completions` và `responses`; base URL gồm API prefix, không gồm
đường dẫn `/chat/completions` hoặc `/responses`. Dùng HTTPS với chứng chỉ hợp lệ.
Proxy riêng có thể chọn không xác thực, nhưng nên có token nếu public.

- Chat Completions gửi `messages`, `stream:false`.
  Endpoint phải hỗ trợ các trường này; nếu nhà cung cấp dùng giao thức khác,
  dùng proxy chuyển đổi. Responses gửi `input`, `instructions`,
  `store:false`. Chỉ gửi `max_tokens` (OpenRouter Chat), `max_completion_tokens` (Chat khác) hoặc `max_output_tokens` khi bạn
  chọn một ngân sách token dương.
- Token mặc định **0 = theo nhà cung cấp**, bỏ trường token khỏi request. Không
  có trần token 2048 của module; có thể nhập 8192, 16384 hoặc số dương khác.
  Model/nhà cung cấp vẫn quyết định giới hạn thực tế. Nâng cấp từ bản cũ chuyển
  các mức 512/1024/2048 sang 0 một lần; lựa chọn tường minh lưu ở bản mới được giữ.
  Timeout mới 180 giây, chỉnh 5..900; mức timeout cũ mặc định 30 đổi sang 180.
  Một yêu cầu/lần, không tự retry. Không tự tiếp tục khi model cắt phản hồi.
- **Lưu cấu hình API không gọi mạng.** Gọi khi bấm Kiểm tra kết nối/Phân tích,
  hoặc theo lịch hằng ngày bạn bật riêng trong **Thói quen**.
  Kiểm tra kết nối & JSON không gửi telemetry, yêu cầu schema và actions rỗng; không áp dụng cài đặt. API có thể phát sinh phí.
- Để trống khóa để giữ khóa cũ **chỉ khi endpoint/auth không đổi**. Đổi endpoint
  phải nhập lại khóa, tránh tự chuyển credential sang host khác.
- Khóa lưu ở `/data/adb/houji-experience/ai.json` (0600, thư mục 0700), không trả
  lại WebUI, không vào browser storage/log/export. Root khác vẫn có thể đọc nó.
  Có thể dùng proxy để giữ khóa nhà cung cấp ngoài điện thoại.
- Dữ liệu gửi kèm gồm mẫu hiện tại, thống kê 7 ngày, trung bình/cực đại/số mẫu
  theo ngày và thiết lập module có hỗ trợ. Không tự gửi tên package, raw logcat,
  thông báo hay serial. Bảng tên package/app-ID tương ứng chính xác với snapshot
  của yêu cầu chỉ được bổ sung vào kết quả **sau** khi gọi API, giữ riêng trên máy.
  Câu hỏi do người dùng nhập cũng được gửi: tránh nhập dữ liệu không muốn chia sẻ.
- Hỏi tiếp dùng tối đa 6 tin nhắn gần nhất, tổng nội dung tối đa 32 KiB trong RAM
  phiên WebUI. Không dùng browser storage. Cuộc trao đổi mới xóa ngữ cảnh phiên
  và kết quả tác vụ thủ công gần nhất. Kết quả AI là văn bản, không được chạy như lệnh
  hoặc chạy tự do. Từ 0.4.0, các actions có cấu trúc được kiểm tra để bạn bấm áp
  dụng hoặc chạy theo tùy chọn tự áp dụng lịch riêng. Khi API lỗi, hiện báo cáo cơ bản.
- Không ghi nội dung hỏi/đáp vào log. Phân tích hằng ngày gần nhất được lưu riêng
  trên máy để bạn xem lại, gồm token usage và bảng đối chiếu app-ID. Không ước lượng tiền.
- Phản hồi bị cắt (`length`/`incomplete`) không được dùng làm lời khuyên; hiển thị
  báo cáo cục bộ và trạng thái kết thúc/model thực. OpenRouter nhận
  `reasoning.exclude:true` để ẩn phần reasoning trả về, **không giảm ngân sách
  reasoning đã tính phí**. PSI là thời gian chờ bộ nhớ, không phải điện năng;
  loadavg không phải phần trăm CPU. Không gửi lại tự động khi hết token.
  Endpoint cần trả `finish_reason:"stop"` (Chat) hoặc `status:"completed"`
  (Responses); thiếu trạng thái kết thúc cũng dùng báo cáo cục bộ.

## Trợ lý và giao diện AI (0.3.0)

- Chọn mục tiêu Hằng ngày/Đa nhiệm/Chơi game/Giảm tải. Hỏi về khác biệt giữa
  các ngày, ưu tiên điều chỉnh hoặc kế hoạch đo; có thể hỏi tiếp vì sao và đánh đổi.
  Prompt không ép 180 từ/4 gạch đầu dòng. AI được yêu cầu đưa bằng chứng có số
  đo, giới hạn độ phủ, đề xuất theo thứ tự và cách so sánh trước/sau.
- Bảng so sánh trung bình/cực đại/số mẫu được tính cục bộ. Hai ngày khác workload
  không tự chứng minh nguyên nhân. CPU busy là toàn máy; không gán cho riêng app.
  Chất lượng kết luận còn phụ thuộc model và dữ liệu; chưa có phép đo thực thì
  không công bố tiết kiệm pin, FPS hay hiệu năng tăng.
- WebUI gửi RPC ngắn tạo tác vụ, native worker riêng gọi API, trạng thái được
  kiểm tra khoảng 1,2 giây/lần khi trang hiển thị. Có spinner, số giây đã chờ,
  nút hủy và mở lại trang để xem tác vụ đang chạy/kết quả gần nhất. Không giả
  phần trăm tiến độ hay giả streaming. CLI cũ vẫn hỗ trợ lời gọi đồng bộ.
- Worker chỉ chạy binary của module với ID ngẫu nhiên; prompt/khóa không nằm
  trong argv hay log. Hủy ngắt context HTTP; việc nhà cung cấp đã xử lý/tính phí
  phần nào trước lúc hủy phụ thuộc nhà cung cấp. Reboot/worker bị dừng được báo
  gián đoạn, không tự gửi lại. Kết quả gần nhất giữ private tối đa 7 ngày khi
  watcher hoạt động, bị xóa khi reset lịch sử/gỡ module.
- Markdown được dựng bằng DOM text: mục, danh sách, chữ đậm, bảng, code dạng
  văn bản. Không dựng HTML hoặc thực thi nội dung model. Metadata token/model
  được gấp riêng. Cấu hình API nằm trong mục thu gọn, ưu tiên không gian đọc.
- HTTP response có giới hạn bộ nhớ riêng 8 MiB, file kết quả đọc tối đa 64 MiB
  để chứa JSON đã escape;
  đây là giới hạn bytes để bảo vệ bộ nhớ, không phải trần token. Kiểm thử dùng
  API mô phỏng, vẫn cần kiểm chứng native worker và cảm giác UI trong Manager
  trên houji. Không gửi khóa API thật vào kiểm thử hay repo.
- Từ 0.4.1, `response-too-large` chỉ dùng khi thực sự đọc vượt 8 MiB.
  Timeout, hủy và lỗi đọc kết nối có thông báo riêng; lỗi cũ có thể đã bị gắn
  nhầm thành quá dung lượng. Không nhận nội dung dở dang, không tự thử lại.

Tham khảo: [OpenRouter: tham số và giá trị mặc định khi không gửi](https://openrouter.ai/docs/api_reference/parameters).

## Áp dụng đề xuất AI (0.6.0)

- AI trả Markdown trong `answer` và danh sách `actions` qua **JSON Schema strict**:
  Chat Completions dùng `response_format.json_schema`, Responses dùng `text.format`.
  OpenRouter dùng `provider.require_parameters=true` để tránh route bỏ qua schema.
  Cần model/provider hỗ trợ Structured Outputs. Schema giới hạn app-ID đúng snapshot,
  native-game đúng hỗ trợ và các mục kernel có khả năng bật. Cấu trúc không thay thế
  kiểm tra live; API vẫn có thể từ chối, timeout hoặc trả dở dang. Khi đó không tạo
  thay đổi, không hạ xuống free text và không gọi lại API để sửa JSON.
- Bảng **Các thay đổi được đề xuất** hiển thị ứng dụng/preset, giá trị trước/sau
  và lý do; bấm **Áp dụng đề xuất** để module kiểm tra lại rồi lưu cấu hình.
  Hỗ trợ profile ứng dụng (`automatic`, `daily`, `native-game`, `windows-game`,
  `linux`), công tắc preset, `kernel_control` boolean tương thích và `kernel_level`
  chọn mức trong allowlist 0.6.0. Tắt/Gốc khôi phục giá trị được lưu, không đoán stock.
  AI nhận giá trị hiện tại, mức đã chọn và các mức hỗ trợ; không có benchmark
  chứng minh cùng một mức luôn phù hợp cho mọi workload.
  Không nhận shell, đường dẫn, giá trị sysctl, quyền, bảo vệ thông báo hoặc API key
  từ AI. `automatic` bỏ quy tắc riêng; Windows/Linux vẫn chỉ theo dõi tích hợp.
- **Thói quen → Tự áp dụng đề xuất hợp lệ sau mỗi lượt AI chạy theo lịch** mặc
  định **tắt**. Khi bật, chỉ lượt lịch (`automatic=true`) mới được tự áp dụng,
  không áp dụng sau khi bạn bấm phân tích thủ công. Cần module/AI/ghi nhận/lịch
  cùng bật, ít nhất 2 ngày có mẫu, 30 mẫu và 1800 giây được quan sát. Đây là
  ngưỡng chính sách thử nghiệm, không chứng minh cấu hình tối ưu.
- Tự động giữ quy tắc ứng dụng bạn tự đặt. Quy tắc do AI áp dụng được đánh dấu
  riêng để lượt sau có thể điều chỉnh; đặt lại bằng UI/CLI sẽ bỏ dấu sở hữu AI.
  Preset và từng control nằm trong phạm vi tùy chọn tự áp dụng; tắt tùy chọn không tự
  hoàn tác lượt đã lưu. Tăng chế độ game/bật preset hoặc control tự động cần mẫu PSI và
  Android thermal hiện tại đọc được, PSI FULL <2% và thermal status <3; giữ
  nguyên cơ chế bảo vệ của hãng. Chỉ chọn performance nếu GameManager thực sự hỗ trợ.
- Kế hoạch có ID ngẫu nhiên, hết hạn sau 24 giờ/reboot, khóa với cấu hình lúc
  phân tích. Kernel phải xác minh khớp metadata, user 0, không có helper cũ/
  yêu cầu reboot hoặc lỗi journal/runtime đang cần xử lý. Kernel preset phải có
  ít nhất một mục hỗ trợ khi bật. Thay đổi cài đặt trong khi AI chạy/reset lịch
  sử vô hiệu hóa snapshot cũ. Mọi kiểm tra xong mới lưu `config.json` một lần
  nguyên tử; watcher áp dụng cấu hình theo foreground/guard, không ép boost tức thời.
- **Lượt áp dụng AI gần nhất → Hoàn tác** khôi phục đúng các trường đã đổi,
  giữ các thiết lập không liên quan. Khi trường đó đã đổi bên ngoài, hoàn tác
  báo xung đột và giữ giá trị bên ngoài. Runtime game/kernel vẫn dùng journal
  so sánh trước khi ghi; không đoán stock. Chỉ giữ một lượt hoàn tác thành công
  gần nhất; một lần áp dụng thất bại không xóa lượt hoàn tác thành công trước.
- `ai-proposal.json` và `ai-application.json` riêng 0600 lưu kế hoạch/biên nhận;
  không vào export công khai. Biên nhận pending/undo-pending cho phép xác minh
  sau gián đoạn mà không gọi lại AI/áp lại. Nếu không xác minh được, UI có lựa
  chọn giữ cấu hình hiện tại và bỏ lượt hoàn tác đó. Reset lịch sử xóa kế hoạch
  chưa áp dụng, giữ biên nhận cần cho undo; gỡ module xóa cả hai sau hoàn tác runtime.
  Nút Apply chỉ gửi ID, không nhận lại danh sách hành động từ browser.
- Kiểm thử dùng API và Android mô phỏng: cần kiểm chứng chu kỳ lịch/áp dụng/
  hoàn tác trên houji trước khi kết luận về trải nghiệm hoặc mức tiêu thụ tài nguyên.

## Học thói quen và phân tích hằng ngày (0.2.0)

- **Thói quen** có công tắc ghi nhận, tự chọn profile, lịch AI, giờ địa phương,
  múi giờ và chỉ gửi lúc sạc. Ghi nhận/tự chọn cục bộ bật mặc định; lịch AI tắt
  mặc định. Có nút xem payload, phân tích thủ công, xóa lịch sử và sang đặt quy tắc.
- Lịch sử riêng `learning.json`: tối đa 7 ngày, 32 ứng dụng/ngày; ứng dụng
  foreground, số phiên và số giây theo giờ. Mẫu CPU delta `/proc/stat`, RAM/PSI,
  nhiệt pin/trạng thái thermal và ZRAM tối đa mỗi phút; lưu đĩa mỗi 5 phút.
  Khoảng không quan sát được trên 90 giây không được điền thành thời gian dùng.
  Đây là độ phủ quan sát của watcher, không phải lịch sử đầy đủ của Android.
- Tự động chỉ dùng các profile hiện có. Với ứng dụng chưa có quy tắc thủ công:
  cần ít nhất 2 ngày, 1800 giây, 3 phiên và 600 giây trong giờ hiện tại, foreground
  ổn định 30 giây, game hỗ trợ performance và mẫu CPU/PSI/thermal không quá 2 phút.
  Khi PSI FULL >=2% hoặc Android thermal status >=3 đã đo được, không boost và
  hoàn tác game mode về giá trị ban đầu, giữ 2 phút trước khi cân nhắc lại.
  Các ngưỡng này là chính sách thử nghiệm, không chứng minh hiệu quả hay thay
  cơ chế bảo vệ nhiệt. Các ứng dụng thường vẫn dùng Hằng ngày; không dự đoán
  để boost ứng dụng trước khi mở. Quy tắc thủ công ưu tiên; bảo vệ áp lực vẫn áp dụng.
- AI nhận thống kê theo giờ và tối đa 16 app-ID tạm thời, không có package,
  thông báo, nội dung hay URL. Bảng tên app chỉ giữ trên máy cùng kết quả để
  đối chiếu chính xác kể cả khi thứ hạng dùng thay đổi. Không đưa lịch sử vào
  báo cáo export công khai. Múi giờ mặc định lấy từ Android, dự phòng UTC.
- Khi bật lịch: watcher kiểm tra sau giờ chọn, cần module/ghi nhận/AI cùng bật
  và ít nhất 2 mẫu/60 giây quan sát. Một lần thử mỗi ngày địa phương; ghi dấu
  **trước** gọi mạng, kể cả API lỗi/reboot không tự thử lại trong ngày đó. Phân
  tích thủ công vẫn là thao tác tường minh và cũng đánh dấu đã phân tích hôm nay.
  Lịch chạy trong worker riêng, không chặn vòng đổi profile/hoàn tác.
- AI tạo tư vấn và đề xuất có cấu trúc. Bạn có thể bấm áp dụng, tự đặt tại
  **Ứng dụng**, hoặc bật tự áp dụng theo lịch (xem phần 0.4.0). Không có bộ thực thi lệnh.
  Chưa đủ 2 ngày có mẫu thì chỉ tiếp tục đo; chưa có dữ liệu thực không kết luận
  hiệu năng hoặc tiết kiệm pin. Cần kiểm chứng overhead và hành vi trên houji.
- Tắt ghi nhận dừng thu thập; xóa lịch sử vô hiệu hóa cả bản nhớ trong watcher
  và xóa phân tích gần nhất, giữ dấu lần gọi hôm nay để tránh retry. Gỡ module
  xóa cả các file học thói quen. Mọi file này là 0600 trong thư mục 0700.
  Watcher dọn các ngày quá hạn và kết quả AI cũ mỗi giờ kể cả khi tắt ghi nhận;
  khi module không chạy, việc dọn tiếp tục vào lần chạy sau.

## CLI

Chạy bằng root:

```sh
CTL=/data/adb/modules/houji_experience/bin/houjictl
$CTL doctor
$CTL status
$CTL apps assign com.example.game native-game
$CTL protect add com.example.chat
$CTL enable
$CTL report
$CTL ai status
$CTL ai preview
printf '%s' 'Có dấu hiệu áp lực RAM không?' | $CTL ai ask
$CTL ai disable
$CTL disable
$CTL restore
$CTL export
```

**Xuất báo cáo:** file JSON sạch nằm trong **Bộ nhớ trong → Download → HNHouji**
(`/storage/emulated/0/Download/HNHouji/HNHouji-report-<UTC>-<random>.json`).
Mỗi lần xuất tạo file riêng để không ghi đè báo cáo trước. Có thể lấy bằng
trình quản lý tệp thông thường; không cần root. Nếu shared storage chưa mở khóa
hoặc không ghi được, module báo lỗi và không chuyển ngầm về `/data/adb`.
File người dùng đã xuất được giữ lại khi gỡ module. Config, journal và API key
vẫn nằm riêng trong thư mục root-only, không được nới quyền để xuất báo cáo.

`ai setup` đọc AIConfig JSON từ stdin (không truyền secret bằng argv).
Khuyến nghị nhập khóa qua WebUI, không dán vào shell history/chat. Schema:

```json
{"enabled":true,"style":"responses","base_url":"https://example.com/v1","model":"your-model","auth":"bearer","key":"YOUR_PERSONAL_KEY","max_output_tokens":512,"timeout_seconds":30}
```

RPC nội bộ: `houjictl rpc`, request JSON qua stdin hoặc biến môi trường
`HOUJI_REQUEST_B64` do WebUI truyền qua bridge. Action được allowlist; trường
không biết bị từ chối, không có API thực thi shell tùy ý.

## Hoàn tác và gỡ

- **Tắt & hoàn tác** lưu disabled trước rồi khôi phục các entry thuộc module.
- Nếu giá trị hiện tại khác cả giá trị ban đầu và giá trị module đã áp dụng,
  giữ nguyên nó và ghi xung đột. Không xóa toàn bộ whitelist/thiết lập OEM.
  Trong Chẩn đoán, đọc `pending_restore`; có thể chọn **Giữ thay đổi bên ngoài**
  để bỏ quyền hoàn tác riêng các mục xung đột đó. Thao tác này tắt tự động và
  không ghi vào Android. Các entry lỗi đọc/ghi chưa khôi phục vẫn được giữ lại.
- Hook `/data/adb/service.d/houji-experience-cleanup.sh` và bản cleanup binary
  độc lập dùng để phục hồi journal sau reboot, kể cả Manager đã disable module.
  Hook tự xóa sau uninstall thành công; nếu khôi phục lỗi, giữ để thử lại.
- Gỡ module xóa cấu hình/khóa AI, giữ journal và bản sao helper cho khôi phục.
  Không tự bật lại helper cũ. Nếu cần, chép thủ công bản `legacy-<SHA256>.sh`
  ra khỏi thư mục state, kiểm tra nội dung rồi mới sử dụng lại.
- Nếu flash ZIP kernel khác cài lại helper cũ, module báo `legacy-conflict`,
  hoàn tác phần đang sở hữu và dừng tinh chỉnh. Xử lý helper rồi reboot.
- Không xóa cả `/data/adb`. Khi gặp lỗi, tắt riêng module, chạy `restore` và
  xuất báo cáo; giữ các bản sao cho đến khi xác minh khôi phục xong.

## Build và kiểm thử

Theo lựa chọn của người dùng, build thực hiện trên GitHub Actions; không build
local và không cần chờ CI kết thúc. Workflow **Houji Experience** chạy kiểm thử
và build Android/WebUI. Khi chạy thủ công, nhập cả `kernel_run_id` của lượt
kernel đã thành công và `kernel_artifact_name` chính xác để đóng gói module
khớp kernel đó. Để trống cả hai tạo bản generic. Quy trình vẫn kiểm tra Image
SHA-256 và metadata trước khi đóng gói. Các lệnh dưới đây là tham chiếu cho CI.

Cần Go 1.27.3, Node 22+, pnpm 11.25.0, Python 3 và Android NDK r28b
(`28.1.13356709`). Dependency WebUI khóa trong pnpm-lock.yaml.

```sh
cd modules/houji-experience
go test ./...
go vet ./...
pnpm install --frozen-lockfile --ignore-scripts
pnpm build
pnpm test
pnpm exec playwright install chromium
pnpm test:browser
ANDROID_NDK_HOME=/path/to/android-ndk-r28b bash tools/build-android.sh
python3 tools/collect-licenses.py
python3 tools/package.py
```

Để đóng gói cặp từ artifact kernel có thật sau khi đã build module:

```sh
python3 tools/package.py --kernel-manifest /path/to/AnyKernel3/houji-kernel.json \
  --kernel-image /path/to/AnyKernel3/Image \
  --kernel-artifact 6.1.177-android14-lts-KernelSU-Next-AnyKernel3
```

Windows: `tools/build-android.ps1 -NdkHome C:\path\android-ndk-r28b` với Go trong
PATH. **Phải bật CGO** để dùng DNS resolver Bionic của Android; không phát hành
binary thuần Go CGO=0 dù cross-compile thành công. Không build bằng `netgo`.
Go runtime được link vào binary; license BSD kèm ZIP. Không có dependency Go
bên thứ ba hoặc HTTP server chạy nền.

Đầu ra `dist/HNHouji-Experience-0.6.1-preview.zip`, checksum và manifest; không
đưa source test, node_modules, model, key hay state vào ZIP. Build metadata nằm
trong manifest. Build riêng tạo ZIP generic; khi được workflow kernel gọi,
tạo ZIP có hậu tố bản kernel/hash và metadata nhúng. Workflow module không tự
publish; workflow kernel chính thu thập cặp đã xác minh để phát hành.

## Kiểm chứng trên thiết bị trước khi gọi ổn định

- Ba lần reboot: bật/tắt Manager, kiểm tra helper cũ, journal và hoàn tác.
- Kiểm tra `kernel_sync.ready=true`; đối chiếu node trước/trong/sau preset,
  tắt riêng preset, tắt module và thay đổi bên ngoài. Đo cùng workload ít nhất
  ba lượt bật/tắt; không dùng một mẫu để tuyên bố nhanh hơn.
- Dùng hỗn hợp 60 phút; một game Android và một game Windows/giả lập trong
  30–45 phút; đối chiếu mode GameManager trước/trong/sau.
- Test tắt màn hình, khóa màn hình, chia đôi cửa sổ, đổi user và force-stop app.
- Thông báo Wi-Fi/di động khi khóa màn hình; một đêm so pin chờ với baseline.
- Chuyển file LocalSend/KDE Connect, truy cập từ xa và mở/dừng container riêng.
- Profile chuyển trong 10 giây khi truy vấn thành công. Watcher dưới 1% **tổng
  CPU** trung bình trên bài đo 10 phút; dùng simpleperf/Perfetto hoặc CPU delta
  `/proc/<watcher PID>/stat` đối chiếu `/proc/stat`, không coi loadavg là CPU %.
- Đo p95/p99 frame time, RAM và pin lặp lại trước khi công bố cải thiện.
  AI không tạo kết nối khi chưa cấu hình/bị tắt; lịch chỉ chạy khi đã bật riêng.

Nguồn: [KSU WebUI](https://kernelsu.org/guide/module-webui.html),
[GameManager](https://developer.android.com/games/optimize/adpf/gamemode/gamemode-interventions),
[page-cluster](https://docs.kernel.org/admin-guide/sysctl/vm.html#page-cluster),
[F2FS sysfs ABI](https://android.googlesource.com/kernel/common/+/refs/heads/android14-6.1/Documentation/ABI/testing/sysfs-fs-f2fs),
[BBR](https://github.com/google/bbr),
[API key](https://developers.openai.com/api/reference/overview).

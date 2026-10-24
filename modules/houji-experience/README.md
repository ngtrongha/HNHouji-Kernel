# HNHouji Experience 0.1.3-preview

Module KernelSU độc lập cho Xiaomi 14 **houji**, arm64, GKI 6.1 và user 0.
WebUI mở trong Manager; CLI là phương án dự phòng. Không cần metamodule,
điều chỉnh một số tham số kernel runtime bằng allowlist, không thay source,
defconfig, pin/driver và không đóng gói model AI.

**Preview chưa được chứng nhận trên thiết bị.** Test máy phát triển và mock
Android không thay thế thử nghiệm reboot, thông báo, game và pin chờ trên houji.

## Cài đặt

1. Dùng ZIP module `HNHouji-Experience-…-for-<kernel>-<hash>.zip` đi cùng đúng
   bản kernel trong cùng workflow build/release. Bản build module riêng vẫn có
   thể dùng để kiểm thử, không khóa vào một Image cụ thể.
2. Cài ZIP trong KSU/KernelSU-Next Manager. Không flash bằng recovery/AnyKernel3.
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
swappiness, THP/MGLRU, bộ nén/kích thước ZRAM và driver vendor. AI chỉ tư vấn,
không được phép ghi tham số kernel.

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

- Chat Completions gửi `messages`, `max_completion_tokens`, `stream:false`.
  Endpoint phải hỗ trợ các trường này; nếu nhà cung cấp dùng giao thức khác,
  dùng proxy chuyển đổi. Responses gửi `input`, `instructions`,
  `max_output_tokens`, `store:false`.
- Mặc định 512 output token, timeout 30 giây; một yêu cầu/lần, không retry.
- **Lưu cấu hình không gọi mạng.** Chỉ bấm Kiểm tra kết nối/Phân tích mới gọi.
  Kiểm tra kết nối không gửi telemetry. API có thể phát sinh phí.
- Để trống khóa để giữ khóa cũ **chỉ khi endpoint/auth không đổi**. Đổi endpoint
  phải nhập lại khóa, tránh tự chuyển credential sang host khác.
- Khóa lưu ở `/data/adb/houji-experience/ai.json` (0600, thư mục 0700), không trả
  lại WebUI, không vào browser storage/log/export. Root khác vẫn có thể đọc nó.
  Có thể dùng proxy để giữ khóa nhà cung cấp ngoài điện thoại.
- Báo cáo gửi kèm chỉ có CPU load, RAM/PSI, ZRAM, pin/nhiệt và khả năng NTSync/
  xung đột helper. Không tự gửi package list, raw logcat, thông báo hay serial.
  Câu hỏi do người dùng nhập cũng được gửi: tránh nhập dữ liệu không muốn chia sẻ.
- Không tải lịch sử hội thoại. Kết quả AI là văn bản, không được chạy như lệnh
  hoặc tự áp dụng. Khi API lỗi, hiện báo cáo cơ bản cùng mã lỗi.
- Không ghi nội dung hỏi/đáp vào log. Metadata chỉ có trạng thái và thời điểm;
  token usage/thời gian phản hồi trả về phiên UI hiện tại, không ước lượng tiền.

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

Đầu ra `dist/HNHouji-Experience-0.1.3-preview.zip`, checksum và manifest; không
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
  AI không tạo kết nối khi chưa cấu hình hoặc khi không có thao tác người dùng.

Nguồn: [KSU WebUI](https://kernelsu.org/guide/module-webui.html),
[GameManager](https://developer.android.com/games/optimize/adpf/gamemode/gamemode-interventions),
[page-cluster](https://docs.kernel.org/admin-guide/sysctl/vm.html#page-cluster),
[F2FS sysfs ABI](https://android.googlesource.com/kernel/common/+/refs/heads/android14-6.1/Documentation/ABI/testing/sysfs-fs-f2fs),
[BBR](https://github.com/google/bbr),
[API key](https://developers.openai.com/api/reference/overview).

# HNHouji Experience — module tùy chọn

Module KSU độc lập, có WebUI tiếng Việt, profile cục bộ và AI API tùy chọn.
Không tải model AI; tinh chỉnh cần kernel có metadata đồng bộ. Đây là **preview**, chưa có
chứng nhận hiệu năng hay pin chờ trên thiết bị.

Từ **0.6.0-preview**, module có các mức kernel theo allowlist cho AI, tùy chọn Android thủ công, thông tin pin/snapshot Android và preset/backup có xem trước. Phần Android mặc định giữ OEM; không nhập preset WALT/thermal/ZRAM từ HyperCool. Xem hướng dẫn để biết quyền AI theo lịch và giới hạn kiểm chứng thiết bị.

Xem [hướng dẫn đầy đủ](../modules/houji-experience/README.md) để cài đặt, cấu
hình API sau cài, sử dụng CLI, build, kiểm thử và khôi phục.

Từ bản **0.1.1-preview**, nút Xuất báo cáo sạch lưu JSON vào **Bộ nhớ trong →
Download → HNHouji**, với tên riêng cho mỗi lần xuất. Không cần root để lấy file;
dữ liệu nội bộ và khóa API vẫn được giữ riêng trong `/data/adb`.

Build kernel **android14-6.1 / 4K** tự gọi workflow **Houji Experience** để
build ZIP module tương ứng sau khi kernel thành công. Module lấy metadata và
Image từ đúng artifact trong cùng run, kiểm tra hash rồi nhúng nhận dạng kernel.
Mỗi biến thể có ZIP riêng; cài nhầm cặp sẽ chỉ chẩn đoán. Release/Nightly đưa
cả hai ZIP và checksum module vào assets sau khi xác minh cặp; thiếu/sai module
sẽ dừng bước phát hành. Các họ kernel khác và 16K không build module houji.
Cài ZIP bằng KSU Manager rồi reboot; mở WebUI, chọn ứng dụng
và cấu hình thêm nếu cần. Từ 0.1.2-preview, profile **Hằng ngày** là mặc định,
tự hoạt động khi kernel xác minh đạt; AI vẫn tùy chọn. Module giữ lựa chọn tắt và quy tắc app
đã lưu; cấu hình trống của 0.1.0/0.1.1 nhận mặc định mới một lần khi cập nhật.

Preset Hằng ngày ghi `vm.page-cluster=0` khi chỉ có ZRAM swap, chọn TCP BBR
nếu kernel cung cấp, và đặt `min_fsync_blocks=20` đúng `/data` F2FS nếu hỗ trợ.
Mỗi mục có journal/đọc lại/hoàn tác và trạng thái trong WebUI. Không đổi
xung/thermal/WALT/HZ hay kích thước/bộ nén ZRAM. Hiệu quả cần đo trên houji.

Từ **0.1.3**, module paired dùng `kernel.json` nhúng trong ZIP để kiểm tra GNU
build ID và toàn bộ hash IKCONFIG thực, nên thiếu sidecar trong `/data/adb`
không chặn đúng cặp nữa. Uname khác metadata chỉ được chấp nhận khi các hash/ID
khớp và cấu hình bật SUSFS SPOOF_UNAME; báo cáo hiện cả hai release. ZIP generic
vẫn cần sidecar. Metadata thiếu/sai, config hoặc build ID không khớp đều chỉ
chẩn đoán; không tự dựng metadata từ runtime.

Nếu helper cũ được AnyKernel3 cài sẵn, installer sao lưu/vô hiệu hóa tên thực
thi của helper; cần reboot. Các thiết lập Android cũ không có bản gốc lưu sẵn
không được tự đoán/reset về stock.

Khi cần tắt, dùng **Tắt & hoàn tác**. Gỡ module sẽ xóa khóa API; hook cleanup
chỉ tồn tại thêm nếu journal chưa khôi phục xong. Không xóa toàn bộ `/data/adb`.

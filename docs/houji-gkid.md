# GKID trên Xiaomi 14 (houji)

Trong **Build Kernels**, chọn **Kernel Version → houji-6.1.138** và **Page Size → 4k**. Target này dùng mã nguồn ACK `0c3d559bcd85` của HyperOS `OS3.0.307.0.WNCCNXM`. Với ROM khác, cần đối chiếu kernel stock và module vendor trước khi dùng.

## BakaSU và ThinLTO

Để dựng cấu hình mở rộng, chọn **Performance**, **BakaSU**, **Commit mode → latest**, **Networking** và **Baseband Guard**. Lần CI cho cấu hình này dùng patch date `2025-06` và tắt các tính năng bổ sung khác. Mỗi lần dispatch ở chế độ latest, workflow phân giải nhánh BakaSU `main` thành SHA để checkout. Tên artifact chứa `BakaSU-houji-GKID-ThinLTO`.

| Nhóm | Thay đổi của bản mở rộng |
| --- | --- |
| CPU và I/O | ThinLTO, timer 300 Hz, governor mặc định schedutil. Ondemand và mq-deadline được biên dịch để HyperOS có thể chọn theo policy hoặc hàng đợi. |
| RAM | MGLRU, zRAM dạng module với mặc định LZ4 và hỗ trợ writeback. ZIP AnyKernel3 chỉ cài Image. Cặp `zram.ko` và `zsmalloc.ko` cùng bản build có artifact riêng cho người tích hợp ROM. Module đang nạp, thuật toán nén và thiết bị backing trên điện thoại phụ thuộc system_dlkm và cấu hình khởi động của ROM. |
| Filesystem | Giảm khoảng nghỉ GC **khẩn cấp** của F2FS xuống 50 ms. JBD2 dùng mặc định commit journal 30 giây cho ext4; mount option có thể ghi đè. Khoảng commit dài làm tăng cửa sổ thay đổi chưa bền vững khi mất nguồn. |
| Bộ nhớ ARM64 | Giữ ba patch nền bên dưới và dùng mã scalar cho ba hàm thư viện. Prefetch nguồn cho memcpy dài theo chiều tiến, hai store chồng lấn cho memset 8–15 byte, so sánh đầu/cuối bằng tải 32 bit cho memcmp 4–7 byte. |
| Nguồn điện | Freezer mặc định 1 giây. Alarm sắp hết hạn giữ thức đến thời điểm báo cộng 1 ms, có chặn dưới 1 ms cho alarm đã trễ. Giới hạn 500 ms áp dụng cho yêu cầu **có thời hạn** đi qua giao diện userspace `/sys/power/wake_lock`. Wakelock của driver và khóa vô thời hạn vẫn theo vòng đời của bên sở hữu. |
| Mạng | Networking cung cấp IP Set, Netfilter, NAT IPv4/IPv6, IPsec/ESP, BBR và Westwood. BBR là TCP mặc định khi biên dịch, FQ-CoDel là qdisc mặc định. Android có thể đổi chúng khi chạy. |
| Quyền và bảo vệ | BakaSU cung cấp root. SELinux và Baseband Guard được bật khi chọn BBG. Workflow kiểm tra danh sách LSM, ABI, ThinLTO và phiên bản thật trong Image trước khi đóng gói. Trạng thái SELinux enforcing trên thiết bị phụ thuộc boot và policy của ROM. |

## Bản nền ba patch

Khi tắt **Performance**, target chỉ thêm ba patch GKID dưới đây. Phần đối chiếu sau bảng mô tả bản nền này.

| Patch | Thay đổi trên cây ACK 6.1.138 | Cách đo trên houji |
| --- | --- | --- |
| 0007 | Thêm hai lệnh prefetch vào phần assembly dùng cho sao chép giữa kernel và userspace. | So sánh thông lượng đọc/ghi qua pipe và thời gian CPU cùng kích thước buffer. |
| 0017 | Căn đầu hàm `__pi_clear_page` theo biên 16 byte. | So sánh địa chỉ hàm trong hai `vmlinux`, rồi đo thời gian cấp và chạm trang. Nếu đã căn sẵn, thay đổi có thể không tạo khác biệt. |
| 0019 | Trong `pm_system_wakeup`, chỉ gọi `s2idle_wake()` khi bộ đếm yêu cầu hủy suspend chuyển từ 0 lên 1. | Đo hao pin khi tắt màn hình và thời gian suspend. Kiểm tra báo thức, thông báo và đánh thức bằng nút nguồn. |

Bản 6.1 của GKID lấy nguồn từ Duchamp và bỏ qua bước áp toàn bộ thư mục patch chung. Cả ba thay đổi ở trên đều hiện diện trong mã Duchamp đã đối chiếu.

## Đối chiếu các tính năng GKID

| Hạng mục | Quyết định cho houji |
| --- | --- |
| Timer, governor và I/O | Giữ 250 Hz của firmware tham chiếu. ACK mặc định chọn schedutil trên ARM64 và biên dịch mq-deadline. Governor đang dùng trên từng cụm CPU và scheduler đang dùng cho từng hàng đợi UFS phải đọc trên máy. Bật thêm ondemand chỉ tạo thêm một lựa chọn. |
| MGLRU và zRAM | Defconfig ACK đã bật MGLRU. Giữ zRAM dạng module và thuật toán mặc định lzo-rle của firmware tham chiếu. HyperOS có thể chọn lại thuật toán khi thiết lập thiết bị zRAM. Thuật toán đang dùng và thiết bị lưu writeback cần kiểm tra trên máy. |
| F2FS và ext4 | 50 ms trong GKID là khoảng nghỉ **GC khẩn cấp** của F2FS. Giảm từ 500 xuống 50 ms có thể tăng cạnh tranh I/O. Mốc 30 giây là mặc định commit journal của JBD2, ảnh hưởng tới cửa sổ dữ liệu chưa commit trên ext4. Cả hai giữ theo baseline. |
| memcpy, memset và memcmp | 0007 tác động tới đường usercopy của ACK. Không có phép đo cho tuyên bố tăng 50% cả ba hàm trên Snapdragon 8 Gen 3. Patch memcmp dùng thanh ghi SIMD trong hàm dùng chung mà không bảo vệ trạng thái FPSIMD. |
| Suspend, alarm và wakelock | Chỉ áp 0019. Hạn freezer 1 giây có thể khiến suspend thất bại sớm. Với alarm đã trễ, 0009 có thể chuyển thời gian âm sang timeout unsigned. Chính sách wakelock 500 ms toàn hệ thống chưa có kiểm thử cuộc gọi, thông báo và báo thức trên houji. |
| BBR, Westwood+ và FQ-CoDel | Fragment Networking đặt BBR làm mặc định cho kết nối TCP mới, trừ khi bị ghi đè lúc chạy. Westwood, FQ-CoDel và CAKE là các lựa chọn được biên dịch. Fragment không đặt FQ-CoDel làm hàng đợi mặc định. Bước gọi BBRv3 hiện bị tắt trong action. Patch BBRv3 trực tiếp từ GKID không qua `git apply --check` trên ACK stock ở cả header TCP lẫn mã IPv4. TCP congestion control chỉ ảnh hưởng các luồng TCP liên quan. |
| IP Set, NAT, IPsec/ESP và Netfilter | ACK đã bật Netfilter, NAT IPv4, XFRM userspace và ESP cho cả IPv4/IPv6. Fragment Networking bổ sung IP Set và NAT/MASQUERADE IPv6. Tethering, VPN và firewall còn phụ thuộc chính sách userspace và đường truyền. |
| NTSync và DroidSpaces | Hai tùy chọn build độc lập đã có. NTSync cần runtime Wine/emulator sử dụng giao diện tương ứng. DroidSpaces bổ sung các khả năng cho môi trường Linux. Lần chạy CI [44726388487](https://github.com/ngtrongha/HNHouji-Kernel/actions/runs/44726388487) đã tắt cả hai để kiểm tra bản có ba patch. |
| Baseband Guard, SELinux và root | BBG và trình quản lý root là các lựa chọn riêng của fork. Lần chạy kiểm chứng chọn No Root. Khả năng tương thích ứng dụng ngân hàng và Play Integrity cần kiểm tra theo thiết bị, ROM và bootloader. Các chức năng che giấu, giả mạo và nhiều trình quản lý nằm ngoài bộ tối ưu hiệu năng houji. |
| NetHunter và WirelessKSU | Driver Wi-Fi ngoài, firmware, monitor mode và packet injection thuộc bộ công cụ chuyên dụng. Bản houji không đóng gói module WirelessKSU. |
| LTO và Compat | Quy trình Kleaf hiện có điều khiển bản build. Giữ kiểm tra phiên bản module cho houji. Chuyển sang FullLTO, ThinLTO hoặc biến thể Compat cần phép so sánh riêng với đúng toolchain và module của ROM. |

Đối chiếu mạng dựa trên [defconfig của ACK](https://android.googlesource.com/kernel/common/+/0c3d559bcd85c64dae9c9c9687abbe5347fff3ad/arch/arm64/configs/gki_defconfig), [action Networking của fork](../.github/actions/networking/action.yml) và [fragment Networking](../.github/actions/networking-config/networking-config.fragment).

Khi đo A/B, dùng cùng SHA ACK, toolchain, cấu hình và firmware cho cả hai bản. Trên bản đối chứng, bỏ đúng ba patch trong bước **Apply GKID tuning for Xiaomi 14**. Kết quả build và kiểm tra CRC chỉ xác nhận một phần tính tương thích. Lợi ích về hiệu năng và pin cần số đo từ máy thật.

Nguồn đối chiếu gồm [GKID-Kernels tại ef6c99d](https://github.com/ahmed-alnassif/GKID-Kernels/tree/ef6c99dc6fc10f120cc02c37e721ab959a2fbd9c/kernel-patches/common), [cây Duchamp tại 36aba3c](https://github.com/ahmed-alnassif/GKI-Duchamp-6.1/tree/36aba3c75f8c4df693a602dd4631490559c1b008) và [ACK stock](https://android.googlesource.com/kernel/common/+/0c3d559bcd85c64dae9c9c9687abbe5347fff3ad).

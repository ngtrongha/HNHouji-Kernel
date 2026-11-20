# GKID trên Xiaomi 14 (houji)

Trong **Build Kernels**, chọn **Kernel Version → houji-6.1.138** và **Page Size → 4k**. Target này dùng mã nguồn ACK `0c3d559bcd85` của HyperOS `OS3.0.307.0.WNCCNXM`. Với ROM khác, cần đối chiếu kernel stock và module vendor trước khi dùng. Tên artifact có hậu tố `-houji-AnyKernel3`.

Target áp ba patch GKID sau cho mọi lựa chọn root. Ô **Performance** chỉ điều khiển bộ patch chung của các target khác.

| Patch | Thay đổi trên cây ACK 6.1.138 | Cách đo trên houji |
| --- | --- | --- |
| 0007 | Thêm hai lệnh prefetch vào phần assembly dùng cho sao chép giữa kernel và userspace. | So sánh thông lượng đọc/ghi qua pipe và thời gian CPU cùng kích thước buffer. |
| 0017 | Căn đầu hàm `__pi_clear_page` theo biên 16 byte. | So sánh địa chỉ hàm trong hai `vmlinux`, rồi đo thời gian cấp và chạm trang. Nếu đã căn sẵn, thay đổi có thể không tạo khác biệt. |
| 0019 | Trong `pm_system_wakeup`, chỉ gọi `s2idle_wake()` khi bộ đếm yêu cầu hủy suspend chuyển từ 0 lên 1. | Đo hao pin khi tắt màn hình và thời gian suspend. Kiểm tra báo thức, thông báo và đánh thức bằng nút nguồn. |

Bản 6.1 của GKID lấy nguồn từ Duchamp. Trên houji, bộ patch này giữ nguyên cấu hình HZ, ZRAM, MGLRU và mã F2FS của cấu hình nền. Patch `memcmp` của GKID dùng thanh ghi SIMD trong hàm dùng chung mà không bảo vệ trạng thái FPSIMD. Các thay đổi mạng BBRv3 và NTSync vẫn đi theo tùy chọn build riêng của dự án.

Khi đo A/B, dùng cùng SHA ACK, toolchain, cấu hình và firmware cho cả hai bản. Trên bản đối chứng, bỏ đúng ba patch trong bước **Apply GKID tuning for Xiaomi 14**. Kết quả build và kiểm tra CRC chỉ xác nhận một phần tính tương thích. Lợi ích về hiệu năng và pin cần số đo từ máy thật.

Nguồn đối chiếu gồm [GKID-Kernels tại ef6c99d](https://github.com/ahmed-alnassif/GKID-Kernels/tree/ef6c99dc6fc10f120cc02c37e721ab959a2fbd9c/kernel-patches/common), [cây Duchamp tại 36aba3c](https://github.com/ahmed-alnassif/GKI-Duchamp-6.1/tree/36aba3c75f8c4df693a602dd4631490559c1b008) và [ACK stock](https://android.googlesource.com/kernel/common/+/0c3d559bcd85c64dae9c9c9687abbe5347fff3ad).

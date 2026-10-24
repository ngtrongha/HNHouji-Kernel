package experience

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"regexp"
	"strings"
)

type KernelManifest struct {
	Schema         int    `json:"schema"`
	Device         string `json:"device"`
	ModuleContract int    `json:"module_contract"`
	KernelRelease  string `json:"kernel_release"`
	ConfigSHA256   string `json:"config_sha256"`
	BuildID        string `json:"build_id"`
	ImageSHA256    string `json:"image_sha256"`
	SourceRevision string `json:"source_revision"`
}

type KernelSync struct {
	Ready          bool              `json:"ready"`
	State          string            `json:"state"`
	Message        string            `json:"message"`
	KernelRelease  string            `json:"kernel_release"`
	ConfigSHA256   string            `json:"config_sha256,omitempty"`
	BuildID        string            `json:"build_id,omitempty"`
	SourceRevision string            `json:"source_revision,omitempty"`
	Features       map[string]string `json:"features,omitempty"`
	Paired         bool              `json:"paired"`
}

func configValues(data []byte) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "CONFIG_") {
			if key, value, ok := strings.Cut(line, "="); ok {
				values[key] = value
			}
		}
		if strings.HasPrefix(line, "# CONFIG_") && strings.HasSuffix(line, " is not set") {
			values[strings.TrimSuffix(strings.TrimPrefix(line, "# "), " is not set")] = "n"
		}
	}
	return values
}

// /sys/kernel/notes contains structured ELF notes, unlike raw Image which may
// also embed unrelated nVHE/pKVM build IDs. Only one running GNU ID is accepted.
func runningBuildID(data []byte) string {
	ids := map[string]bool{}
	for offset := 0; offset+12 <= len(data); {
		n := int(binary.LittleEndian.Uint32(data[offset:]))
		d := int(binary.LittleEndian.Uint32(data[offset+4:]))
		kind := binary.LittleEndian.Uint32(data[offset+8:])
		offset += 12
		if n < 0 || n > 4096 || d < 0 || d > 65536 {
			return ""
		}
		nameEnd := offset + n
		descStart := offset + ((n + 3) &^ 3)
		end := descStart + ((d + 3) &^ 3)
		if nameEnd > len(data) || end > len(data) {
			return ""
		}
		if kind == 3 && n == 4 && bytes.Equal(data[offset:nameEnd], []byte("GNU\x00")) && d >= 8 && d <= 64 {
			ids[hex.EncodeToString(data[descStart:descStart+d])] = true
		}
		offset = end
	}
	if len(ids) != 1 {
		return ""
	}
	for id := range ids {
		return id
	}
	return ""
}

var shaRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
var buildIDRE = regexp.MustCompile(`^(?:[0-9a-f]{2}){8,64}$`)
var revisionRE = regexp.MustCompile(`^[0-9a-f]{40}$`)

func validKernelManifest(m KernelManifest) bool {
	return m.Schema == 1 && m.Device == "houji" && m.ModuleContract == 1 && shaRE.MatchString(m.ConfigSHA256) && shaRE.MatchString(m.ImageSHA256) && buildIDRE.MatchString(m.BuildID) && revisionRE.MatchString(m.SourceRevision)
}

func (a *App) KernelSync(ctx context.Context) KernelSync {
	s := KernelSync{State: "unverified", Message: "Chưa xác minh kernel; chỉ chẩn đoán."}
	if e := ctx.Err(); e != nil {
		s.State = "read-failed"
		return s
	}
	s.KernelRelease, _ = a.run(ctx, "uname", "-r")
	if !strings.HasPrefix(s.KernelRelease, "6.1.177-android14-") {
		s.State = "family-mismatch"
		s.Message = "Kernel khác baseline android14-6.1.177; không áp dụng quy tắc ứng dụng."
		return s
	}
	f, e := os.Open(a.system("/proc/config.gz"))
	if e != nil {
		s.State = "config-unavailable"
		s.Message = "Không đọc được /proc/config.gz; chỉ chẩn đoán."
		return s
	}
	defer f.Close()
	z, e := gzip.NewReader(io.LimitReader(f, 512*1024))
	if e != nil {
		s.State = "config-invalid"
		s.Message = "IKCONFIG không hợp lệ; chỉ chẩn đoán."
		return s
	}
	defer z.Close()
	data, e := io.ReadAll(io.LimitReader(z, 2*1024*1024+1))
	if e != nil || len(data) > 2*1024*1024 {
		s.State = "config-invalid"
		s.Message = "IKCONFIG không đọc đầy đủ; chỉ chẩn đoán."
		return s
	}
	s.ConfigSHA256 = digest(data)
	values := configValues(data)
	s.Features = map[string]string{}
	for _, key := range []string{"CONFIG_HZ", "CONFIG_ARM64_4K_PAGES", "CONFIG_ZRAM", "CONFIG_ZSMALLOC", "CONFIG_LRU_GEN", "CONFIG_TRANSPARENT_HUGEPAGE", "CONFIG_NTSYNC", "CONFIG_WIREGUARD", "CONFIG_CIFS", "CONFIG_DEBUG_INFO_BTF"} {
		v := values[key]
		if v == "" {
			v = "unknown"
		}
		s.Features[key] = v
	}
	for key, want := range map[string]string{"CONFIG_HZ": "250", "CONFIG_ARM64_4K_PAGES": "y", "CONFIG_ZRAM": "m", "CONFIG_ZSMALLOC": "m"} {
		if values[key] != want {
			s.State = "config-policy-mismatch"
			s.Message = "Cấu hình HZ/page size/ZRAM khác hợp đồng houji r11; không ghi đè cấu hình kernel."
			return s
		}
	}
	notes, e := os.ReadFile(a.system("/sys/kernel/notes"))
	if e != nil || len(notes) > 256*1024 {
		s.State = "build-id-unavailable"
		s.Message = "Không đọc được build ID của kernel đang chạy; chỉ chẩn đoán."
		return s
	}
	s.BuildID = runningBuildID(notes)
	if s.BuildID == "" {
		s.State = "build-id-unavailable"
		s.Message = "Build ID kernel không xác định được; chỉ chẩn đoán."
		return s
	}
	var m KernelManifest
	e = readJSON(a.system("/data/adb/houji-kernel/manifest.json"), &m)
	if os.IsNotExist(e) {
		s.State = "manifest-missing"
		s.Message = "Kernel chưa có metadata đồng bộ. Cần bản kernel được đóng gói kèm manifest; module chỉ chẩn đoán."
		return s
	}
	if e != nil || !validKernelManifest(m) {
		s.State = "manifest-invalid"
		s.Message = "Metadata kernel thiếu hoặc không tương thích với module; chỉ chẩn đoán."
		return s
	}
	s.SourceRevision = m.SourceRevision
	if m.KernelRelease != s.KernelRelease || m.ConfigSHA256 != s.ConfigSHA256 || m.BuildID != s.BuildID {
		s.State = "build-mismatch"
		s.Message = "Release, IKCONFIG hoặc build ID không khớp bản kernel đã đóng gói. Cần đúng cặp kernel/metadata; chỉ chẩn đoán."
		return s
	}
	var expected KernelManifest
	e = readJSON(a.Paths.Module+"/kernel.json", &expected)
	if !os.IsNotExist(e) {
		s.Paired = true
		if e != nil || !validKernelManifest(expected) {
			s.State = "package-manifest-invalid"
			s.Message = "Metadata đi kèm ZIP module không hợp lệ; chỉ chẩn đoán."
			return s
		}
		if expected.KernelRelease != m.KernelRelease || expected.ConfigSHA256 != m.ConfigSHA256 || expected.BuildID != m.BuildID || expected.ImageSHA256 != m.ImageSHA256 {
			s.State = "package-build-mismatch"
			s.Message = "ZIP module dành cho bản kernel khác. Cài đúng ZIP module đi cùng bản kernel; chỉ chẩn đoán."
			return s
		}
	}
	s.Ready = true
	s.State = "matched"
	s.Message = "Release, hash IKCONFIG và GNU build ID khớp metadata bản build. Không thay thế kiểm chứng KMI/boot trên thiết bị."
	if s.Paired {
		s.Message = "Module đi cùng đúng bản kernel: release, IKCONFIG và build ID đã khớp. Cần kiểm chứng hiệu năng trên thiết bị."
	}
	return s
}

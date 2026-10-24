type NativeBridge = { exec(command: string, options: string, callback: string): void };
declare global { interface Window { ksu?: NativeBridge; [key: string]: unknown } }
export type RPC = { action: string; [key: string]: unknown };
export const COMMAND = "/data/adb/modules/houji_experience/bin/houjictl rpc";
let serial = 0;

export function available(): boolean { return typeof window.ksu?.exec === "function"; }
export function encodeRequest(request: RPC): string {
  const bytes = new TextEncoder().encode(JSON.stringify(request));
  if (bytes.length > 65536) throw new Error("Yêu cầu quá lớn.");
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

export function rpc<T = any>(request: RPC): Promise<T> {
  if (!available()) return Promise.reject(new Error("Chưa có bridge KernelSU. Mở WebUI trong Manager hoặc dùng CLI."));
  // Only a fixed command reaches the shell. Untrusted fields are JSON encoded,
  // then transported in a fixed-name environment variable with a base64 alphabet.
  const payload = encodeRequest(request);
  return new Promise((resolve, reject) => {
    const name = `houji_${Date.now()}_${serial++}`;
    const cleanup = () => { clearTimeout(timer); delete window[name]; };
    const timer = setTimeout(() => { cleanup(); reject(new Error("Bridge không phản hồi. Kiểm tra trạng thái trước khi thử lại.")); }, 75000);
    window[name] = (_errno: number, stdout: string, _stderr: string) => {
      cleanup();
      try {
        const result = JSON.parse(stdout);
        if (!result.ok) throw new Error(result.error || "Thao tác không thành công.");
        resolve(result.data as T);
      } catch (error) { reject(error instanceof Error ? error : new Error("Phản hồi backend không hợp lệ.")); }
    };
    try { window.ksu!.exec(COMMAND, JSON.stringify({ env: { HOUJI_REQUEST_B64: payload } }), name); }
    catch { cleanup(); reject(new Error("Manager không hỗ trợ bridge cần thiết. Dùng CLI houjictl.")); }
  });
}

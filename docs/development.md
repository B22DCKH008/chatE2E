# Quy ước phát triển cho hai người

## Trước khi tách nhánh

Hai người đọc API/schema và chạy `docker compose up --build -d --wait`, sau đó `docker compose --profile test run --rm test`. Mốc bàn giao bước 1 là health/readiness hoạt động, migration chạy lại được, unit/integration/race tests qua và API contract có kiểm tra tự động.

Nền đã qua các kiểm tra Docker và integration trên máy phát triển ngày 02/10/2026. Xem [kết quả nghiệm thu và sự cố đã khắc phục](verification.md); đồng đội chạy lại các lệnh trên sau khi clone để xác nhận môi trường của mình.

Thư mục dự án không tự tạo tài khoản GitHub, remote hay push. Khi đưa mã nguồn lên Git, workflow `.github/workflows/backend.yml` sẽ chạy trên push/pull request.

## Ownership và thứ tự bước 2

| Người 1 | Người 2 |
| --- | --- |
| Auth + device registration, implement `Verifier` | Message repository + inbox repository |
| Prekey upload/atomic claim | Transactional outbox + consumer idempotency |
| Device revoke và session rotation | Message handlers gắn `auth.Require` |
| Group membership/epoch/authorization | Offline sync, ACK và WebSocket |

Trong lúc auth chưa xong, Người 2 kiểm thử service qua dependency injection và fake verifier **chỉ trong `_test.go`**. Không thêm bypass auth hoặc token cố định vào server chạy thật. Có thể phát triển song song sau khi chốt signature của interface; không cần đợi toàn bộ nhóm auth hoàn thành.

Nhánh gợi ý: `feat/auth-devices`, `feat/key-directory`, `feat/message-inbox`, `feat/outbox-realtime`. Chia PR nhỏ theo luồng sử dụng; review chéo các thay đổi auth, group permission, schema và event.

`cmd/server`, `internal/platform`, `api/openapi.yaml`, `go.mod/go.sum` và migration là vùng dùng chung. Người thay đổi chủ động báo người còn lại để tránh sửa cùng điểm. Không dùng common package để dồn mọi logic nghiệp vụ.

## Migration

- File `NNNN_description.up.sql`, số tăng dần, thống nhất số trước khi tạo hai migration cùng lúc.
- Không sửa migration đã áp dụng/chia sẻ. Checksum khác làm setup thất bại; thêm migration mới để sửa.
- SQL chạy trong transaction và dưới advisory lock; không đặt `BEGIN/COMMIT` trong file.
- Dùng migration forward-only; thay đổi xóa/đổi cột cần kế hoạch dữ liệu và review riêng.
- Bước 1 dùng chung DB account cho tiện local. Production tách quyền DDL của setup và DML của API.
- PostgreSQL là nguồn bền vững. Không chuyển inbox/outbox sang Redis làm bản duy nhất.

## Logging và cấu hình

Không log request body, ciphertext, Authorization/cookie, mật khẩu, private key, refresh token, URL query hoặc panic value. Logger nền chỉ ghi request ID, status và duration; tránh label metrics chứa user/device/message ID để không tăng cardinality hoặc lộ metadata.

Cấu hình lỗi chỉ nêu tên biến và dạng giá trị mong đợi. `.env` không commit. Bộ công cụ/cache local nằm trong `.tools/` và `.cache/`, được gitignore.

## Definition of Done cho từng PR

1. API và schema thay đổi đồng bộ; `x-status` chỉ chuyển sang implemented khi handler hoạt động và có auth/authorization cần thiết.
2. Có test hành vi quan trọng: quyền truy cập, retry/concurrency, giới hạn đầu vào, không mất dữ liệu khi dependency lỗi.
3. `gofmt`, `go vet`, unit test qua; thay đổi persistence/broker chạy thêm integration và race detector.
4. README/tài liệu phản ánh đúng phần chạy được; không mô tả contract dự kiến như chức năng đã hoàn thành.

Trước module key directory, cùng frontend làm thử nghiệm thư viện Signal và thống nhất bộ key, binary serialization, device lifecycle. Đây là quyết định cần dữ liệu từ client; skeleton chưa tự chọn thư viện mật mã thay cho frontend.


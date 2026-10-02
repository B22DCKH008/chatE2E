# Hợp đồng tích hợp backend

`backend/api/openapi.yaml` là hợp đồng HTTP. `x-status: implemented` đánh dấu route đã chạy; `planned` là thiết kế chờ triển khai. Thay đổi body, status, auth hoặc enum cần cập nhật OpenAPI trước khi merge.

## Quy ước HTTP

- Prefix nghiệp vụ: `/v1`; health không nằm trong prefix.
- JSON dùng `snake_case`, UUID dạng chuỗi, timestamp UTC RFC3339, dữ liệu nhị phân dùng standard padded base64.
- Xác thực HTTP dùng `Authorization: Bearer ...`; sender user/device lấy từ `auth.Principal`, không nhận từ body.
- Body tối đa 1 MiB, ciphertext/envelope tối đa 256 KiB sau decode. Tối đa 100 envelopes/request nhưng tổng body vẫn phải vừa giới hạn.
- `400`: input sai; `401`: thiếu/hết hạn credential; `403`: không có quyền; `404`: không tìm thấy; `409`: xung đột epoch/idempotency; `413`: quá lớn; `415`: sai media type; `429`: rate limit; `500`: lỗi nội bộ; `503`: dependency chưa sẵn sàng.
- Không trả error từ DB/broker trực tiếp. Mã lỗi ổn định, message không chứa input bí mật. Mỗi response có `X-Request-ID` do server sinh.
- Health/readiness có schema riêng. Những lỗi thông thường dùng schema dưới đây.

```json
{
  "error": {
    "code": "UNAUTHORIZED",
    "message": "Authentication required",
    "request_id": "6429d5c994934c369fe6947b963aa05f"
  }
}
```

Các mã tương lai cần thống nhất: `DEVICE_REVOKED`, `GROUP_EPOCH_MISMATCH`, `IDEMPOTENCY_CONFLICT`, `RATE_LIMITED`. CORS chỉ cho phép origin cấu hình; không bật cookies/credentials trong skeleton.

## Ranh giới module

`auth.Verifier.Verify(ctx, token) -> Principal` thuộc Người 1. Verifier phải kiểm tra chữ ký với allowlist thuật toán, issuer/audience, expiry và trạng thái session/device. Parse JWT mà không verify không đáp ứng interface. JWT/key signing và password hashing được implement ở bước 2; refresh token chỉ lưu hash, giữ bản ghi đã thu hồi để phát hiện reuse theo family.

`groups.Access.CanSend(ctx, tx, groupID, userID, epoch)` thuộc Người 1 và được Người 2 gọi trong transaction lưu message. Implementation phải lấy khóa group và kiểm tra thành viên/epoch bằng chính `tx`. Sau đó messaging kiểm tra toàn bộ recipient devices vẫn thuộc các thành viên đang hoạt động. Không chỉ kiểm tra quyền bằng Redis cache trước khi mở transaction.

Tạo/xóa device ảnh hưởng tập recipient của nhóm: khi triển khai multi-device cần phát sự kiện và phân phối lại Sender Key cho thiết bị mới. Việc đổi epoch không thể ép một client độc hại thật sự đổi key; client tin cậy phải thực hiện chính sách rotation.

## Idempotency và inbox

Client tạo `client_message_id` một lần cho mỗi logical send, retry giữ nguyên ciphertext đã serialize và tập recipients. Server canonicalize các trường ngữ nghĩa, sắp recipients theo device ID rồi tính SHA-256; không hash nguyên chuỗi JSON vì khoảng trắng/thứ tự key có thể khác. Cùng ID nhưng nội dung thay đổi trả 409. Không được mã hóa lại bằng ratchet mỗi lần retry cùng ID.

`message_envelopes.id` là PostgreSQL bigint và trả bằng **chuỗi thập phân** để không mất độ chính xác JavaScript. Sequence được cấp trước commit nên không phải thứ tự commit tuyệt đối. Cursor chỉ dùng cho một lượt phân trang pending inbox; bắt đầu lại không có cursor khi hết trang/reconnect để nhận cả transaction commit muộn. ACK chỉ áp dụng cho đúng recipient device; retry ACK phải an toàn.

Client chỉ ACK khi đã lưu bền vững; không dùng ACK này làm read receipt. Receipt đọc là tính năng bước sau, phải có lựa chọn privacy vì tiết lộ trạng thái đọc.

## NATS và WebSocket

Stream: `SECUREAI_EVENTS`; subject bền vững bắt đầu bằng `secureai.events.`. Phiên bản nằm cuối subject.

| Subject | Producer | Ý nghĩa |
| --- | --- | --- |
| `secureai.events.message.accepted.v1` | Outbox worker | Message/envelopes đã commit |
| `secureai.events.group.changed.v1` | Outbox worker | Membership epoch đã đổi |
| `secureai.events.device.revoked.v1` | Outbox worker | Device/session bị thu hồi |

Payload sự kiện dự kiến:

```json
{
  "event_id": "f0503e7e-68dc-4d1d-bb28-4ce4990f14de",
  "version": 1,
  "occurred_at": "2026-10-02T00:00:00Z",
  "message_id": "83dfcd6c-080c-401f-9f0a-c714cb6c824d"
}
```

Giới hạn event 16 KiB; chỉ chứa ID và metadata cần thiết, không chứa key bí mật, body/ciphertext. `message_id` dùng cho message event; group/device event dùng ID và epoch thích hợp. Publish ACK, consumer ACK và device ACK là ba mốc khác nhau. Consumer crash/retry không được làm mất pending envelope.

WebSocket tương lai lấy ticket qua `POST /v1/realtime/tickets`, dùng ticket một lần, device-bound, TTL 30 giây trong Redis. Handshake bắt buộc kiểm tra Origin và consume ticket atomically; không đưa access/refresh token dài hạn vào URL. Request URL/query không được log ở gateway/proxy. Thiết bị bị revoke phải đóng socket đang mở.

Frame JSON dự kiến:

```json
{
  "type": "message.available",
  "event_id": "f0503e7e-68dc-4d1d-bb28-4ce4990f14de",
  "data": { "envelope_id": "42" }
}
```

Ban đầu WebSocket có thể báo có tin, client lấy ciphertext qua inbox để tái sử dụng delivery semantics. Presence/typing là sự kiện ephemeral có TTL, không ghi vào stream bền vững. Routing giữa gateway instances sẽ do Người 2 implement; không dùng queue subscriber bất kỳ rồi mặc định instance đó đang giữ socket người nhận.


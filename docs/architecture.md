# Kiến trúc nền

## Phạm vi

Backend là một ứng dụng Go chia module, dùng `net/http`, `pgxpool`, `go-redis` và `nats.go/jetstream`. Database là nguồn dữ liệu bền vững; Redis phục vụ trạng thái ngắn hạn. Mọi private key, Signal session và Sender Key nằm ở client. ONNX/AI phân tích nội dung chạy ở client, không có endpoint tải plaintext lên server.

```text
Client — HTTPS / WSS — Go API và gateway
                         |          |
                    PostgreSQL    Redis
                         |
                  outbox worker (bước 2)
                         |
                  NATS JetStream
                         |
                  delivery worker (bước 2)
                         |
                 gateway đang giữ socket
```

Nền hiện tại chỉ kết nối và kiểm tra các dependency. Worker, gateway và luồng gửi chưa được triển khai. Gateway tương lai giữ socket trong RAM nên không hoàn toàn stateless; các instance dùng chung registry/routing và khôi phục delivery qua inbox PostgreSQL khi reconnect.

## Schema ban đầu

| Bảng | Dữ liệu | Chủ trì |
| --- | --- | --- |
| `users` | Username và password hash; không chứa password gốc | Người 1 |
| `devices` | Public identity key, phiên bản identity, trạng thái thu hồi | Người 1 |
| `auth_sessions` | Refresh token hash, token family, hạn dùng, thu hồi | Người 1 |
| `signed_prekeys` | Public signed prekey và chữ ký | Người 1 |
| `one_time_prekeys` | Public prekey và thời điểm claim | Người 1 |
| `groups`, `group_members` | Thành viên, vai trò, membership epoch | Người 1 |
| `messages` | Metadata, client ID, hash request dùng kiểm tra retry | Người 2 |
| `message_envelopes` | Ciphertext cho từng thiết bị, trạng thái giao/đọc | Người 2 |
| `outbox_events` | Sự kiện đang chờ publish; chỉ chứa ID và routing metadata | Người 2 |
| `audit_events` | Ai thực hiện hành động gì, kết quả, thời điểm | Cả hai |
| `schema_migrations` | Phiên bản/checksum migration | Platform |

Thiết kế ban đầu dùng một envelope cho mỗi recipient device; ciphertext nhóm có thể được lặp trong các envelope. Đây là lựa chọn đơn giản cho đồ án, cần đo dung lượng trước khi tối ưu shared group ciphertext. Chưa có bảng conversation để tránh trùng ý nghĩa với group; danh sách hội thoại phía client có thể suy ra từ metadata đã giải mã. Nếu cần chỉ mục hội thoại phía server, thêm migration sau khi chốt nhu cầu.

Một signed prekey đang hoạt động/thiết bị được bảo đảm bằng partial unique index. Claim one-time prekey phải dùng transaction với `FOR UPDATE SKIP LOCKED`, cập nhật `claimed_at` và trả bundle trong cùng transaction. API phải rate limit việc claim và xử lý trường hợp hết key. Chưa có logic claim trong bước 1.

`groups.epoch` là phiên bản thành viên, **không phải Sender Key**. Mỗi sender device quản lý Sender Key riêng trên client. Khi thành viên thay đổi, phải khóa row group, kiểm tra quyền, tăng epoch và ghi sự kiện outbox trong một transaction. Gửi tin nhóm lấy cùng khóa trong transaction lưu message để không có race giữa gửi và xóa thành viên. Owner và hàng membership tương ứng phải được tạo/cập nhật atomically; FK/index chưa thể thay thế các quy tắc nghiệp vụ đó.

## Tính bền vững và delivery

1. Server xác thực device; kiểm tra schema, ciphertext size, recipients và group epoch.
2. Trong một DB transaction: lưu message, envelopes và outbox event. Unique `(sender_device_id, client_message_id)` bảo vệ retry.
3. Commit thành công mới trả `202 Accepted`.
4. Outbox worker publish với `Nats-Msg-Id = event_id`, chờ publish ACK rồi ghi `published_at`.
5. Nếu worker chết giữa publish và cập nhật DB, sự kiện sẽ được publish lại. Consumer phải idempotent ngoài cửa sổ dedup của NATS.
6. Socket delivery chỉ là một lần thử. Envelope vẫn pending cho đến khi client lưu bền vững và ACK theo envelope ID.
7. Reconnect đọc inbox chưa ACK; bản sao được dedup ở client trước khi ảnh hưởng ratchet hoặc UI.

Mục tiêu là at-least-once với xử lý idempotent, chưa hứa exactly-once. Sự kiện broker chỉ chứa tham chiếu message/envelope; nội dung được lấy từ DB sau kiểm tra quyền. Stream local giới hạn 256 MiB/7 ngày; worker phải giữ outbox chưa publish khi broker đầy và có cơ chế retry/quarantine được triển khai ở bước 2. Không có dead-letter queue tự động ở bước 1.

Đề xuất retention message offline là 7 ngày, cần chốt với nhóm. Hiện chỉ có `expires_at` và index; chưa có job xóa dữ liệu. ACK không đồng nghĩa xóa ngay. Khi xây cleanup, phải phối hợp thời hạn message, event và idempotency để retry cũ không tạo message mới ngoài ý muốn.

## Ranh giới bảo mật

E2EE bảo vệ nội dung khi thực hiện đúng ở hai client; backend vẫn thấy danh tính, thiết bị, thành viên nhóm, thời điểm và kích thước. Zero-knowledge trong đồ án chỉ nói về **nội dung tin nhắn**, không có nghĩa ẩn metadata hoặc một chứng minh zero-knowledge mật mã.

Schema không có cột plaintext tin nhắn nhưng server không thể chứng minh byte được gửi là ciphertext hợp lệ. Kiểm thử E2EE phải dùng client thật và kiểm tra DB/log sau gửi. Server cung cấp public key vẫn có nguy cơ tráo key: client cần xác minh chữ ký, theo dõi identity change và có cơ chế safety number/verification phù hợp thư viện đã chọn.

Chưa chốt thư viện Signal cho React/TypeScript. Trước khi implement key directory phải làm thử nghiệm liên thông client, chốt serialization, loại/bộ key và rotation. Schema hiện hỗ trợ identity + signed prekey + one-time prekey kiểu classical; nếu thư viện dùng PQXDH/KEM prekey thì bổ sung contract/migration tương ứng. Không coi việc có các bảng public key là đã tích hợp Signal Protocol.

API chưa có xác thực thật, rate limit, TLS termination hoặc observability đầy đủ. Middleware `auth.Require` có interface để tích hợp và từ chối truy cập khi verifier thiếu. Các route nghiệp vụ chưa được mở. Cần cấu hình log ở reverse proxy để không ghi token/ticket/query/body khi bổ sung WebSocket.

Tài liệu kỹ thuật tham khảo: [Go net/http](https://pkg.go.dev/net/http), [pgxpool](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool), [NATS consumers](https://docs.nats.io/learn/jetstream/pull-consumers), [Signal specifications](https://signal.org/docs/).


# Kiểm chứng nền backend

## Kết quả nghiệm thu local ngày 02/10/2026

| Kiểm tra | Kết quả |
| --- | --- |
| `docker compose config --quiet` | Exit 0 |
| `docker compose run --rm --no-deps nats --version` | `v2.11.17`, exit 0 |
| `docker compose --parallel 1 up --build -d --wait` | Build thành công; API, PostgreSQL, Redis, NATS healthy |
| Setup/migration/provisioning | Exit 0; chạy lại thành công trên cùng volumes |
| `docker compose --profile test run --rm test` | Exit 0 với `-race -count=1 -v`; tất cả test PASS |
| `TestFoundation` | PASS; chạy thật với PostgreSQL, Redis, JetStream |
| `TestHTTPFoundation` | PASS; gọi API thật qua mạng Docker |
| `GET http://localhost:8080/health/ready` | HTTP 200; `postgres`, `schema`, `redis`, `jetstream` đều `ok` |
| `go mod verify`, `go vet ./...` | Exit 0; dependencies đúng checksum, không có lỗi vet |
| `gofmt -l .` | Không có file cần định dạng lại |

Không có integration test bị skip trong lần chạy Docker này, không có cảnh báo data race. Đây là kết quả kiểm tra local; workflow GitHub Actions chỉ được xác nhận sau khi repository được push và workflow chạy trên GitHub.

Phạm vi nghiệm thu là bước 1. Những endpoint nghiệp vụ đang có `x-status: planned` vẫn thuộc bước 2, chưa được coi là chức năng đã triển khai.

## Sự cố Docker/NATS ngày 02/10/2026

Container NATS local thoát với exit code `135` ngay cả khi chạy `--version`, trước khi đọc `deploy/nats/nats.conf`. Kiểm tra binary trong image cho thấy file bị cắt ngắn:

| Bản binary Linux amd64 | Kích thước | SHA-256 |
| --- | ---: | --- |
| Image local bị hỏng | 8.388.608 byte | `f83919b3de809ac2c73cbf864a193d1cdc026569273a943d28d931ca1429f0fb` |
| Bản chính thức 2.11.17 và image đã tải lại | 16.968.836 byte | `40e725f0fcd8142888ed87b779fc75d5f48224c737b916499c785ecf8157e610` |

Đối chiếu với [release NATS 2.11.17](https://github.com/nats-io/nats-server/releases/tag/v2.11.17). Archive Linux amd64 chính thức có SHA-256 `ad608dd0ea8bbfa58c40e3b8f58ed67478eec26166ec49c502db8d17589b51b5`, đã được kiểm tra trước khi giải nén để so sánh.

Cách khắc phục đã thực hiện: kiểm tra image chỉ được container NATS của SecureAI sử dụng, xóa container đã dừng và image local bị hỏng, tải lại image chính thức. Không xóa volume PostgreSQL, Redis hoặc NATS. Sau khi tải lại, binary có đúng kích thước/checksum và `nats-server --version` trả `v2.11.17`, exit code 0.

`compose.yaml` khóa NATS bằng digest multi-platform `sha256:e4bf19f15fd3218814a4e3c9e0064e1334bd8aa20d5984b9f1a0afd084f8cc00`. Digest khóa nguồn image giữa các máy; nếu dữ liệu image đã giải nén trên ổ đĩa local bị hỏng, cần thay bản cache hỏng bằng bản tải mới.

## Cách nghiệm thu trên máy đồng đội

```powershell
docker compose config --quiet
docker compose run --rm --no-deps nats --version
docker compose --parallel 1 up --build -d --wait
docker compose --profile test run --rm test
```

Trong log phải có `PASS` cho cả `TestFoundation` và `TestHTTPFoundation`, không phải `SKIP`. Bộ test chạy `-race -count=1 -v`. Container test đợi API healthy và nhận `INTEGRATION_TEST=1`, `API_BASE_URL=http://api:8080` từ Compose.

`TestFoundation` kiểm tra migration chạy đồng thời/chạy lại, constraint dữ liệu, PostgreSQL/Redis/JetStream, publish/consume/ACK và deduplication. `TestHTTPFoundation` kiểm tra binary API thật qua mạng container: liveness/readiness, HEAD không có body, lỗi JSON và request ID.

Trên máy hiện tại, Redis dùng cổng host `16379` do `6379` đã có ứng dụng khác sử dụng. Đây là override trong `.env` không commit; kết nối giữa container vẫn dùng `redis:6379`. Mã Go và image build giới hạn mức song song để phù hợp máy ít RAM.

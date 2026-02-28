# SMR デバイス検証 — 非 SMR デバイス誤指定の防止

## Context

`smr.Open()` に非 SMR デバイスのパスを渡した場合、現状は REPORT ZONES の SCSI エラーで暗黙的に失敗するだけ。Host-Aware SMR（Sequential Write Preferred）が渡された場合は Open が成功してしまい、ZTL の前提（Host-Managed = 厳密な順序制約）と矛盾する動作になる危険がある。

`discover()` の先頭に SCSI INQUIRY + VPD ページによる明示的なデバイス種別チェックを追加し、非 SMR / 非 Host-Managed デバイスを早期に弾く。

## 方針

SCSI 仕様で定義された2段階の検証を行う:

1. **Standard INQUIRY** — Peripheral Device Type が `0x14`（Host-Managed Zoned Block Device）であることを確認
2. **VPD Page 0xB1**（Block Device Characteristics）— Zoned フィールドが `0x01`（Host-Managed）であることを確認

どちらか一方だけでも十分だが、古いファームウェアで VPD 0xB1 未対応のケースや、Peripheral Device Type が `0x00`（通常ブロックデバイス）のまま VPD で Host-Managed を返すケースの両方をカバーするため、フォールバック付きの2段階にする。

### 判定ロジック

```
1. Standard INQUIRY を送信
2. Peripheral Device Type をチェック:
   - 0x14 → Host-Managed 確定 → OK
   - 0x00 → 通常ブロックデバイスとして報告されている → VPD で追加確認
   - その他 → エラー（SMR ドライブではない）
3. VPD 0xB1 を送信（PDT=0x00 の場合のみ）
4. Zoned フィールド（byte 8, bits 5:4）をチェック:
   - 0b01 → Host-Aware（警告ログ出力、許可するがログで注意喚起）
   - 0b10 → Host-Managed → OK（VPD で正しく報告）
   - 0b00 → 非ゾーンデバイス → エラー
   - 0b11 → 予約値 → エラー
```

## 変更ファイル

| ファイル | 操作 | 内容 |
|---------|------|------|
| `internal/backend/smr/cdb.go` | 変更 | `buildInquiryCDB`, `buildInquiryVPDCDB` 追加 |
| `internal/backend/smr/parse.go` | 変更 | `parseInquiryBasic`, `parseVPDB1Zoned` 追加 |
| `internal/backend/smr/smr.go` | 変更 | `discover()` 先頭に `verifyDevice()` 呼び出し追加 |
| `internal/backend/smr/cdb_test.go` | 変更 | INQUIRY CDB ビルダーのテスト追加 |
| `internal/backend/smr/parse_test.go` | 変更 | INQUIRY / VPD パーサーのテスト追加 |
| `internal/backend/smr/mock_transport_test.go` | 変更 | mock に INQUIRY 応答追加、検証テスト追加 |
| `pkg/zbc/constants.go` | 変更 | `OpcodeInquiry`, `PeripheralDeviceTypeZBC`, `VPDPageBlockDevChar` 定数追加 |

## 詳細設計

### 1. `pkg/zbc/constants.go` に追加する定数

```go
OpcodeInquiry                    = 0x12
PeripheralDeviceTypeZBC   uint8  = 0x14  // Host-Managed Zoned Block Device
VPDPageBlockDeviceChar    uint8  = 0xB1  // Block Device Characteristics
```

### 2. `cdb.go` に追加する CDB ビルダー

```go
// buildInquiryCDB — Standard INQUIRY (EVPD=0)
func buildInquiryCDB(allocLen uint16) []byte  // 6-byte CDB

// buildInquiryVPDCDB — VPD INQUIRY (EVPD=1, page code)
func buildInquiryVPDCDB(pageCode uint8, allocLen uint16) []byte  // 6-byte CDB
```

INQUIRY CDB は 6 バイト（既存の 16 バイト CDB と異なる）:
- `[0]` = 0x12 (opcode)
- `[1]` = EVPD bit (0 or 1)
- `[2]` = Page Code (EVPD=1 の場合のみ)
- `[3:5]` = Allocation Length (big-endian 16-bit)
- `[5]` = Control (0x00)

### 3. `parse.go` に追加するパーサー

```go
// parseInquiryBasic — Standard INQUIRY レスポンスから PDT を抽出
func parseInquiryBasic(buf []byte) (peripheralDeviceType uint8, err error)

// parseVPDB1Zoned — VPD B1 レスポンスから Zoned フィールドを抽出
// 戻り値: 0=非ゾーン, 1=Host-Aware, 2=Host-Managed, 3=予約
func parseVPDB1Zoned(buf []byte) (uint8, error)
```

### 4. `smr.go` の `verifyDevice()` メソッド

```go
func (s *SMRBackend) verifyDevice() error {
    // 1. Standard INQUIRY
    // 2. PDT チェック (0x14 → OK, 0x00 → VPD 確認, other → error)
    // 3. VPD 0xB1 チェック (Host-Managed → OK, Host-Aware → warn+OK, other → error)
}
```

`discover()` の先頭で `s.verifyDevice()` を呼び出す。

### 5. Host-Aware の扱い

Host-Aware デバイスは REPORT ZONES が成功し、ゾーン管理も動作する。ただし書き込み順序制約が「推奨」であり「必須」ではないため、ZTL が想定する「Sequential Write Required」と微妙に異なる。完全にブロックするのではなく、`slog.Warn` でログ出力して続行する（ユーザーが意図的に使うケースを許容）。

## 検証方法

```bash
# テストが通ること
go test ./internal/backend/smr/... -v
go test -race ./...

# ビルド確認
go build ./...
GOOS=windows GOARCH=amd64 go build ./cmd/zns-iscsi
```

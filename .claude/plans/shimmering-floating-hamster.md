# Windows SPTI 対応 - ZNS/SMR iSCSI Target

## Context

現在の SMR バックエンドは Linux SG_IO ioctl 専用。Linux でしか実 SMR HDD にアクセスできないため、「Linux で動かすなら既存ツール（btrfs + targetcli）で十分」という問題がある。Windows SPTI（SCSI Pass-Through Interface）対応を追加し、Windows 上で直接 SMR HDD を使えるようにする。Docker（Linux コンテナ）はそのまま残す。

## 方針

SCSI CDB（コマンドバイト列）はOS非依存。SG_IO と SPTI で異なるのは「CDB をデバイスに送る方法」だけ。`ScsiTransport` インターフェースを導入し、Linux/Windows で実装を差し替える。

## 変更概要

### Phase 1: Tidy — CDB/パース処理をOS非依存に抽出（振る舞い変更なし）

1. `internal/backend/smr/zbc.go` から CDB ビルダー関数を `cdb.go`（build tag なし）に移動
   - `buildReportZonesCDB`, `buildZoneActionCDB`, `buildRead16CDB`, `buildWrite16CDB`
2. `internal/backend/smr/zbc.go` からバイナリパース関数を `parse.go`（build tag なし）に移動
   - `parseZoneDescriptor`, 新規 `parseReportZonesResponse`
3. `zbc_test.go` の `//go:build linux` タグを削除 → `cdb_test.go` + `parse_test.go` に分割
4. `ScsiTransport` インターフェース定義 → `scsi_transport.go`（build tag なし）
   ```go
   type ScsiTransport interface {
       ScsiRead(cdb []byte, buf []byte, timeoutMs uint32) error
       ScsiWrite(cdb []byte, buf []byte, timeoutMs uint32) error
       ScsiNoData(cdb []byte, timeoutMs uint32) error
       Close() error
   }
   ```
5. 既存 SG_IO を `linuxTransport` にラップ → `transport_linux.go`（`//go:build linux`）
6. `smr.go` を `fd int` から `ScsiTransport` に変更、build tag 削除。`Open()` を `smr_linux.go` に移動

### Phase 2: Windows SPTI トランスポート追加

7. `spti.go`（`//go:build windows`）— SPTI 構造体定義
   - `scsiPassThroughDirect` 構造体（Windows C 構造体と同じレイアウト）
   - `IOCTL_SCSI_PASS_THROUGH_DIRECT = 0x4D014`
8. `transport_windows.go`（`//go:build windows`）— `windowsTransport` 実装
   - `windows.CreateFile` で `\\.\PhysicalDriveN` を開く
   - `windows.DeviceIoControl` で SPTI 実行
   - エラーに「run as Administrator」ヒントを含める
9. `smr_windows.go`（`//go:build windows`）— `Open()` 関数

### Phase 3: cmd/ とコンフィグの修正

10. `cmd/zns-iscsi/smr_windows.go`（`//go:build windows`）— `openSMRBackend` 追加
11. `cmd/zns-iscsi/smr_other.go` — build tag を `!linux && !windows` に変更
12. シグナル処理のクロスプラットフォーム化
    - `signal_unix.go`（`//go:build !windows`）: `SIGTERM + SIGINT`
    - `signal_windows.go`（`//go:build windows`）: `os.Interrupt`
    - `main.go` から `syscall` import 削除、`notifySignals(sigCh)` に変更
13. `config.yaml.example` に Windows パス例（`\\.\PhysicalDrive1`）追加
14. Makefile に `build-windows` ターゲット追加

### Phase 4: テスト

15. `mock_transport_test.go`（build tag なし）— SMRBackend のモックテスト
16. `spti_test.go`（`//go:build windows`）— 構造体サイズ検証

## ファイル変更一覧

| ファイル | 操作 | build tag |
|---------|------|-----------|
| `internal/backend/smr/cdb.go` | 新規（zbc.go から抽出） | なし |
| `internal/backend/smr/parse.go` | 新規（zbc.go から抽出） | なし |
| `internal/backend/smr/scsi_transport.go` | 新規 | なし |
| `internal/backend/smr/transport_linux.go` | 新規（sgioctl.go ラッパー） | linux |
| `internal/backend/smr/transport_windows.go` | 新規 | windows |
| `internal/backend/smr/spti.go` | 新規 | windows |
| `internal/backend/smr/smr.go` | 変更: fd→ScsiTransport | なし（tag削除） |
| `internal/backend/smr/smr_linux.go` | 新規: Open() | linux |
| `internal/backend/smr/smr_windows.go` | 新規: Open() | windows |
| `internal/backend/smr/zbc.go` | 変更: CDB/parse移動後の残り | linux |
| `internal/backend/smr/cdb_test.go` | リネーム（tag削除） | なし |
| `internal/backend/smr/parse_test.go` | 新規 | なし |
| `internal/backend/smr/mock_transport_test.go` | 新規 | なし |
| `internal/backend/smr/spti_test.go` | 新規 | windows |
| `cmd/zns-iscsi/smr_windows.go` | 新規 | windows |
| `cmd/zns-iscsi/smr_other.go` | 変更: tag | !linux && !windows |
| `cmd/zns-iscsi/signal_unix.go` | 新規 | !windows |
| `cmd/zns-iscsi/signal_windows.go` | 新規 | windows |
| `cmd/zns-iscsi/main.go` | 変更: signal | なし |
| `config.yaml.example` | 変更: Windows例追加 | — |
| `Makefile` | 変更: build-windows追加 | — |

## 検証方法

```bash
# macOS/Linux: 既存テストが壊れていないこと
go build ./...
go test ./...
go test -race ./...

# Windows クロスコンパイル
GOOS=windows GOARCH=amd64 go build ./cmd/zns-iscsi

# フロントエンド（変更なし）
cd web && npm run build && npx vitest run
```

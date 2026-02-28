# ZNS/SMR iSCSI Target

SATA SMR (Shingled Magnetic Recording) Host-Managed HDD を、iSCSI 経由で通常のブロックデバイスとして利用可能にするターゲットサーバ。Zone Translation Layer (ZTL) がシーケンシャル書き込み制約を透過的に処理する。

> **Experimental** — このプロジェクトは Vibe-Coding による実験的な実装です。本番環境での運用は想定していません。データの整合性や耐障害性は保証されないため、重要なデータの保存には使用しないでください。

## 構成

```
iSCSI Initiator (Windows / Linux / macOS)
        │ TCP 3260
        ▼
  iSCSI Target Server
        │
   Zone Translation Layer (ZTL)
        │
  SMR Backend (SCSI CDB via SG_IO / SPTI)
        │
   Host-Managed SMR HDD
```

## Linux での既存の代替手段

Linux カーネルには Host-Managed SMR/ZNS デバイスを扱う仕組みが既にある。Linux だけで完結するなら、以下の組み合わせで本プロジェクトと同等のことが実現できる。

| 手段 | 概要 | コマンド例 |
|------|------|-----------|
| **btrfs + targetcli** | btrfs は Host-Managed SMR を直接サポートする。targetcli で iSCSI ターゲットとして公開すれば、Windows から通常のディスクとして接続可能 | `mkfs.btrfs /dev/sdX && targetcli` |
| **dm-zoned** | Device Mapper レイヤーで SMR デバイスを通常のブロックデバイスに変換する。変換後のデバイスを LIO/targetcli で公開できる | `dmzadm --format /dev/sdX && dmzadm --start /dev/sdX` |
| **zonefs** | ゾーンごとに1ファイルとして見せるファイルシステム。直接的な代替ではないが、ゾーンの可視化やデバッグに有用 | `mkzonefs /dev/sdX && mount -t zonefs /dev/sdX /mnt` |
| **f2fs** | Host-Managed SMR 対応の FTL 内蔵ファイルシステム | `mkfs.f2fs /dev/sdX` |

**本プロジェクトの存在意義**: 上記はすべて Linux カーネルの ZBC/ZAC サポートに依存する。Windows には同等の仕組みがなく、SMR HDD を直接活用する手段がない。本ツールは Windows SPTI を介して SMR HDD に直接アクセスし、OS に依存せず iSCSI ターゲットを提供する。

## クイックスタート

```bash
# ビルド
go build -o zns-iscsi ./cmd/zns-iscsi

# 設定ファイルを準備
cp config.yaml.example config.yaml
# config.yaml を環境に合わせて編集

# 起動（エミュレータモード）
./zns-iscsi -config config.yaml

# 起動（実デバイスモード、要 root）
sudo ./zns-iscsi -config config.yaml
```

## SMR デバイスの確認方法

`backend: smr` で実デバイスを使う場合、対象デバイスが **Host-Managed** であることを事前に確認する。本ツールは起動時に SCSI INQUIRY で自動検証するが、Linux では既存のコマンドで同等の確認ができる。

### sg_inq（sg3_utils）

```bash
# Peripheral Device Type の確認
sudo sg_inq /dev/sdX
```

出力例（Host-Managed の場合）:

```
  Peripheral device type: host managed zoned block device
```

`Peripheral device type` が `host managed zoned block device`（PDT=0x14）であれば OK。`disk`（PDT=0x00）の場合は VPD で追加確認が必要。

### sg_vpd（VPD page 0xB1: Block Device Characteristics）

```bash
# Zoned フィールドの確認
sudo sg_vpd -p bdc /dev/sdX
```

出力例:

```
  Zoned block device model: host-managed
```

| 値 | 意味 | 本ツールでの扱い |
|----|------|-----------------|
| `host-managed` | Host-Managed (HM) | OK |
| `host-aware` | Host-Aware (HA) | **拒否** — 書き込み順序制約が advisory のため ZTL と非互換 |
| `none (or not reported)` | 非ゾーンデバイス | 拒否 |

### lsscsi

```bash
# デバイス一覧と型の確認
lsscsi
```

出力例:

```
[0:0:0:0]    zbc     ATA      HGST ...    /dev/sdb
```

TYPE 列が `zbc` であれば Host-Managed。`disk` の場合は sg_vpd で追加確認する。

### パッケージのインストール

```bash
# Debian / Ubuntu
sudo apt install sg3-utils lsscsi

# RHEL / Fedora
sudo dnf install sg3_utils lsscsi
```

## 設定

`config.yaml.example` を参照。主要な設定項目:

| セクション | キー | 説明 |
|-----------|------|------|
| `device.backend` | `emulator` / `smr` | バックエンド種別 |
| `device.path` | `/dev/sdX` | SMR デバイスのパス（`smr` 時必須） |
| `target.iqn` | `iqn.2026-02.io.zns:target0` | iSCSI Qualified Name |
| `target.portal` | `0.0.0.0:3260` | リッスンアドレス |

## 開発

```bash
# テスト
go test ./...

# Race detector 付き
go test -race ./...

# Windows クロスコンパイル
GOOS=windows GOARCH=amd64 go build ./cmd/zns-iscsi
```

## ライセンス

[MIT](LICENSE)

# ZNS/SMR iSCSI Target

An iSCSI target server that exposes SATA SMR (Shingled Magnetic Recording) Host-Managed HDDs as conventional block devices. The built-in Zone Translation Layer (ZTL) transparently handles the sequential write constraint.

> **Experimental** — This project was built through Vibe-Coding and is not intended for production use. Data integrity and fault tolerance are not guaranteed. Do not use this for important data.

[Japanese documentation / 日本語ドキュメント](README.ja.md)

## Architecture

```
iSCSI Initiator (Windows / Linux / macOS)
        | TCP 3260
        v
  iSCSI Target Server
        |
   Zone Translation Layer (ZTL)
        |
  SMR Backend (SCSI CDB via SG_IO / SPTI)
        |
   Host-Managed SMR HDD
```

## Existing Alternatives on Linux

The Linux kernel already has native support for Host-Managed SMR/ZNS devices. If you only need Linux, the following combinations achieve the same goal:

| Approach | Description | Example |
|----------|-------------|---------|
| **btrfs + targetcli** | btrfs natively supports Host-Managed SMR. Export via targetcli as an iSCSI target for Windows access | `mkfs.btrfs /dev/sdX && targetcli` |
| **dm-zoned** | Device Mapper layer that translates an SMR device into a conventional block device. Export the resulting dm device via LIO/targetcli | `dmzadm --format /dev/sdX && dmzadm --start /dev/sdX` |
| **zonefs** | Exposes each zone as a file. Not a direct alternative, but useful for zone visualization and debugging | `mkzonefs /dev/sdX && mount -t zonefs /dev/sdX /mnt` |
| **f2fs** | Flash-Friendly File System with built-in FTL; supports Host-Managed SMR | `mkfs.f2fs /dev/sdX` |

**Why this project exists**: All of the above depend on Linux kernel ZBC/ZAC support. Windows has no equivalent mechanism for directly utilizing SMR HDDs. This tool accesses SMR HDDs directly via Windows SPTI, providing an OS-independent iSCSI target.

## Quick Start

```bash
# Build
go build -o zns-iscsi ./cmd/zns-iscsi

# Prepare config
cp config.yaml.example config.yaml
# Edit config.yaml for your environment

# Run (emulator mode)
./zns-iscsi -config config.yaml

# Run (real device, requires root)
sudo ./zns-iscsi -config config.yaml
```

## Verifying Your SMR Device

When using `backend: smr`, the target device must be **Host-Managed**. The tool automatically verifies this via SCSI INQUIRY at startup, but you can check manually on Linux using standard tools.

### sg_inq (sg3_utils)

```bash
sudo sg_inq /dev/sdX
```

Expected output for Host-Managed:

```
  Peripheral device type: host managed zoned block device
```

If `Peripheral device type` shows `host managed zoned block device` (PDT=0x14), the device is supported. If it shows `disk` (PDT=0x00), check VPD for further details.

### sg_vpd (VPD page 0xB1: Block Device Characteristics)

```bash
sudo sg_vpd -p bdc /dev/sdX
```

Expected output:

```
  Zoned block device model: host-managed
```

| Value | Meaning | Handling |
|-------|---------|----------|
| `host-managed` | Host-Managed (HM) | Accepted |
| `host-aware` | Host-Aware (HA) | **Rejected** — write ordering is advisory, incompatible with ZTL |
| `none (or not reported)` | Non-zoned device | Rejected |

### lsscsi

```bash
lsscsi
```

Example output:

```
[0:0:0:0]    zbc     ATA      HGST ...    /dev/sdb
```

A TYPE of `zbc` indicates Host-Managed. If it shows `disk`, use sg_vpd for further verification.

### Installing the Tools

```bash
# Debian / Ubuntu
sudo apt install sg3-utils lsscsi

# RHEL / Fedora
sudo dnf install sg3_utils lsscsi
```

## Configuration

See `config.yaml.example` for all options. Key settings:

| Section | Key | Description |
|---------|-----|-------------|
| `device.backend` | `emulator` / `smr` | Backend type |
| `device.path` | `/dev/sdX` | SMR device path (required for `smr`) |
| `target.iqn` | `iqn.2026-02.io.zns:target0` | iSCSI Qualified Name |
| `target.portal` | `0.0.0.0:3260` | Listen address |

## Development

```bash
# Run tests
go test ./...

# With race detector
go test -race ./...

# Cross-compile for Windows
GOOS=windows GOARCH=amd64 go build ./cmd/zns-iscsi
```

## License

[MIT](LICENSE)

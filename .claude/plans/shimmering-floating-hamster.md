# Windows プリビルトバイナリ — GitHub Releases 自動配布

## Context

Windows ユーザーが Go のビルド環境なしで利用できるよう、プリビルトバイナリを GitHub Releases で配布する。タグ push 時に GitHub Actions で自動ビルド・リリースする。

## 方針

GitHub Actions ワークフローを1つ追加する。`v*` タグの push をトリガーに Windows amd64 バイナリをビルドし、GitHub Release を作成してアップロードする。

## 変更ファイル

| ファイル | 操作 | 内容 |
|---------|------|------|
| `.github/workflows/release.yml` | 新規 | タグ push 時のビルド・リリースワークフロー |
| `.gitignore` | 確認 | `bin/` が含まれていることを確認 |

## ワークフロー設計

### トリガー

```yaml
on:
  push:
    tags: ['v*']
```

### ジョブ

1. **test** — `go test -race ./...` で全テスト通過を確認
2. **release** — test 成功後にビルド・リリース

### ビルド対象

| OS | Arch | アーカイブ名 |
|----|------|-------------|
| windows | amd64 | `zns-iscsi_v0.0.1_windows_amd64.zip` |

アーカイブ内のファイル名は `zns-iscsi.exe`（バージョンなし、展開後すぐ使える）。
アーカイブ名にバージョン・OS・アーキテクチャを含めて一意にする。
`config.yaml.example` も同梱する。

Linux バイナリは今回はスキップ（Linux ユーザーは `go build` できる前提）。

### リリース作成

`softprops/action-gh-release` アクションで zip をアップロード。

## 検証方法

```bash
# ローカルでクロスコンパイルが通ること
GOOS=windows GOARCH=amd64 go build -o bin/zns-iscsi-windows-amd64.exe ./cmd/zns-iscsi

# タグを打って push → Actions が動くこと
jj bookmark create v0.0.1 -r @-
jj git push --bookmark v0.0.1
# GitHub の Actions タブと Releases を確認
```

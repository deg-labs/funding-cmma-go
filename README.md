# funding-cmma-go

Bybit または Bitget の USDT 無期限契約から Funding Rate を収集し、異常な金利を持つ銘柄を検索できる API サーバーです。`funding-cmma` の Go 実装版であり、既存の SQLite データベースと HTTP API の互換性を維持します。

## 機能

- データ収集（`fetcher`）
  - 取引所から USDT 無期限契約のシンボル一覧と Funding Rate 履歴を取得します。
  - SQLite の `funding_rates` テーブルへ UPSERT します。
  - 初回は履歴をまとめて取得し、2 回目以降は差分だけを取得します。欠損を検出した場合は自動で補完します。
  - goroutine のワーカープールとトークンバケットで同時実行数とリクエスト頻度を制御します。
- API サーバー（`api`）
  - `GET /funding-rates` で銘柄ごとの最新 Funding Rate を返します。
  - `GET /funding-rates/extreme-continuity` で閾値超過の継続性を分析します。
  - Swagger UI と OpenAPI 仕様を配信します。
  - `GET /healthz`、`GET /readyz`、`GET /metrics` を提供します。
- プロキシ（`nginx`）
  - `localhost:8002` で外部へ公開し、`api:8000` へ転送します。

## 必要要件

- Docker
- Docker Compose

バイナリを直接実行する場合は Go 1.25 以降が必要です。

## 構成

```text
funding-cmma-go/
├── api/             HTTP API（package main）
├── fetcher/         データ収集（package main）
├── internal/
│   ├── config/      環境変数の読み込みと検証
│   ├── exchange/    Bybit・Bitget クライアント
│   ├── funding/     ドメインモデルと継続性計算
│   ├── logging/     ログレベル別の出力先制御
│   └── store/       SQLite のスキーマとクエリ
├── nginx/           リバースプロキシ
└── docker-compose.yml
```

データの流れは次のとおりです。

1. `fetcher` が取引所 API から Funding Rate を取得します。
2. `fetcher` が `./data/cmma.db` へ保存します。
3. `api` が同じデータベースを読み取り専用で開きます。
4. `nginx` が `api:8000` へリクエストを転送します。

## 実行方法

1. 環境変数ファイルを作成します。

   ```bash
   cp .env.example .env
   ```

2. コンテナを起動します。

   ```bash
   docker compose up --build -d
   ```

3. ログを確認します。

   ```bash
   docker compose logs -f funding-cmma-go-fetcher
   docker compose logs -f funding-cmma-go-api
   ```

4. 停止します。

   ```bash
   docker compose down
   ```

データベースを初期化する場合は `./data/cmma.db` を削除してください。

### バイナリで実行する

```bash
make build
```

```bash
DB_PATH=./data/cmma.db HTTP_ADDR=:8000 ./bin/funding-cmma-api
```

```bash
EXCHANGE=bybit DB_PATH=./data/cmma.db ./bin/funding-cmma-fetcher
```

## 環境変数

| 変数 | 既定値 | 説明 |
| --- | --- | --- |
| `EXCHANGE` | `bybit` | `bybit` または `bitget` |
| `BITGET_PRODUCT_TYPE` | `usdt-futures` | Bitget の商品タイプ |
| `FETCH_INTERVAL_SECONDS` | `300` | 収集サイクルの間隔 |
| `CONCURRENCY_LIMIT` | `10` | 同時に実行する取引所リクエスト数 |
| `FUNDING_RATE_THRESHOLD` | `0.001` | 異常候補としてログに記録する閾値 |
| `FUNDING_RATE_HISTORY_LIMIT` | `200` | 初回取得と欠損補完の履歴件数 |
| `INCREMENTAL_HISTORY_LIMIT` | `10` | 通常サイクルの履歴件数 |
| `BYBIT_BASE_URL` | `https://api.bybit.com` | Bybit API のベース URL |
| `BITGET_BASE_URL` | `https://api.bitget.com` | Bitget API のベース URL |
| `REQUEST_TIMEOUT_SECONDS` | `10` | 取引所 API のタイムアウト |
| `REQUEST_RETRY_MAX` | `3` | 取引所 API の最大再試行回数 |
| `RATE_LIMIT_RPS` | `10` | 秒あたりのリクエスト数 |
| `RATE_LIMIT_BURST` | `20` | バーストで許可するリクエスト数 |
| `DB_PATH` | `/app/data/cmma.db` | SQLite のパス |
| `HTTP_ADDR` | `:8000` | API の待ち受けアドレス |
| `HTTP_READ_TIMEOUT_SECONDS` | `10` | API の読み取りタイムアウト |
| `HTTP_WRITE_TIMEOUT_SECONDS` | `10` | API の書き込みタイムアウト |
| `DB_MAX_OPEN_CONNS` | `4` | API が使う SQLite 接続数の上限 |
| `LOG_FORMAT` | `json` | `json` または `text` |
| `LOG_LEVEL` | `info` | `debug`、`info`、`warn`、`error` |

不正な値は既定値へ置き換えず、起動時にエラーとして終了します。

## API

API は `http://localhost:8002` で利用できます。

### API ドキュメント

- Swagger UI: `http://localhost:8002/funding-rates/docs`
- OpenAPI 仕様: `http://localhost:8002/funding-rates/openapi.json`

### `GET /funding-rates`

銘柄ごとの最新 Funding Rate を返します。

| パラメータ | 必須 | 既定値 | 説明 |
| --- | --- | --- | --- |
| `threshold` | はい | — | `\|rate\|` の下限。0 より大きい値 |
| `direction` | いいえ | `both` | `positive`、`negative`、`both` |
| `sort` | いいえ | `funding_abs_desc` | `funding_abs_desc`、`funding_rate_desc`、`funding_rate_asc`、`utilization_desc`、`symbol_asc` |
| `limit` | いいえ | `100` | 1 以上 500 以下 |
| `utilization_gte` | いいえ | — | 利用率の下限。0 以上 |

```bash
curl -s "http://localhost:8002/funding-rates?threshold=0.001&direction=both&sort=funding_abs_desc&limit=20"
```

```json
{
  "count": 1,
  "data": [
    {
      "symbol": "BTCUSDT",
      "funding_ts": 1700000000000,
      "next_funding_ts": 1700007200000,
      "funding": {
        "rate": 0.005,
        "direction": "positive",
        "utilization": 0.5
      },
      "constraints": {
        "interval_hours": 8,
        "cap": 0.01,
        "floor": -0.01
      }
    }
  ]
}
```

算出できない値は `null` になります。結果が 0 件の場合、`data` は `[]` です。

### `GET /funding-rates/extreme-continuity`

指定した銘柄の履歴から、閾値超過の継続性を計算します。

| パラメータ | 必須 | 既定値 | 説明 |
| --- | --- | --- | --- |
| `symbol` | はい | — | 分析対象のシンボル |
| `threshold` | はい | — | 「極端」と判断する閾値。0 より大きい値 |
| `lookback` | はい | — | 分析する履歴件数。2 以上 200 以下 |
| `direction` | いいえ | `both` | `positive`、`negative`、`both` |

```bash
curl -s "http://localhost:8002/funding-rates/extreme-continuity?symbol=BTCUSDT&threshold=0.001&lookback=24&direction=both"
```

履歴が存在しない場合は HTTP 404 を返します。

統計指標は次のとおりです。

- `total_hit_rate`: 取得できた履歴のうち、閾値を超えた割合
- `consecutive_hit_rate`: 最新から連続して閾値を超えた割合
- `average_run_length`: 閾値超過が連続した区間の平均長

`direction=both` の場合、`total_hit_rate` と `consecutive_hit_rate` は正負を区別しません。`average_run_length` は正の区間と負の区間を別々に数えます。

### エラーレスポンス

```json
{
  "error": {
    "code": "INVALID_INPUT",
    "message": "[query->threshold]: Input should be greater than 0"
  }
}
```

| ステータス | コード | 説明 |
| --- | --- | --- |
| 422 | `INVALID_INPUT` | 入力値が不正 |
| 404 | `HTTP_EXCEPTION` | 対象の履歴が存在しない |
| 500 | `INTERNAL_ERROR` | サーバー内部エラー |

### 運用エンドポイント

| パス | 説明 |
| --- | --- |
| `/healthz` | プロセスの生存確認 |
| `/readyz` | SQLite とスキーマの確認 |
| `/metrics` | Prometheus 形式のメトリクス |

`/metrics` は外部へ公開しないでください。

## データベース

`funding_rates` テーブルは既存 Python 実装と同一です。

```sql
CREATE TABLE IF NOT EXISTS funding_rates (
    symbol TEXT NOT NULL,
    funding_rate REAL NOT NULL,
    funding_rate_timestamp INTEGER NOT NULL,
    next_funding_time INTEGER,
    funding_interval INTEGER,
    funding_rate_cap REAL,
    funding_rate_floor REAL,
    created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    updated_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now')),
    PRIMARY KEY (symbol, funding_rate_timestamp)
);
```

`funding_rate_timestamp` と `next_funding_time` はミリ秒、`created_at` と `updated_at` は秒です。移行時にこの単位を変更しないでください。

書き込み側は WAL モードと接続数 1、読み取り側は `query_only` と読み取り専用 URI を使います。API はデータディレクトリへの書き込み権限を必要としないため、`./data` を読み取り専用でマウントできます。ただし WAL の共有メモリファイルを作成するため、ディレクトリ自体は書き込み可能である必要があります。

## 移行時の互換性

`funding-cmma` からの移行にあたり、次の挙動を維持しています。

- 既存の `cmma.db` をそのまま読み書きできます。
- JSON のフィールド名、型、`null` の扱いを維持します。
- 入力エラーは HTTP 422、履歴なしは HTTP 404 です。
- 最新レコードの検索は銘柄ごとの最大タイムスタンプを使います。
- 負の Funding Rate の利用率は、現行実装と同じく負の値になります。

最後の項目は API 仕様の「0.0〜1.0」と一致しません。移行時に挙動を変えないため、あえて現行の計算を維持しています。修正する場合は `utilization_gte` の検索結果も変わるため、破壊的変更として別途扱ってください。

## 開発

```bash
make fmt
make vet
make test
make race
```

テストは SQLite の一時ファイルと `httptest` を使うため、外部ネットワークへアクセスしません。

## ライセンス

BSD 3-Clause License。詳細は `LICENSE` を参照してください。

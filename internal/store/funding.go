package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/deg-labs/funding-cmma-go/internal/funding"
)

// UpsertRow は保存する 1 行です。
type UpsertRow struct {
	Symbol          string
	Rate            float64
	Timestamp       int64
	NextFundingTime *int64
	IntervalHours   *int
	Cap             *float64
	Floor           *float64
}

const upsertSQL = `
INSERT INTO funding_rates (
    symbol,
    funding_rate,
    funding_rate_timestamp,
    next_funding_time,
    funding_interval,
    funding_rate_cap,
    funding_rate_floor,
    updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, strftime('%s', 'now'))
ON CONFLICT(symbol, funding_rate_timestamp) DO UPDATE SET
    funding_rate = excluded.funding_rate,
    next_funding_time = excluded.next_funding_time,
    funding_interval = excluded.funding_interval,
    funding_rate_cap = excluded.funding_rate_cap,
    funding_rate_floor = excluded.funding_rate_floor,
    updated_at = excluded.updated_at`

// UpsertRows は 1 トランザクションで複数行を UPSERT します。
// 1 件でも失敗した場合はロールバックし、エラーを返します。
func UpsertRows(ctx context.Context, db *sql.DB, rows []UpsertRow) error {
	if len(rows) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin upsert: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	stmt, err := tx.PrepareContext(ctx, upsertSQL)
	if err != nil {
		return fmt.Errorf("prepare upsert: %w", err)
	}
	defer stmt.Close()

	for _, row := range rows {
		if _, err := stmt.ExecContext(ctx,
			row.Symbol,
			row.Rate,
			row.Timestamp,
			nullInt64(row.NextFundingTime),
			nullInt64FromInt(row.IntervalHours),
			nullFloat64(row.Cap),
			nullFloat64(row.Floor),
		); err != nil {
			return fmt.Errorf("upsert %s@%d: %w", row.Symbol, row.Timestamp, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit upsert: %w", err)
	}
	return nil
}

// LatestTimestamps は銘柄ごとの最新タイムスタンプを返します。
func LatestTimestamps(ctx context.Context, db *sql.DB) (map[string]int64, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT symbol, MAX(funding_rate_timestamp) FROM funding_rates GROUP BY symbol`,
	)
	if err != nil {
		return nil, fmt.Errorf("query latest timestamps: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var symbol string
		var ts int64
		if err := rows.Scan(&symbol, &ts); err != nil {
			return nil, err
		}
		result[symbol] = ts
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// LatestQuery は最新 Funding Rate の検索条件です。
type LatestQuery struct {
	Threshold      float64
	Direction      funding.Direction
	Sort           string
	Limit          int
	UtilizationGTE *float64
}

var sortClauses = map[string]string{
	"funding_rate_desc": "funding_rate DESC, symbol ASC",
	"funding_rate_asc":  "funding_rate ASC, symbol ASC",
	"funding_abs_desc":  "ABS(funding_rate) DESC, symbol ASC",
	"symbol_asc":        "symbol ASC",
	"utilization_desc":  "funding_utilization DESC, symbol ASC",
}

const latestSelect = `
WITH latest AS (
    SELECT symbol, MAX(funding_rate_timestamp) AS latest_ts
    FROM funding_rates
    GROUP BY symbol
),
joined AS (
    SELECT
        f.symbol,
        f.funding_rate,
        f.funding_rate_timestamp,
        f.next_funding_time,
        f.funding_interval,
        f.funding_rate_cap,
        f.funding_rate_floor,
        CASE
            WHEN f.funding_rate > 0 THEN f.funding_rate / f.funding_rate_cap
            WHEN f.funding_rate < 0 THEN f.funding_rate / ABS(f.funding_rate_floor)
            ELSE 0.0
        END AS funding_utilization
    FROM funding_rates f
    JOIN latest l
        ON f.symbol = l.symbol
        AND f.funding_rate_timestamp = l.latest_ts
)
SELECT
    symbol,
    funding_rate,
    funding_rate_timestamp,
    next_funding_time,
    funding_interval,
    funding_rate_cap,
    funding_rate_floor,
    funding_utilization
FROM joined
WHERE ABS(funding_rate) >= ?`

// LatestRates は銘柄ごとの最新 Funding Rate を条件付きで取得します。
func LatestRates(ctx context.Context, db *sql.DB, query LatestQuery) ([]funding.StoredRate, error) {
	clause, ok := sortClauses[query.Sort]
	if !ok {
		return nil, fmt.Errorf("unsupported sort: %s", query.Sort)
	}

	var sb strings.Builder
	sb.WriteString(latestSelect)
	args := []any{query.Threshold}

	switch query.Direction {
	case funding.DirectionPositive:
		sb.WriteString(" AND funding_rate > 0")
	case funding.DirectionNegative:
		sb.WriteString(" AND funding_rate < 0")
	case funding.DirectionBoth, "":
	default:
		return nil, fmt.Errorf("unsupported direction: %s", query.Direction)
	}

	if query.UtilizationGTE != nil {
		sb.WriteString(" AND funding_utilization >= ?")
		args = append(args, *query.UtilizationGTE)
	}

	sb.WriteString(" ORDER BY ")
	sb.WriteString(clause)
	sb.WriteString(" LIMIT ?")
	args = append(args, query.Limit)

	rows, err := db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query latest rates: %w", err)
	}
	defer rows.Close()

	return scanStoredRates(rows, true)
}

const historySelect = `
SELECT
    symbol,
    funding_rate,
    funding_rate_timestamp,
    next_funding_time,
    funding_interval,
    funding_rate_cap,
    funding_rate_floor
FROM funding_rates
WHERE symbol = ?
ORDER BY funding_rate_timestamp DESC
LIMIT ?`

// History は指定銘柄の履歴を新しい順に返します。
func History(ctx context.Context, db *sql.DB, symbol string, limit int) ([]funding.StoredRate, error) {
	rows, err := db.QueryContext(ctx, historySelect, symbol, limit)
	if err != nil {
		return nil, fmt.Errorf("query history: %w", err)
	}
	defer rows.Close()

	return scanStoredRates(rows, false)
}

func scanStoredRates(rows *sql.Rows, withUtilization bool) ([]funding.StoredRate, error) {
	result := make([]funding.StoredRate, 0, 64)
	for rows.Next() {
		var (
			rate        funding.StoredRate
			next        sql.NullInt64
			interval    sql.NullInt64
			cap         sql.NullFloat64
			floor       sql.NullFloat64
			utilization sql.NullFloat64
		)

		dest := []any{
			&rate.Symbol,
			&rate.Rate,
			&rate.Timestamp,
			&next,
			&interval,
			&cap,
			&floor,
		}
		if withUtilization {
			dest = append(dest, &utilization)
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}

		rate.NextFundingTime = int64Ptr(next)
		rate.IntervalHours = intPtr(interval)
		rate.Cap = float64Ptr(cap)
		rate.Floor = float64Ptr(floor)
		if withUtilization && utilization.Valid {
			rate.Utilization = float64Ptr(utilization)
		}
		result = append(result, rate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func nullInt64(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func nullInt64FromInt(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}

func nullFloat64(v *float64) sql.NullFloat64 {
	if v == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *v, Valid: true}
}

func int64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	value := v.Int64
	return &value
}

func intPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	value := int(v.Int64)
	return &value
}

func float64Ptr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	value := v.Float64
	return &value
}

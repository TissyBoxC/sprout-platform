// Package repository resolves the child profile a device session is bound to.
//
// 解析顺序体现了「绑定优先、推断兜底」：
//
//  1. 设备在 device_child_bindings 里有激活绑定，就直接用它。这是多孩家庭
//     唯一正确的答案，也是设备切换身份后的唯一来源。
//  2. 没有激活绑定时，只有在该家庭恰好有一个儿童档案的情况下才回退到它，
//     让刚配网、家长还没选过儿童的设备立刻能用。
//  3. 家庭有多个儿童且没有绑定，返回 ErrDeviceChildAmbiguous，由调用方以稳定
//     错误码拒绝同步或提示家长选择，而不是猜一个孩子。
//
// 设备上报的长期记忆必须落到具体儿童身上，否则同一家庭第二个孩子的记忆会
// 串到第一个孩子。
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrDeviceChildAmbiguous is returned when a family has more than one child
// profile and the device binding does not identify which one it serves.
var ErrDeviceChildAmbiguous = errors.New("device child binding is ambiguous")

// DeviceChildResolver maps a device session to its child profile.
type DeviceChildResolver struct {
	pool *pgxpool.Pool
}

// NewDeviceChildResolver creates the resolver.
func NewDeviceChildResolver(pool *pgxpool.Pool) *DeviceChildResolver {
	return &DeviceChildResolver{pool: pool}
}

// ResolveFamilyChild returns the child profile the device currently serves.
//
// It returns "" without an error when the family has no child profile yet, so a
// freshly provisioned device receives "not bound to a child" rather than a
// server error. The family id is used as a guard on the binding lookup: a
// binding row that points at another family is never honoured.
func (r *DeviceChildResolver) ResolveFamilyChild(
	ctx context.Context,
	familyID string,
	deviceID string,
) (string, error) {
	if r == nil || r.pool == nil {
		return "", errors.New("device child resolver database is unavailable")
	}
	familyID = strings.TrimSpace(familyID)
	if familyID == "" {
		return "", nil
	}

	if deviceID != "" {
		childID, err := r.activeBinding(ctx, familyID, deviceID)
		if err != nil {
			return "", err
		}
		if childID != "" {
			return childID, nil
		}
	}

	return r.onlyChild(ctx, familyID, deviceID)
}

// activeBinding reads the device's active child within the family.
//
// The join on child_profiles drops a binding whose child was deleted, so a
// stale row degrades to the inference path instead of pointing at nothing.
func (r *DeviceChildResolver) activeBinding(
	ctx context.Context,
	familyID string,
	deviceID string,
) (string, error) {
	var childID string
	err := r.pool.QueryRow(ctx, `
		SELECT b.child_id::text
		FROM device_child_bindings b
		JOIN child_profiles c ON c.id = b.child_id
		WHERE b.device_id = $1 AND b.family_id = $2 AND b.is_active
	`, deviceID, familyID).Scan(&childID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("resolve device child binding: %w", err)
	}
	return childID, nil
}

// onlyChild is the single-child fallback for devices with no binding yet.
func (r *DeviceChildResolver) onlyChild(
	ctx context.Context,
	familyID string,
	deviceID string,
) (string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text
		FROM child_profiles
		WHERE family_id = $1
		ORDER BY created_at ASC, id ASC
		LIMIT 2
	`, familyID)
	if err != nil {
		return "", fmt.Errorf("resolve device child profile: %w", err)
	}
	defer rows.Close()
	childIDs := make([]string, 0, 2)
	for rows.Next() {
		var childID string
		if err := rows.Scan(&childID); err != nil {
			return "", fmt.Errorf("scan device child profile: %w", err)
		}
		childIDs = append(childIDs, childID)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate device child profiles: %w", err)
	}
	switch len(childIDs) {
	case 0:
		return "", nil
	case 1:
		return childIDs[0], nil
	default:
		// The device id is deliberately not included in the error text: it
		// would end up in an operator log next to a family identifier.
		_ = deviceID
		return "", ErrDeviceChildAmbiguous
	}
}

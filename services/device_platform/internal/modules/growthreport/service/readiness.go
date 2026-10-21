package service

import (
	"context"
	"fmt"
	"time"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/growthreport/domain"
)

// stalenessDays is how many days may pass without a new report before the
// generator is considered degraded. A weekly report is produced at least once a
// week, so two weeks of silence means the worker or its queries stopped.
const stalenessDays = 14

// Readiness reports the growth-report capability health.
//
// The snapshot reads the report table rather than trusting the worker's own last
// log line: the worker can be running while its queries fail, and the console
// must not show a healthy generator that produces nothing.
func (s *Service) Readiness(
	ctx context.Context,
) (domain.Readiness, error) {
	count, children, err := s.store.CountForAdmin(ctx)
	if err != nil {
		return domain.Readiness{State: domain.ReadinessUnavailable}, nil
	}
	if count == 0 {
		return domain.Readiness{
			State:       domain.ReadinessDegraded,
			ReportCount: 0,
			ChildCount:  0,
		}, nil
	}
	lastGeneratedAt, err := s.store.LastGeneratedAt(ctx)
	if err != nil {
		return domain.Readiness{State: domain.ReadinessUnavailable}, nil
	}
	if lastGeneratedAt.IsZero() {
		return domain.Readiness{
			State:       domain.ReadinessDegraded,
			ReportCount: int64(count),
			ChildCount:  int64(children),
		}, nil
	}
	staleDays := int64(time.Since(lastGeneratedAt).Hours() / 24)
	readiness := domain.Readiness{
		State:       domain.ReadinessHealthy,
		ReportCount: int64(count),
		ChildCount:  int64(children),
		StaleDays:   staleDays,
		HasAny:      true,
	}
	if staleDays > stalenessDays {
		readiness.State = domain.ReadinessDegraded
	}
	return readiness, nil
}

// ReadinessSummary renders the snapshot for an operator.
//
// The sentence names the condition, not just a status word, so an operator can
// act without decoding a log.
func ReadinessSummary(readiness domain.Readiness) string {
	switch readiness.State {
	case domain.ReadinessUnavailable:
		return "暂时读不到成长报告"
	case domain.ReadinessDegraded:
		if !readiness.HasAny {
			return "还没有生成任何成长报告"
		}
		return fmt.Sprintf(
			"成长报告已 %d 天没有更新",
			readiness.StaleDays,
		)
	default:
		return "成长报告正常生成"
	}
}

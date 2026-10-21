package domain

import (
	"fmt"
	"strings"
)

// BuildSummary writes the guardian-facing sentence for a report.
//
// The wording follows the product's writing rules: it states what happened and
// what changed, never judges the child, never compares to other children, and
// never promises an outcome. A period with no activity says so plainly instead
// of manufacturing encouragement, because an empty week is normal and a
// fabricated cheerful line would read as insincere.
func BuildSummary(period PeriodType, metrics Metrics) string {
	var parts []string
	parts = append(parts, periodNoun(period))

	practice := metrics.Practice
	switch {
	case practice.Attempts == 0 && metrics.PreviousPractice.Attempts == 0:
		parts = append(parts, "这个周期没有英语练习记录")
	case practice.Attempts == 0:
		// Dropping to zero is worth naming once, without alarm: a break from
		// practice is ordinary and the report is not a nudge.
		parts = append(parts, fmt.Sprintf(
			"这个周期没有英语练习，上一周期练习了 %d 次",
			metrics.PreviousPractice.Attempts,
		))
	default:
		parts = append(parts, fmt.Sprintf(
			"练习了 %d 次，完成 %d 次，平均得分 %.1f",
			practice.Attempts,
			practice.Passed,
			practice.AverageScore,
		))
	}

	parts = append(parts, describeChange(metrics))

	if metrics.Usage.ActiveSeconds > 0 {
		parts = append(parts, fmt.Sprintf(
			"设备共使用 %d 分钟",
			metrics.Usage.ActiveSeconds/60,
		))
	}

	if metrics.Counts.GrowthRecords > 0 || metrics.Counts.Memories > 0 {
		parts = append(parts, fmt.Sprintf(
			"新增 %d 条成长记录，记忆了 %d 件小事",
			metrics.Counts.GrowthRecords,
			metrics.Counts.Memories,
		))
	}

	return strings.Join(parts, "，") + "。"
}

// describeChange phrases the comparison with the previous period.
//
// A change worth mentioning is named with its direction; a flat period says so
// rather than inventing movement, and a first period has nothing to compare
// with.
func describeChange(metrics Metrics) string {
	if metrics.PreviousPractice.Attempts == 0 {
		if metrics.Practice.Attempts == 0 {
			return "暂时没有可与上一周期比较的内容"
		}
		return "这是第一次有练习记录"
	}
	switch {
	case metrics.PracticeDelta > 0:
		return fmt.Sprintf(
			"比上一周期多练习 %d 次",
			metrics.PracticeDelta,
		)
	case metrics.PracticeDelta < 0:
		return fmt.Sprintf(
			"比上一周期少练习 %d 次",
			-metrics.PracticeDelta,
		)
	default:
		return "练习次数与上一周期持平"
	}
}

// periodNoun names the window in the way a guardian would say it.
func periodNoun(period PeriodType) string {
	switch period {
	case PeriodWeekly:
		return "这一周"
	case PeriodMonthly:
		return "这个月"
	default:
		return "这个周期"
	}
}

// AccuracyPhrase renders the pass rate for a console table.
//
// It is separate from the summary because an operator scanning a list wants the
// number and the sample size together; a bare percentage on two attempts would
// read as a trend it is not.
func AccuracyPhrase(metrics Metrics) string {
	practice := metrics.Practice
	if practice.Attempts == 0 {
		return "没有练习"
	}
	return fmt.Sprintf(
		"%.0f%%（%d/%d）",
		practice.Accuracy(),
		practice.Passed,
		practice.Attempts,
	)
}

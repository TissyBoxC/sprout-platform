package service

import (
	"fmt"
	"strings"

	"github.com/TissyBoxC/sprout-platform/services/device_platform/internal/modules/timeline/domain"
)

// The copy below is the only place a timeline sentence is written down.
//
// Every line states a fact the child did and stops there. It never grades the
// child, never compares them to anyone, and never repeats raw material: no
// score breakdown, no recognised speech, no audio, no lesson or package title.
// A parent reading ten cards should see the days in order, not a report card.

// assessmentEvent describes a finished assessment.
//
// The mastery percentage is a banded maturity word rather than a number. The
// exact score belongs to the assessment screen, where it is shown next to the
// answers it was computed from; on a timeline a bare percentage invites a
// comparison between siblings that the number cannot support.
func assessmentEvent(assessment Assessment) domain.Event {
	title := "完成了一次学习测评"
	description := "这次测评里"
	if assessment.Answered > 0 {
		description = fmt.Sprintf(
			"一共练习了 %d 道题，%s",
			assessment.Answered,
			masteryPhrase(assessment.Mastery),
		)
	} else {
		description = "这次测评没有留下作答记录"
	}
	return domain.NewEvent(
		domain.KindAssessmentCompleted,
		assessment.ID,
		assessment.FinishedAt,
		title,
		description,
		"",
	)
}

// masteryPhrase turns a 0-100 mastery into a plain description.
func masteryPhrase(mastery int) string {
	switch {
	case mastery >= 90:
		return "大部分都答对了"
	case mastery >= 70:
		return "多数答对了"
	case mastery >= 50:
		return "答对了一半左右"
	case mastery > 0:
		return "还在熟悉这些内容"
	default:
		return "还在练习"
	}
}

// practiceEvent describes one completed English practice.
func practiceEvent(practice Practice) domain.Event {
	subject := "英语"
	description := "跟着读了一遍并完成了练习"
	switch strings.ToLower(strings.TrimSpace(practice.ItemType)) {
	case "word":
		subject = "英语单词"
		description = "读完了一个单词，练习完成"
	case "sentence":
		subject = "英语句子"
		description = "读完了一句话，练习完成"
	}
	return domain.NewEvent(
		domain.KindEnglishPractice,
		practice.ID,
		practice.Practiced,
		"完成了一次"+subject+"练习",
		description,
		subject,
	)
}

// reportEvent describes a generated growth report.
//
// The body states that the report exists and that it can be opened; it does not
// repeat the summary sentence, which is itself child-derived text.
func reportEvent(report Report) domain.Event {
	period := "这段时间"
	title := "生成了一份成长报告"
	switch strings.ToLower(strings.TrimSpace(report.PeriodType)) {
	case "weekly":
		period = "这一周"
		title = "生成了这周的成长报告"
	case "monthly":
		period = "这个月"
		title = "生成了这个月的成长报告"
	}
	return domain.NewEvent(
		domain.KindReportPublished,
		report.ID,
		report.GeneratedAt,
		title,
		period+"的练习和小事已经整理好了，可以点开看看",
		"",
	)
}

// contentEvent describes content played to the end.
func contentEvent(play ContentPlay) domain.Event {
	return domain.NewEvent(
		domain.KindContentCompleted,
		play.ID,
		play.PlayedAt,
		"完整听完了一个内容",
		"这个内容从头听到了尾",
		"",
	)
}

// recordEvent describes a guardian-written growth record.
//
// Only a milestone is a timeline entry. An encouragement or a habit note is a
// message from the guardian to the child, and replaying it years later in a
// story of achievements would turn a private encouragement into a score. The
// guardian's own title is used, bounded, because the guardian wrote it for
// exactly this purpose.
func recordEvent(record GrowthRecord) (domain.Event, bool) {
	if !strings.EqualFold(strings.TrimSpace(record.Category), "milestone") {
		return domain.Event{}, false
	}
	title := strings.TrimSpace(record.Title)
	if title == "" {
		title = "记录了一个成长里程碑"
	}
	return domain.NewEvent(
		domain.KindMilestone,
		record.ID,
		record.RecordedAt,
		title,
		"你为孩子记下了这个瞬间",
		"",
	), true
}

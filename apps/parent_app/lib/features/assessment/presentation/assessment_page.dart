import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../../child_profile/domain/child_profile.dart';
import '../../english/domain/english.dart';
import '../application/assessment_controller.dart';
import '../domain/assessment.dart';
import 'assessment_copy.dart';

/// The guardian's learning assessment screen.
///
/// The guardian picks a lesson and the parent app scores each item, because
/// this product has no device recognition to rely on. Leaving the screen always
/// ends an open run on the platform: a run nobody closes would block the
/// family's next one, and the child would be shown questions with no adult.
class AssessmentPage extends ConsumerWidget {
  const AssessmentPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final run = ref.watch(assessmentScreenControllerProvider);
    return PopScope<Object?>(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (didPop) {
          return;
        }
        _leave(context, ref);
      },
      child: Scaffold(
        appBar: AppBar(
          title: const Text(AssessmentCopy.pageTitle),
          leading: IconButton(
            tooltip: AssessmentCopy.backButton,
            onPressed: () => _leave(context, ref),
            icon: const Icon(Icons.arrow_back_rounded),
          ),
        ),
        body: run.hasSession
            ? _RunView(run: run)
            : const _EntryView(),
      ),
    );
  }

  Future<void> _leave(BuildContext context, WidgetRef ref) async {
    final run = ref.read(assessmentScreenControllerProvider);
    final controller = ref.read(assessmentScreenControllerProvider.notifier);
    final session = run.session;
    if (session != null && session.state == AssessmentState.active) {
      // The platform's finish is what actually closes the run, so it is always
      // attempted before the screen goes away.
      await controller.finish();
    }
    controller.reset();
    if (!context.mounted) {
      return;
    }
    if (context.canPop()) {
      context.pop();
    } else {
      context.go('/me/english');
    }
  }
}

/// Holds the guardianship before /me/english/assessment is opened directly.
///
/// The internet can paste any address, so arriving here without having passed
/// through the English page must not open a run on a lesson the guardian never
/// chose.
class AssessmentGate extends ConsumerWidget {
  const AssessmentGate({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lessons = ref.watch(assessmentOfferedLessonsProvider);
    return lessons.when(
      loading: () => const Scaffold(body: _LoadingState()),
      error: (_, _) => const Scaffold(
        body: _EmptyLessonsState(key: ValueKey<String>('assessment-gate')),
      ),
      data: (value) => value.isEmpty
          ? const Scaffold(
              body: _EmptyLessonsState(
                key: ValueKey<String>('assessment-gate'),
              ),
            )
          : const AssessmentPage(),
    );
  }
}

/// Loads the entry card's lesson names before the guardian can start.
class _EntryView extends ConsumerWidget {
  const _EntryView();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lessons = ref.watch(assessmentOfferedLessonsProvider);
    final children = ref.watch(assessmentOfferedChildrenProvider);
    return AppStateSwitcher(
      stateKey: lessons.when(
        data: (value) => value.isEmpty ? 'assessment-lessons-empty' : 'assessment-lessons',
        error: (_, _) => 'assessment-lessons-error',
        loading: () => 'assessment-lessons-loading',
      ),
      child: lessons.when(
        loading: () => const _LoadingState(
          key: ValueKey<String>('assessment-lessons-loading'),
        ),
        error: (error, _) => _ErrorState(
          key: const ValueKey<String>('assessment-lessons-error'),
          message: assessmentMessageFor(error),
          actionLabel: AssessmentCopy.lessonRetry,
          onAction: () => ref.invalidate(assessmentOfferedLessonsProvider),
        ),
        data: (value) => value.isEmpty
            ? const _EmptyLessonsState(
                key: ValueKey<String>('assessment-lessons-empty'),
              )
            : _EntryCard(
                key: const ValueKey<String>('assessment-lessons'),
                lessons: value,
                children: children.value ?? const <ChildProfile>[],
              ),
      ),
    );
  }
}

class _EntryCard extends ConsumerStatefulWidget {
  const _EntryCard({
    required this.lessons,
    required this.children,
    super.key,
  });

  final List<EnglishLessonSummary> lessons;
  final List<ChildProfile> children;

  @override
  ConsumerState<_EntryCard> createState() => _EntryCardState();
}

class _EntryCardState extends ConsumerState<_EntryCard> {
  late String _lessonId = widget.lessons.first.lessonId;
  String _childId = '';

  @override
  Widget build(BuildContext context) {
    final run = ref.watch(assessmentScreenControllerProvider);
    final theme = Theme.of(context);
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
      children: <Widget>[
        AppReveal(child: _PreflightCard(theme: theme)),
        const SizedBox(height: 12),
        AppReveal(
          child: Card(
            child: Padding(
              padding: const EdgeInsets.all(20),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(
                    AssessmentCopy.lessonLabel,
                    style: theme.textTheme.titleMedium,
                  ),
                  const SizedBox(height: 10),
                  for (final lesson in widget.lessons)
                    RadioGroup<String>(
                      groupValue: _lessonId,
                      onChanged: (value) {
                        if (value == null) {
                          return;
                        }
                        setState(() => _lessonId = value);
                      },
                      child: RadioListTile<String>(
                        key: ValueKey<String>('lesson-${lesson.lessonId}'),
                        value: lesson.lessonId,
                        contentPadding: EdgeInsets.zero,
                        title: Text(lesson.title),
                        subtitle: lesson.summary.isEmpty
                            ? null
                            : Text(lesson.summary),
                      ),
                    ),
                  const SizedBox(height: 8),
                  Text(
                    AssessmentCopy.childLabel,
                    style: theme.textTheme.titleMedium,
                  ),
                  const SizedBox(height: 10),
                  if (widget.children.isEmpty)
                    Text(
                      AssessmentCopy.childEmpty,
                      style: theme.textTheme.bodySmall,
                    )
                  else
                    Wrap(
                      spacing: 8,
                      runSpacing: 8,
                      children: <Widget>[
                        ChoiceChip(
                          label: const Text(AssessmentCopy.childUnnamed),
                          selected: _childId.isEmpty,
                          onSelected: (_) => setState(() => _childId = ''),
                        ),
                        for (final child in widget.children)
                          ChoiceChip(
                            label: Text(child.nickname),
                            selected: _childId == child.childId,
                            onSelected: (_) =>
                                setState(() => _childId = child.childId),
                          ),
                      ],
                    ),
                ],
              ),
            ),
          ),
        ),
        // The refusal sits beside the action that caused it, so it is read
        // before the button is pressed again.
        if (run.error != null) ...<Widget>[
          const SizedBox(height: 12),
          _RefusalNotice(message: assessmentMessageFor(run.error)),
        ],
        const SizedBox(height: 16),
        FilledButton(
          key: const ValueKey<String>('assessment-start'),
          onPressed: run.isLoading
              ? null
              : () => _start(context, ref, _lessonId, _childId),
          child: Text(
            run.isLoading
                ? AssessmentCopy.startingButton
                : AssessmentCopy.startButton,
          ),
        ),
      ],
    );
  }

  Future<void> _start(
    BuildContext context,
    WidgetRef ref,
    String lessonId,
    String childId,
  ) async {
    final confirmed = await confirmAssessmentStart(context);
    if (!confirmed || !context.mounted) {
      return;
    }
    try {
      await ref
          .read(assessmentScreenControllerProvider.notifier)
          .start(lessonId: lessonId, childId: childId);
    } on AssessmentRefused catch (refusal) {
      // Another screen may still hold a run open for this family, so the entry
      // card offers the one way out instead of repeating the refusal.
      if (refusal.refusal == AssessmentRefusal.alreadyActive) {
        await _resumeOpenRun(ref);
      }
    } on Object {
      // The entry card already renders the stored refusal, so the reason is
      // shown once and stays readable instead of vanishing in a snack bar.
    }
  }

  /// Opens the run the family already has, so the refusal is actionable.
  Future<void> _resumeOpenRun(WidgetRef ref) async {
    final recent = await ref.read(assessmentHistoryProvider.future);
    for (final session in recent) {
      if (session.isActive) {
        await ref
            .read(assessmentScreenControllerProvider.notifier)
            .resume(session);
        return;
      }
    }
  }
}

/// Asks the guardian to accept the bounds of a run before it is sent.
///
/// Changing the number of items or the time limit is a product promise, so the
/// dialog repeats the same numbers the entry card states.
Future<bool> confirmAssessmentStart(BuildContext context) async {
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      icon: const Icon(Icons.quiz_outlined),
      title: const Text(AssessmentCopy.preflightConfirmTitle),
      content: const Text(AssessmentCopy.preflightConfirmBody),
      actions: <Widget>[
        TextButton(
          onPressed: () => Navigator.of(dialogContext).pop(false),
          child: const Text(AssessmentCopy.cancelStart),
        ),
        FilledButton(
          key: const ValueKey<String>('assessment-confirm-start'),
          onPressed: () => Navigator.of(dialogContext).pop(true),
          child: const Text(AssessmentCopy.confirmStart),
        ),
      ],
    ),
  );
  return confirmed ?? false;
}

class _PreflightCard extends StatelessWidget {
  const _PreflightCard({required this.theme});

  final ThemeData theme;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Row(
              children: <Widget>[
                Icon(
                  Icons.psychology_alt_outlined,
                  size: 20,
                  color: theme.colorScheme.primary,
                ),
                const SizedBox(width: 8),
                Text(
                  AssessmentCopy.entryTitle,
                  style: theme.textTheme.titleMedium,
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(AssessmentCopy.entryBody),
            const SizedBox(height: 12),
            Text(
              AssessmentCopy.preflightTitle,
              style: theme.textTheme.titleSmall,
            ),
            const SizedBox(height: 8),
            const _Bullet(AssessmentCopy.preflightOne),
            const _Bullet(AssessmentCopy.preflightTwo),
            const _Bullet(AssessmentCopy.preflightThree),
            const _Bullet(AssessmentCopy.preflightFour),
          ],
        ),
      ),
    );
  }
}

class _Bullet extends StatelessWidget {
  const _Bullet(this.text);

  final String text;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          const Text('· '),
          Expanded(child: Text(text)),
        ],
      ),
    );
  }
}

/// The run itself: questions with scoring, or the result once it is closed.
class _RunView extends StatelessWidget {
  const _RunView({required this.run});

  final AssessmentRunState run;

  @override
  Widget build(BuildContext context) {
    final session = run.session!;
    if (session.state != AssessmentState.active) {
      return _ResultView(run: run, session: session);
    }
    return _ActiveRunView(run: run, session: session);
  }
}

class _ActiveRunView extends ConsumerStatefulWidget {
  const _ActiveRunView({required this.run, required this.session});

  final AssessmentRunState run;
  final AssessmentSession session;

  @override
  ConsumerState<_ActiveRunView> createState() => _ActiveRunViewState();
}

class _ActiveRunViewState extends ConsumerState<_ActiveRunView> {
  double _score = 100;
  bool _isPartialOpen = false;

  @override
  Widget build(BuildContext context) {
    final run = widget.run;
    final session = widget.session;
    final item = run.item;
    final isOutOfTime = run.remainingSeconds <= 0;
    return Column(
      children: <Widget>[
        Expanded(
          child: ListView(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
            children: <Widget>[
              // A failure caused by the item is read above the item, never
              // below a card the guardian has already scrolled past.
              if (run.error != null) ...<Widget>[
                _RefusalNotice(message: assessmentMessageFor(run.error)),
                const SizedBox(height: 12),
              ],
              AppReveal(child: _ProgressCard(run: run, session: session)),
              const SizedBox(height: 12),
              AppReveal(
                child: _QuestionCard(
                  run: run,
                  session: session,
                  item: item,
                  isOutOfTime: isOutOfTime,
                  score: _score,
                  isPartialOpen: _isPartialOpen,
                  onTogglePartial: () =>
                      setState(() => _isPartialOpen = !_isPartialOpen),
                  onScoreChanged: (value) => setState(() => _score = value),
                  onScore: (score) => _submit(context, ref, score),
                ),
              ),
              if (run.notice != null) ...<Widget>[
                const SizedBox(height: 12),
                _Notice(message: run.notice!),
              ],
            ],
          ),
        ),
        // The way out stays reachable without scrolling past the question,
        // because a child who wants to stop should not have to hunt for it.
        SafeArea(
          top: false,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 12),
            child: OutlinedButton(
              key: const ValueKey<String>('assessment-finish'),
              onPressed: run.isFinishing
                  ? null
                  : () => _confirmFinish(context, ref),
              child: Text(
                run.isFinishing
                    ? AssessmentCopy.finishingButton
                    : AssessmentCopy.finishButton,
              ),
            ),
          ),
        ),
      ],
    );
  }

  Future<void> _submit(BuildContext context, WidgetRef ref, int score) async {
    await ref
        .read(assessmentScreenControllerProvider.notifier)
        .submitScore(score);
  }

  Future<void> _confirmFinish(BuildContext context, WidgetRef ref) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        icon: const Icon(Icons.stop_circle_outlined),
        title: const Text(AssessmentCopy.finishConfirmTitle),
        content: const Text(AssessmentCopy.finishConfirmBody),
        actions: <Widget>[
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text(AssessmentCopy.finishKeepGoing),
          ),
          FilledButton(
            key: const ValueKey<String>('assessment-confirm-finish'),
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text(AssessmentCopy.finishConfirm),
          ),
        ],
      ),
    );
    if (confirmed != true || !context.mounted) {
      return;
    }
    await ref.read(assessmentScreenControllerProvider.notifier).finish();
  }
}

class _ResultView extends ConsumerWidget {
  const _ResultView({required this.run, required this.session});

  final AssessmentRunState run;
  final AssessmentSession session;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final history = ref.watch(assessmentHistoryProvider);
    final startDifficulty = run.startDifficulty == 0
        ? session.difficulty
        : run.startDifficulty;
    final masteryVisible = session.answeredCount > 0;
    return RefreshIndicator(
      onRefresh: () async {
        ref.invalidate(assessmentHistoryProvider);
        await ref.read(assessmentHistoryProvider.future);
      },
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
        children: <Widget>[
          AppReveal(
            child: Card(
              child: Padding(
                padding: const EdgeInsets.all(20),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Row(
                      children: <Widget>[
                        Icon(
                          Icons.emoji_events_outlined,
                          size: 20,
                          color: theme.colorScheme.primary,
                        ),
                        const SizedBox(width: 8),
                        Text(
                          AssessmentCopy.resultTitle,
                          style: theme.textTheme.titleMedium,
                        ),
                      ],
                    ),
                    const SizedBox(height: 10),
                    Text(AssessmentCopy.resultReason(session)),
                    const SizedBox(height: 16),
                    if (masteryVisible) ...<Widget>[
                      Text(
                        '${AssessmentCopy.resultMasteryLabel}：${session.mastery}%',
                        key: const ValueKey<String>('assessment-result-mastery'),
                        style: theme.textTheme.headlineSmall?.copyWith(
                          color: theme.colorScheme.primary,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                      const SizedBox(height: 8),
                      ClipRRect(
                        borderRadius: BorderRadius.circular(16),
                        child: LinearProgressIndicator(
                          value: (session.mastery / 100).clamp(0, 1),
                          minHeight: 8,
                        ),
                      ),
                      const SizedBox(height: 12),
                    ] else
                      Text(
                        AssessmentCopy.resultNoAnswers,
                        key: const ValueKey<String>(
                          'assessment-result-no-answers',
                        ),
                      ),
                    _Metric(
                      label: AssessmentCopy.resultCorrectLabel,
                      value:
                          '${session.correctCount} / ${session.answeredCount}',
                    ),
                    const SizedBox(height: 6),
                    _Metric(
                      label: AssessmentCopy.resultDifficultyLabel,
                      value: AssessmentCopy.difficultyRange(
                        startDifficulty,
                        session.difficulty,
                      ),
                    ),
                    const SizedBox(height: 16),
                    Text(
                      AssessmentCopy.tip(session),
                      key: const ValueKey<String>('assessment-result-tip'),
                    ),
                  ],
                ),
              ),
            ),
          ),
          const SizedBox(height: 16),
          FilledButton(
            key: const ValueKey<String>('assessment-next-lesson'),
            onPressed: () =>
                ref.read(assessmentScreenControllerProvider.notifier).reset(),
            child: const Text(AssessmentCopy.nextLessonButton),
          ),
          const SizedBox(height: 24),
          Text(
            AssessmentCopy.historyTitle,
            style: theme.textTheme.titleMedium,
          ),
          const SizedBox(height: 8),
          _HistoryList(history: history),
        ],
      ),
    );
  }
}

class _HistoryList extends ConsumerWidget {
  const _HistoryList({required this.history});

  final AsyncValue<List<AssessmentSession>> history;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return history.when(
      loading: () => const Padding(
        padding: EdgeInsets.symmetric(vertical: 20),
        child: Center(child: CircularProgressIndicator()),
      ),
      error: (error, _) => _InlineNotice(
        key: const ValueKey<String>('assessment-history-error'),
        message: AssessmentCopy.historyFailed,
        actionLabel: AssessmentCopy.retryButton,
        onAction: () => ref.invalidate(assessmentHistoryProvider),
      ),
      data: (sessions) {
        if (sessions.isEmpty) {
          return const _InlineNotice(
            key: ValueKey<String>('assessment-history-empty'),
            message: AssessmentCopy.historyEmpty,
            title: AssessmentCopy.historyEmptyTitle,
          );
        }
        return Column(
          key: const ValueKey<String>('assessment-history'),
          children: <Widget>[
            for (final session in sessions)
              _HistoryTile(
                key: ValueKey<String>('history-${session.sessionId}'),
                session: session,
              ),
          ],
        );
      },
    );
  }
}

class _HistoryTile extends StatelessWidget {
  const _HistoryTile({required this.session, super.key});

  final AssessmentSession session;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      child: ListTile(
        leading: Icon(
          session.state == AssessmentState.completed
              ? Icons.check_circle_outline_rounded
              : session.state == AssessmentState.active
              ? Icons.timelapse_rounded
              : Icons.cloud_off_outlined,
          color: theme.colorScheme.primary,
        ),
        title: Text(
          '${AssessmentCopy.historyStatus(session)}'
          ' · ${session.mastery}%',
        ),
        subtitle: Text(
          '${AssessmentCopy.formatDateTime(session.startedAt)}'
          ' · ${AssessmentCopy.historyDetail(session)}',
        ),
      ),
    );
  }
}

/// Question number, level, correct count and the time left.
///
/// The numbers are what a guardian checks mid-run, so they sit above the
/// question rather than beside it on a phone-sized screen.
class _ProgressCard extends StatelessWidget {
  const _ProgressCard({required this.run, required this.session});

  final AssessmentRunState run;
  final AssessmentSession session;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Row(
              children: <Widget>[
                Icon(
                  Icons.timelapse_rounded,
                  size: 20,
                  color: theme.colorScheme.primary,
                ),
                const SizedBox(width: 8),
                Text(
                  AssessmentCopy.progressTitle,
                  style: theme.textTheme.titleMedium,
                ),
                const Spacer(),
                Text(
                  AssessmentCopy.remaining(run.remainingSeconds),
                  key: const ValueKey<String>('assessment-remaining'),
                  style: theme.textTheme.bodySmall,
                ),
              ],
            ),
            const SizedBox(height: 12),
            ClipRRect(
              borderRadius: BorderRadius.circular(16),
              child: LinearProgressIndicator(
                value: session.maxItems <= 0
                    ? 0
                    : (session.itemIndex / session.maxItems).clamp(0, 1),
                minHeight: 6,
              ),
            ),
            const SizedBox(height: 12),
            Wrap(
              spacing: 24,
              runSpacing: 8,
              children: <Widget>[
                _Metric(
                  label: AssessmentCopy.itemProgress,
                  value: AssessmentCopy.itemCounter(
                    session.itemIndex,
                    session.maxItems,
                  ),
                ),
                _Metric(
                  label: AssessmentCopy.difficultyLabel,
                  value: AssessmentCopy.difficultyName(session.difficulty),
                ),
                _Metric(
                  label: AssessmentCopy.correctLabel,
                  value: '${session.correctCount} / ${session.answeredCount}',
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

/// The item to read out and the ways a guardian can score it: right, wrong, or
/// a middle score for an answer that was close.
class _QuestionCard extends StatelessWidget {
  const _QuestionCard({
    required this.run,
    required this.session,
    required this.item,
    required this.isOutOfTime,
    required this.score,
    required this.isPartialOpen,
    required this.onTogglePartial,
    required this.onScoreChanged,
    required this.onScore,
  });

  final AssessmentRunState run;
  final AssessmentSession session;
  final AssessmentItem? item;
  final bool isOutOfTime;
  final double score;
  final bool isPartialOpen;
  final VoidCallback onTogglePartial;
  final ValueChanged<double> onScoreChanged;
  final ValueChanged<int> onScore;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final item = this.item;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(
              AssessmentCopy.itemPosition(session.itemIndex),
              style: theme.textTheme.titleSmall,
            ),
            const SizedBox(height: 6),
            Text(AssessmentCopy.readingHint, style: theme.textTheme.bodySmall),
            const SizedBox(height: 12),
            if (isOutOfTime)
              Text(AssessmentCopy.resultEndedByTime)
            else if (item == null || item.isEmpty)
              Text(AssessmentCopy.waitingForItem)
            else ...<Widget>[
              Text(
                AssessmentCopy.itemType(item),
                style: theme.textTheme.bodySmall,
              ),
              const SizedBox(height: 4),
              Text(
                item.text,
                key: const ValueKey<String>('assessment-item-text'),
                style: theme.textTheme.headlineSmall?.copyWith(
                  color: theme.colorScheme.onSurface,
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(height: 16),
              Row(
                children: <Widget>[
                  Expanded(
                    child: FilledButton(
                      key: const ValueKey<String>('assessment-correct'),
                      onPressed: run.isSubmitting ? null : () => onScore(100),
                      child: const Text(AssessmentCopy.correctButton),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: OutlinedButton(
                      key: const ValueKey<String>('assessment-wrong'),
                      onPressed: run.isSubmitting ? null : () => onScore(0),
                      child: const Text(AssessmentCopy.wrongButton),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 8),
              Text(
                AssessmentCopy.scoringHint,
                style: theme.textTheme.bodySmall,
              ),
              // A middle score is the exception, so it stays folded away: two
              // buttons cover almost every answer and record it in one tap.
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton(
                  key: const ValueKey<String>('assessment-partial-toggle'),
                  onPressed: run.isSubmitting ? null : onTogglePartial,
                  child: Text(
                    isPartialOpen
                        ? AssessmentCopy.partialScoreHide
                        : AssessmentCopy.partialScoreToggle,
                  ),
                ),
              ),
              if (isPartialOpen) ...[
                Text(
                  AssessmentCopy.partialScoreHint,
                  style: theme.textTheme.bodySmall,
                ),
                Row(
                  children: <Widget>[
                    Expanded(
                      child: Slider(
                        key: const ValueKey<String>('assessment-score-slider'),
                        value: score,
                        max: 100,
                        divisions: 20,
                        label: score.round().toString(),
                        onChanged: run.isSubmitting ? null : onScoreChanged,
                      ),
                    ),
                    Text(
                      '${AssessmentCopy.scorePreviewPrefix} ${score.round()} 分',
                    ),
                  ],
                ),
                const SizedBox(height: 4),
                OutlinedButton(
                  key: const ValueKey<String>('assessment-submit-score'),
                  onPressed: run.isSubmitting
                      ? null
                      : () => onScore(score.round()),
                  child: Text(
                    run.isSubmitting
                        ? AssessmentCopy.submittingAnswerButton
                        : AssessmentCopy.submitAnswerButton,
                  ),
                ),
              ],
            ],
          ],
        ),
      ),
    );
  }
}

class _Metric extends StatelessWidget {
  const _Metric({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text(label, style: theme.textTheme.bodySmall),
        const SizedBox(height: 2),
        Text(value, style: theme.textTheme.titleSmall),
      ],
    );
  }
}

class _Notice extends StatelessWidget {
  const _Notice({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: const Color(0xFFF2FBF4),
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: const Color(0xFFBEE3C8)),
      ),
      child: Row(
        children: <Widget>[
          const Icon(Icons.check_circle_outline_rounded, size: 18),
          const SizedBox(width: 8),
          Expanded(
            child: Text(message, style: theme.textTheme.bodyMedium),
          ),
        ],
      ),
    );
  }
}

class _RefusalNotice extends StatelessWidget {
  const _RefusalNotice({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: const Color(0xFFFFF2F5),
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: const Color(0xFFF0BDCB)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Icon(
            Icons.info_outline_rounded,
            size: 18,
            color: theme.colorScheme.primary,
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              message,
              key: const ValueKey<String>('assessment-refusal'),
              style: theme.textTheme.bodyMedium,
            ),
          ),
        ],
      ),
    );
  }
}

class _InlineNotice extends StatelessWidget {
  const _InlineNotice({
    required this.message,
    this.title,
    this.actionLabel,
    this.onAction,
    super.key,
  });

  final String message;
  final String? title;
  final String? actionLabel;
  final VoidCallback? onAction;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Row(
              children: <Widget>[
                Icon(
                  Icons.inbox_outlined,
                  size: 20,
                  color: theme.colorScheme.primary,
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    title ?? AssessmentCopy.historyTitle,
                    style: theme.textTheme.titleSmall,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Text(message),
            if (actionLabel != null && onAction != null) ...<Widget>[
              const SizedBox(height: 12),
              OutlinedButton(
                onPressed: onAction,
                child: Text(actionLabel!),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _LoadingState extends StatelessWidget {
  const _LoadingState({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          const SizedBox(
            width: 28,
            height: 28,
            child: CircularProgressIndicator(strokeWidth: 3),
          ),
          const SizedBox(height: 12),
          Text(AssessmentCopy.loading, style: theme.textTheme.bodyMedium),
        ],
      ),
    );
  }
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({
    required this.message,
    required this.actionLabel,
    required this.onAction,
    super.key,
  });

  final String message;
  final String actionLabel;
  final VoidCallback onAction;

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
      children: <Widget>[
        _InlineNotice(
          message: message,
          title: AssessmentCopy.lessonEmptyTitle,
          actionLabel: actionLabel,
          onAction: onAction,
        ),
      ],
    );
  }
}

class _EmptyLessonsState extends StatelessWidget {
  const _EmptyLessonsState({super.key});

  @override
  Widget build(BuildContext context) {
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
      children: const <Widget>[
        _InlineNotice(
          message: AssessmentCopy.lessonEmpty,
          title: AssessmentCopy.lessonEmptyTitle,
        ),
      ],
    );
  }
}

/// The entry a guardian taps from the English learning page.
class AssessmentEntryCard extends StatelessWidget {
  const AssessmentEntryCard({super.key, this.onOpen});

  final VoidCallback? onOpen;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Row(
              children: <Widget>[
                Icon(
                  Icons.psychology_alt_outlined,
                  color: theme.colorScheme.primary,
                ),
                const SizedBox(width: 10),
                Text(
                  AssessmentCopy.entryTitle,
                  style: theme.textTheme.titleMedium,
                ),
              ],
            ),
            const SizedBox(height: 8),
            const Text(AssessmentCopy.entryBody),
            const SizedBox(height: 12),
            FilledButton(
              key: const ValueKey<String>('assessment-open'),
              onPressed:
                  onOpen ??
                  () => Navigator.of(context).push<void>(
                    MaterialPageRoute<void>(
                      builder: (context) => const AssessmentPage(),
                    ),
                  ),
              child: const Text(AssessmentCopy.entryAction),
            ),
          ],
        ),
      ),
    );
  }
}

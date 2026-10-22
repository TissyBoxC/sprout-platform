import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/time/current_time.dart';
import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../../child_profile/domain/child_profile.dart';
import '../application/timeline_controller.dart';
import '../domain/timeline.dart';
import 'timeline_copy.dart';

/// The guardian's 成长时间轴 screen.
///
/// It shows what a child already finished, oldest first, plus the counters built
/// from the same records. It never shows a score breakdown, a transcript, or an
/// audio reference, because the platform's events carry none: each card is one
/// sentence and a time.
class TimelinePageView extends ConsumerWidget {
  const TimelinePageView({super.key, this.isVisible = true});

  /// Whether the screen is on top. A hidden screen passes false so the
  /// notifier does not hold a read open behind another page.
  final bool isVisible;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // A page without focus reports itself so the notifier stops holding a read
    // open; the real values come back when the page becomes visible again.
    ref.listen(timelineBindingProvider, (_, _) {});
    final children = ref.watch(timelineChildrenProvider);
    final state = ref.watch(timelineModelProvider);

    return Scaffold(
      appBar: AppBar(
        title: const Text(TimelineCopy.pageTitle),
        leading: IconButton(
          tooltip: TimelineCopy.backButton,
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: children.when(
        loading: () => const _LoadingState(),
        error: (error, _) => _ErrorState(
          message: timelineMessageFor(error),
          onRetry: () => ref.invalidate(timelineChildrenProvider),
        ),
        data: (profiles) => profiles.isEmpty
            ? const _EmptyState(
                key: ValueKey<String>('timeline-children-empty'),
                title: TimelineCopy.childEmptyTitle,
                body: TimelineCopy.childEmptyBody,
              )
            : _content(context, ref, state, profiles),
      ),
    );
  }

  Widget _content(
    BuildContext context,
    WidgetRef ref,
    TimelineState state,
    List<ChildProfile> profiles,
  ) {
    final hasChild = state.childId.isNotEmpty;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 8),
          child: _ChildSelector(
            children: profiles,
            selectedId: state.childId,
            onSelected: (index) => ref
                .read(timelineSelectedChildIndexProvider.notifier)
                .select(index),
          ),
        ),
        Expanded(
          child: AppStateSwitcher(
            stateKey: _stateKey(state, hasChild),
            child: _body(context, ref, state, hasChild),
          ),
        ),
      ],
    );
  }

  String _stateKey(TimelineState state, bool hasChild) {
    if (!hasChild) {
      return 'timeline-no-child';
    }
    if (state.error != null) {
      return 'timeline-error';
    }
    if (state.isLoading) {
      return 'timeline-loading';
    }
    return state.isEmpty ? 'timeline-empty' : 'timeline-list';
  }

  Widget _body(
    BuildContext context,
    WidgetRef ref,
    TimelineState state,
    bool hasChild,
  ) {
    if (!hasChild) {
      return const _EmptyState(
        key: ValueKey<String>('timeline-no-child'),
        title: TimelineCopy.childSelectionHint,
        body: TimelineCopy.childTimelineHint,
      );
    }
    if (state.error != null) {
      return _ErrorState(
        key: const ValueKey<String>('timeline-error'),
        message: timelineMessageFor(state.error),
        onRetry: () => ref.read(timelineControllerProvider.notifier).refresh(),
      );
    }
    if (state.isLoading) {
      return const _LoadingState();
    }
    if (state.isEmpty) {
      return const _EmptyState(
        key: ValueKey<String>('timeline-empty'),
        title: TimelineCopy.emptyTitle,
        body: TimelineCopy.emptyBody,
      );
    }
    return _TimelineList(state: state);
  }

  void _leave(BuildContext context) {
    if (context.canPop()) {
      context.pop();
      return;
    }
    context.go('/me');
  }
}

/// The milestones card and the list of events.
class _TimelineList extends ConsumerWidget {
  const _TimelineList({required this.state});

  final TimelineState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final now = ref.watch(currentTimeProvider)();
    final milestones = state.milestones;
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 4, 16, 32),
      children: <Widget>[
        AppReveal(child: _MilestonesCard(milestones: milestones)),
        const SizedBox(height: 16),
        Padding(
          padding: const EdgeInsets.only(left: 4, bottom: 8),
          child: Text(
            TimelineCopy.listTitle,
            style: Theme.of(context).textTheme.titleSmall,
          ),
        ),
        for (final event in state.events)
          Padding(
            padding: const EdgeInsets.only(bottom: 10),
            child: AppReveal(
              child: _EventCard(
                key: ValueKey<String>('timeline-event-${event.eventId}'),
                event: event,
                now: now,
              ),
            ),
          ),
        _Footer(state: state),
      ],
    );
  }
}

/// The seven counters of finished work.
class _MilestonesCard extends StatelessWidget {
  const _MilestonesCard({required this.milestones});

  final TimelineMilestones? milestones;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final value = milestones;
    return Card(
      key: const ValueKey<String>('timeline-milestones'),
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(
              TimelineCopy.milestonesTitle,
              style: theme.textTheme.titleMedium,
            ),
            const SizedBox(height: 14),
            if (value == null)
              Text(
                TimelineCopy.milestonesFailed,
                style: theme.textTheme.bodySmall,
              )
            else if (value.isEmpty)
              Text(
                TimelineCopy.milestonesEmpty,
                style: theme.textTheme.bodySmall,
              )
            else
              Wrap(
                spacing: 10,
                runSpacing: 10,
                children: <Widget>[
                  for (final metric in MilestoneMetric.values)
                    _MilestoneTile(metric: metric, milestones: value),
                ],
              ),
          ],
        ),
      ),
    );
  }
}

/// One counter, in the same tile shape the rest of the app uses.
class _MilestoneTile extends StatelessWidget {
  const _MilestoneTile({required this.metric, required this.milestones});

  final MilestoneMetric metric;
  final TimelineMilestones milestones;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: 96,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: theme.colorScheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text(
            TimelineCopy.milestoneLabel(metric),
            style: theme.textTheme.bodySmall,
          ),
          const SizedBox(height: 4),
          FittedBox(
            fit: BoxFit.scaleDown,
            alignment: Alignment.centerLeft,
            child: Text.rich(
              TextSpan(
                text: '${milestoneValueOf(milestones, metric)}',
                style: theme.textTheme.titleLarge?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
                children: <InlineSpan>[
                  TextSpan(
                    text: ' ${TimelineCopy.milestoneUnit(metric)}',
                    style: theme.textTheme.bodySmall,
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// One entry: a kind marker, the title, the sentence, and the time.
class _EventCard extends StatelessWidget {
  const _EventCard({required this.event, required this.now, super.key});

  final TimelineEvent event;
  final DateTime now;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final marker = _markerOf(event.kind);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Container(
              width: 40,
              height: 40,
              decoration: BoxDecoration(
                color: marker.background,
                borderRadius: BorderRadius.circular(14),
              ),
              child: Icon(marker.icon, size: 20, color: marker.foreground),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Row(
                    children: <Widget>[
                      Expanded(
                        child: Text(
                          event.title,
                          style: theme.textTheme.titleSmall,
                        ),
                      ),
                      const SizedBox(width: 8),
                      Text(
                        TimelineCopy.relativeTime(event.occurredAt, now),
                        style: theme.textTheme.bodySmall,
                      ),
                    ],
                  ),
                  const SizedBox(height: 4),
                  Text(event.description, style: theme.textTheme.bodyMedium),
                  const SizedBox(height: 6),
                  Text(
                    TimelineCopy.kindName(event.kind),
                    style: theme.textTheme.labelSmall?.copyWith(
                      color: marker.foreground,
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// The icon and the two colours of one kind.
///
/// The colours stay inside the brand palette and one accent each, so the list
/// can be scanned by kind without turning every card into decoration.
_KindMarker _markerOf(TimelineEventKind kind) {
  return switch (kind) {
    TimelineEventKind.assessmentCompleted => const _KindMarker(
      icon: Icons.fact_check_outlined,
      background: Color(0xFFFFF2F5),
      foreground: Color(0xFFD94F83),
    ),
    TimelineEventKind.englishPractice => const _KindMarker(
      icon: Icons.record_voice_over_outlined,
      background: Color(0xFFEAF8FF),
      foreground: Color(0xFF2A7FA8),
    ),
    TimelineEventKind.reportPublished => const _KindMarker(
      icon: Icons.insights_outlined,
      background: Color(0xFFFFF8DE),
      foreground: Color(0xFF8A6A12),
    ),
    TimelineEventKind.milestone => const _KindMarker(
      icon: Icons.auto_awesome_outlined,
      background: Color(0xFFFFF2F5),
      foreground: Color(0xFFF7A8BF),
    ),
    TimelineEventKind.contentCompleted => const _KindMarker(
      icon: Icons.headphones_outlined,
      background: Color(0xFFF3F1FF),
      foreground: Color(0xFF5B4B9E),
    ),
    TimelineEventKind.other => const _KindMarker(
      icon: Icons.circle_outlined,
      background: Color(0xFFF7F4F5),
      foreground: Color(0xFF6B4F5A),
    ),
  };
}

class _KindMarker {
  const _KindMarker({
    required this.icon,
    required this.background,
    required this.foreground,
  });

  final IconData icon;
  final Color background;
  final Color foreground;
}

/// The end of the list, or the button that continues it.
class _Footer extends ConsumerWidget {
  const _Footer({required this.state});

  final TimelineState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final error = state.loadMoreError;
    if (error != null) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Text(
            timelineMessageFor(error),
            key: const ValueKey<String>('timeline-load-more-error'),
            textAlign: TextAlign.center,
            style: theme.textTheme.bodySmall,
          ),
          const SizedBox(height: 8),
          OutlinedButton(
            key: const ValueKey<String>('timeline-load-more-retry'),
            onPressed: () =>
                ref.read(timelineControllerProvider.notifier).loadMore(),
            child: const Text(TimelineCopy.loadMoreButton),
          ),
        ],
      );
    }
    if (state.isLoadingMore) {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 16),
        child: Column(
          children: <Widget>[
            const SizedBox(
              width: 20,
              height: 20,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
            const SizedBox(height: 8),
            Text(TimelineCopy.loadingMore, style: theme.textTheme.bodySmall),
          ],
        ),
      );
    }
    if (!state.hasMore) {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 16),
        child: Text(
          TimelineCopy.reachedEnd,
          key: const ValueKey<String>('timeline-reached-end'),
          textAlign: TextAlign.center,
          style: theme.textTheme.bodySmall,
        ),
      );
    }
    // An empty cursor ends the list for good; the button only exists while the
    // platform says there is an older page to read.
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 12),
      child: OutlinedButton(
        key: const ValueKey<String>('timeline-load-more'),
        onPressed: () =>
            ref.read(timelineControllerProvider.notifier).loadMore(),
        child: const Text(TimelineCopy.loadMoreButton),
      ),
    );
  }
}

/// The list of children, using the same chips as the device child panel.
class _ChildSelector extends StatelessWidget {
  const _ChildSelector({
    required this.children,
    required this.selectedId,
    required this.onSelected,
  });

  final List<ChildProfile> children;
  final String selectedId;

  /// The position picked in [children].
  final ValueChanged<int> onSelected;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text(
          TimelineCopy.childTitle,
          style: Theme.of(context).textTheme.bodySmall,
        ),
        const SizedBox(height: 6),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: <Widget>[
            for (final (index, child) in children.indexed)
              ChoiceChip(
                key: ValueKey<String>('timeline-child-${child.childId}'),
                label: Text(child.nickname),
                selected: selectedId == child.childId,
                onSelected: (_) => onSelected(index),
              ),
          ],
        ),
      ],
    );
  }
}

class _LoadingState extends StatelessWidget {
  const _LoadingState();

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            const SizedBox(
              width: 28,
              height: 28,
              child: CircularProgressIndicator(strokeWidth: 2.5),
            ),
            const SizedBox(height: 12),
            Text(
              TimelineCopy.loading,
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ),
      ),
    );
  }
}

class _EmptyState extends StatelessWidget {
  const _EmptyState({required this.title, required this.body, super.key});

  final String title;
  final String body;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Center(
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 28, vertical: 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            const Icon(
              Icons.auto_awesome_outlined,
              size: 28,
              color: Color(0xFFF7A8BF),
            ),
            const SizedBox(height: 10),
            Text(title, style: theme.textTheme.titleMedium),
            const SizedBox(height: 6),
            Text(body, textAlign: TextAlign.center),
          ],
        ),
      ),
    );
  }
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({required this.message, required this.onRetry, super.key});

  final String message;
  final VoidCallback? onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Text(message, textAlign: TextAlign.center),
            if (onRetry != null) ...<Widget>[
              const SizedBox(height: 16),
              FilledButton(
                onPressed: onRetry,
                child: const Text(TimelineCopy.retryButton),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

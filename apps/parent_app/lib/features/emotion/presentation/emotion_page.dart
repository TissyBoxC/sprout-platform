import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../../child_profile/domain/child_profile.dart';
import '../application/emotion_controller.dart';
import '../domain/emotion.dart';
import 'emotion_copy.dart';

/// The guardian's 情绪感知 screen.
///
/// It shows a distribution over a period and nothing else: the platform has no
/// route that returns a single observation, and this screen has no place that
/// could show one. Sensing is off until a guardian turns it on, and every state
/// explains what is happening rather than what failed.
class EmotionPage extends ConsumerWidget {
  const EmotionPage({super.key, this.isVisible = true});

  /// Whether the screen is on top. A hidden screen passes false so the
  /// notifier does not hold a read open behind another page.
  final bool isVisible;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // A page without focus reports itself so the notifier stops holding a read
    // open; the real values come back when the page becomes visible again.
    ref.listen(emotionBindingProvider, (_, _) {});
    final children = ref.watch(emotionChildrenProvider);
    final state = ref.watch(emotionModelProvider);

    return Scaffold(
      appBar: AppBar(
        title: const Text(EmotionCopy.pageTitle),
        leading: IconButton(
          tooltip: EmotionCopy.backButton,
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: children.when(
        loading: () => const _LoadingState(),
        error: (error, _) => _ErrorState(
          message: emotionMessageFor(error),
          onRetry: () => ref.invalidate(emotionChildrenProvider),
        ),
        data: (profiles) => _content(context, ref, state, profiles),
      ),
    );
  }

  Widget _content(
    BuildContext context,
    WidgetRef ref,
    EmotionState state,
    List<ChildProfile> profiles,
  ) {
    // A failure with no settings behind it means the first read never finished,
    // so there is no screen to put a sentence beside; a failure after settings
    // arrived is shown next to the control that caused it.
    if (state.error != null && state.settings == null) {
      return AppStateSwitcher(
        stateKey: 'emotion-error',
        child: _ErrorState(
          key: const ValueKey<String>('emotion-error'),
          message: emotionMessageFor(state.error),
          onRetry: () => ref.read(emotionControllerProvider.notifier).refresh(),
        ),
      );
    }
    if (state.isLoading && state.settings == null) {
      return const _LoadingState();
    }
    if (profiles.isEmpty) {
      return const _EmptyState(
        key: ValueKey<String>('emotion-children-empty'),
        title: EmotionCopy.childEmptyTitle,
        body: EmotionCopy.childEmptyBody,
      );
    }
    final selectedId = ref.watch(emotionSelectedChildIdProvider);
    final selectedChild = profiles
        .where((child) => child.childId == selectedId)
        .firstOrNull;
    return ListView(
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
      children: <Widget>[
        AppReveal(
          child: _ChildSelector(
            children: profiles,
            selectedId: selectedChild?.childId ?? '',
            onSelected: (index) => ref
                .read(emotionSelectedChildIndexProvider.notifier)
                .select(index),
          ),
        ),
        const SizedBox(height: 12),
        if (state.notice != null) ...<Widget>[
          AppReveal(child: _Notice(message: _noticeText(state.notice!))),
          const SizedBox(height: 12),
        ],
        if (state.isEnabled)
          _EnabledBody(state: state, selectedChild: selectedChild)
        else
          _DisabledBody(state: state, selectedChild: selectedChild),
      ],
    );
  }

  String _noticeText(EmotionNotice notice) {
    return switch (notice) {
      EmotionNotice.enabled => EmotionCopy.switchOnHint,
      EmotionNotice.disabled => EmotionCopy.disabledNotice,
    };
  }

  void _leave(BuildContext context) {
    if (context.canPop()) {
      context.pop();
      return;
    }
    context.go('/me');
  }
}

/// The explanation and the switch, shown while sensing is off.
class _DisabledBody extends ConsumerWidget {
  const _DisabledBody({required this.state, required this.selectedChild});

  final EmotionState state;
  final ChildProfile? selectedChild;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        AppReveal(
          child: _IntroCard(
            // Every claim below is a promise the platform keeps: the label
            // vocabulary, the absence of media, the aggregate-only contract,
            // and the retention window that the backend worker enforces.
            items: const <String>[
              EmotionCopy.privacyTagOnly,
              EmotionCopy.privacyNoMedia,
              EmotionCopy.privacyGuardianOnly,
              EmotionCopy.privacyNoMoments,
              EmotionCopy.privacyRetention,
              EmotionCopy.privacySwitchOff,
            ],
          ),
        ),
        const SizedBox(height: 12),
        AppReveal(
          delay: const Duration(milliseconds: 60),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.all(20),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: <Widget>[
                  FilledButton(
                    key: const ValueKey<String>('emotion-enable'),
                    onPressed: state.isSaving
                        ? null
                        : () => ref
                              .read(emotionControllerProvider.notifier)
                              .enable(),
                    child: Text(
                      state.isSaving
                          ? EmotionCopy.enablingButton
                          : EmotionCopy.enableButton,
                    ),
                  ),
                  const SizedBox(height: 10),
                  Text(
                    _childPhrase(state.settings, selectedChild),
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                  if (state.error != null) ...<Widget>[
                    const SizedBox(height: 12),
                    _InlineError(message: emotionMessageFor(state.error)),
                  ],
                ],
              ),
            ),
          ),
        ),
      ],
    );
  }

  /// Names who the distribution would be about, so the switch is not blind.
  String _childPhrase(EmotionSettings? settings, ChildProfile? child) {
    final name = child?.nickname ?? EmotionCopy.childFamilyWide;
    return '开启后可以在下方切换看哪个孩子的分布。现在是「$name」。';
  }
}

/// The switch, the window, the distribution and the retention choice.
class _EnabledBody extends ConsumerWidget {
  const _EnabledBody({required this.state, required this.selectedChild});

  final EmotionState state;
  final ChildProfile? selectedChild;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final summary = state.summary;
    final settings = state.settings;
    // The window chips and the distribution card are keyed on the phase of the
    // read, so a second read of the same length does not replay the animation
    // while a change of length does. The retention card is deliberately not
    // keyed: it does not change when the window does, and re-animating it would
    // make a guardian wait to reach the same control again.
    final windowPhase = state.isLoading ? 'holding' : 'shown';
    final windowChip = ValueKey<String>('emotion-window-$windowPhase');
    // A failed save is shown where the control that failed is; a failed read
    // is already on screen as the missing card.
    final saveError = state.notice == null ? state.error : null;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        AppReveal(
          child: Card(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(20, 16, 12, 16),
              child: Row(
                children: <Widget>[
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: <Widget>[
                        Text(
                          EmotionCopy.switchLabel,
                          style: theme.textTheme.titleMedium,
                        ),
                        const SizedBox(height: 4),
                        Text(
                          EmotionCopy.switchOnHint,
                          style: theme.textTheme.bodySmall,
                        ),
                      ],
                    ),
                  ),
                  Switch(
                    key: const ValueKey<String>('emotion-switch'),
                    value: true,
                    onChanged: state.isSaving
                        ? null
                        : (_) => _confirmDisable(context, ref),
                  ),
                ],
              ),
            ),
          ),
        ),
        const SizedBox(height: 12),
        AppReveal(
          delay: const Duration(milliseconds: 40),
          child: _WindowSelector(
            // While a window is being read the selection shown is the one
            // asked for, not the one that is still on screen, so the chip and
            // the card cannot disagree about which period is displayed.
            key: windowChip,
            days: summary?.days ?? ref.watch(emotionEffectiveWindowProvider),
          ),
        ),
        const SizedBox(height: 12),
        if (state.isLoading)
          const _LoadingState(key: ValueKey<String>('emotion-summary-loading'))
        else if (summary == null || !summary.hasData)
          _EmptyState(
            key: ValueKey<String>('emotion-summary-empty'),
            title: EmotionCopy.emptyTitle,
            body: EmotionCopy.emptyBody,
          )
        else
          _DistributionCard(
            key: ValueKey<String>('emotion-summary-$windowPhase'),
            summary: summary,
            childName: selectedChild?.nickname ?? EmotionCopy.childFamilyWide,
          ),
        // A read that failed is already on screen as the missing card, so only
        // a failure of the switch itself is repeated beside it.
        if (state.error != null && saveError != null) ...<Widget>[
          const SizedBox(height: 12),
          _InlineError(message: emotionMessageFor(saveError)),
        ],
        if (settings != null) ...<Widget>[
          const SizedBox(height: 12),
          _RetentionCard(settings: settings, isSaving: state.isSavingRetention),
        ],
      ],
    );
  }

  Future<void> _confirmDisable(BuildContext context, WidgetRef ref) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text(EmotionCopy.disableConfirmTitle),
        content: const Text(EmotionCopy.disableConfirmBody),
        actions: <Widget>[
          TextButton(
            key: const ValueKey<String>('emotion-disable-cancel'),
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text(EmotionCopy.disableKeep),
          ),
          FilledButton(
            key: const ValueKey<String>('emotion-disable-confirm'),
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text(EmotionCopy.disableConfirm),
          ),
        ],
      ),
    );
    if (confirmed != true) {
      return;
    }
    await ref.read(emotionControllerProvider.notifier).disable();
  }
}

/// The two windows a guardian can compare.
class _WindowSelector extends ConsumerWidget {
  const _WindowSelector({required this.days, super.key});

  final int days;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: <Widget>[
        for (final option in emotionWindowDays)
          ChoiceChip(
            key: ValueKey<String>('emotion-window-$option'),
            label: Text(
              option == 7 ? EmotionCopy.window7 : EmotionCopy.window30,
            ),
            selected: option == days,
            onSelected: (selected) {
              if (!selected) {
                return;
              }
              ref.read(emotionWindowProvider.notifier).select(option);
            },
          ),
      ],
    );
  }
}

/// The distribution itself: a sentence, the counts, and the shares as bars.
///
/// Bars are proportions within one period, not a sequence in time, so the card
/// cannot be read as "what happened when".
class _DistributionCard extends StatelessWidget {
  const _DistributionCard({
    required this.summary,
    required this.childName,
    super.key,
  });

  final EmotionSummary summary;
  final String childName;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final recorded = summary.recordedShares;
    final dominant = summary.dominantLabel;
    final comparable = recorded
        .where((share) => share.trend != EmotionTrend.unknown)
        .toList(growable: false);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Row(
              children: <Widget>[
                Expanded(
                  child: Text(
                    EmotionCopy.summaryTitle,
                    style: theme.textTheme.titleMedium,
                  ),
                ),
                Text(
                  EmotionCopy.windowPhrase(summary.days),
                  style: theme.textTheme.bodySmall,
                ),
              ],
            ),
            const SizedBox(height: 4),
            Text(
              '${EmotionCopy.sampleTotal(summary.totalSamples)} · $childName',
              style: theme.textTheme.bodySmall,
            ),
            const SizedBox(height: 10),
            Text(
              EmotionCopy.dominantPhrase(summary.days, dominant),
              style: theme.textTheme.bodyMedium,
            ),
            const SizedBox(height: 14),
            for (final share in recorded)
              _ShareBar(
                key: ValueKey<String>('emotion-share-${share.label}'),
                share: share,
              ),
            const SizedBox(height: 8),
            Text(EmotionCopy.trendTitle, style: theme.textTheme.titleSmall),
            const SizedBox(height: 4),
            Text(
              comparable.isEmpty
                  ? EmotionCopy.trendNoComparison
                  : _trendLine(comparable),
              key: const ValueKey<String>('emotion-trend'),
              style: theme.textTheme.bodyMedium,
            ),
            if (summary.days > 0) ...<Widget>[
              const SizedBox(height: 10),
              Text(
                _windowDescription(summary),
                style: theme.textTheme.bodySmall,
              ),
            ],
          ],
        ),
      ),
    );
  }

  /// Names the window in calendar days, so "最近 30 天" is never ambiguous.
  String _windowDescription(EmotionSummary summary) {
    final start = summary.periodStart;
    final end = summary.periodEnd;
    if (start == null || end == null) {
      return '';
    }
    return '统计区间：${_date(start)} 至 ${_date(end)}';
  }

  static String _date(DateTime value) {
    final local = value.toLocal();
    final month = local.month.toString().padLeft(2, '0');
    final day = local.day.toString().padLeft(2, '0');
    return '$month-$day';
  }

  static String _trendLine(List<EmotionLabelShare> shares) {
    return shares.map(EmotionCopy.trendPhrase).join('，');
  }
}

/// One label's count and share.
class _ShareBar extends StatelessWidget {
  const _ShareBar({required this.share, super.key});

  final EmotionLabelShare share;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final fraction = (share.percentage / 100).clamp(0.0, 1.0);
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text(
            EmotionCopy.labelDetail(share),
            style: theme.textTheme.bodySmall,
          ),
          const SizedBox(height: 4),
          ClipRRect(
            borderRadius: BorderRadius.circular(8),
            child: LinearProgressIndicator(
              value: fraction,
              minHeight: 8,
              backgroundColor: const Color(0xFFFFF2F5),
              color: const Color(0xFFF7A8BF),
            ),
          ),
        ],
      ),
    );
  }
}

/// How long samples are kept.
class _RetentionCard extends ConsumerWidget {
  const _RetentionCard({required this.settings, required this.isSaving});

  final EmotionSettings settings;
  final bool isSaving;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final offered = settings.offeredRetentionDays;
    // A stored period that is not one of the offered chips is kept as its own
    // chip, so the selected value is always visible.
    final isStoredOffered = offered.contains(settings.retentionDays);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(
              EmotionCopy.retentionTitle,
              style: theme.textTheme.titleMedium,
            ),
            const SizedBox(height: 4),
            Text(EmotionCopy.retentionBody, style: theme.textTheme.bodySmall),
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: <Widget>[
                for (final days in offered)
                  ChoiceChip(
                    key: ValueKey<String>('emotion-retention-$days'),
                    label: Text(EmotionCopy.retentionDays(days)),
                    selected: settings.retentionDays == days,
                    onSelected: isSaving
                        ? null
                        : (selected) {
                            if (!selected || settings.retentionDays == days) {
                              return;
                            }
                            ref
                                .read(emotionControllerProvider.notifier)
                                .setRetentionDays(days);
                          },
                  ),
                if (!isStoredOffered && settings.retentionDays > 0)
                  ChoiceChip(
                    label: Text(
                      EmotionCopy.retentionDays(settings.retentionDays),
                    ),
                    selected: true,
                    onSelected: null,
                  ),
              ],
            ),
            if (isSaving) ...<Widget>[
              const SizedBox(height: 10),
              Text(
                EmotionCopy.retentionSaving,
                style: theme.textTheme.bodySmall,
              ),
            ],
          ],
        ),
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

  /// The position picked: -1 is the whole family, anything else indexes
  /// [children].
  final ValueChanged<int> onSelected;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text(EmotionCopy.childTitle, style: theme.textTheme.bodySmall),
        const SizedBox(height: 6),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: <Widget>[
            // The family-wide view is the platform's default for the summary,
            // so it stays available beside the named children.
            ChoiceChip(
              key: const ValueKey<String>('emotion-child-family'),
              label: const Text(EmotionCopy.childFamilyWide),
              selected: selectedId.isEmpty,
              onSelected: (_) => onSelected(-1),
            ),
            for (final (index, child) in children.indexed)
              ChoiceChip(
                key: ValueKey<String>('emotion-child-${child.childId}'),
                label: Text(child.nickname),
                selected: selectedId == child.childId,
                onSelected: (_) => onSelected(index),
              ),
          ],
        ),
        // The family-wide view is named on the distribution card itself, so the
        // explanation is not repeated above it; only the choice stays here.
      ],
    );
  }
}

/// The explanation shown before sensing is on.
class _IntroCard extends StatelessWidget {
  const _IntroCard({required this.items});

  final List<String> items;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Text(EmotionCopy.introTitle, style: theme.textTheme.titleMedium),
            const SizedBox(height: 8),
            Text(EmotionCopy.introBody, style: theme.textTheme.bodyMedium),
            const SizedBox(height: 16),
            Text(EmotionCopy.privacyTitle, style: theme.textTheme.titleSmall),
            const SizedBox(height: 8),
            for (final item in items)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    const Padding(
                      padding: EdgeInsets.only(top: 2),
                      child: Icon(
                        Icons.circle,
                        size: 6,
                        color: Color(0xFFF7A8BF),
                      ),
                    ),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(item, style: theme.textTheme.bodySmall),
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

class _Notice extends StatelessWidget {
  const _Notice({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      liveRegion: true,
      child: DecoratedBox(
        decoration: BoxDecoration(
          color: const Color(0xFFFFF2F5),
          borderRadius: BorderRadius.circular(16),
        ),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
          child: Text(message, style: Theme.of(context).textTheme.bodySmall),
        ),
      ),
    );
  }
}

class _InlineError extends StatelessWidget {
  const _InlineError({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: Theme.of(
          context,
        ).colorScheme.errorContainer.withValues(alpha: 0.45),
        borderRadius: BorderRadius.circular(14),
      ),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Text(
          message,
          style: TextStyle(
            color: Theme.of(context).colorScheme.onErrorContainer,
          ),
        ),
      ),
    );
  }
}

class _LoadingState extends StatelessWidget {
  const _LoadingState({super.key});

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
              EmotionCopy.loading,
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
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 16),
            FilledButton(
              onPressed: onRetry,
              child: const Text(EmotionCopy.retryButton),
            ),
          ],
        ),
      ),
    );
  }
}

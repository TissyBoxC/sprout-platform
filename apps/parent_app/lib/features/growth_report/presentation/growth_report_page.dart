import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/error/app_exception.dart';
import '../../child_profile/application/child_profile_controller.dart';
import '../../child_profile/domain/child_profile.dart';
import '../application/growth_report_controller.dart';
import '../domain/growth_report.dart';

/// Guardian view of the automatically generated growth reports.
///
/// A report is a summary the platform produced, not a live figure, so the screen
/// always names the window a report covers. Nothing here shows conversation
/// text or a memory value: the report itself never carries them.
class GrowthReportPage extends ConsumerStatefulWidget {
  const GrowthReportPage({super.key});

  @override
  ConsumerState<GrowthReportPage> createState() => _GrowthReportPageState();
}

class _GrowthReportPageState extends ConsumerState<GrowthReportPage> {
  String? _childId;

  @override
  Widget build(BuildContext context) {
    final children = ref.watch(childProfileControllerProvider);
    final state = ref.watch(growthReportControllerProvider);

    return Scaffold(
      appBar: AppBar(
        title: const Text('成长报告'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: children.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (error, _) => _ErrorState(
          message: _errorMessage(error),
          onRetry: () => ref.invalidate(childProfileControllerProvider),
        ),
        data: (profiles) {
          if (profiles.isEmpty) {
            return const _EmptyState(
              title: '还没有儿童档案',
              description: '添加儿童档案后，成长报告会按周和按月自动生成。',
            );
          }
          final selected = _resolveChildId(profiles);
          return Column(
            children: <Widget>[
              if (profiles.length > 1)
                _ChildSelector(
                  children: profiles,
                  selectedId: selected,
                  onSelected: (childId) {
                    setState(() => _childId = childId);
                    ref.read(growthReportControllerProvider.notifier).load(
                      childId,
                    );
                  },
                ),
              Expanded(
                child: state.when(
                  loading: () => const Center(
                    child: CircularProgressIndicator(),
                  ),
                  error: (error, _) => _ErrorState(
                    message: _errorMessage(error),
                    onRetry: () => ref
                        .read(growthReportControllerProvider.notifier)
                        .load(selected),
                  ),
                  data: (value) => _ReportList(
                    state: value,
                    onFilter: (filter) => ref
                        .read(growthReportControllerProvider.notifier)
                        .setFilter(filter),
                  ),
                ),
              ),
            ],
          );
        },
      ),
    );
  }

  /// Picks the remembered child, or the first one, and loads on first build.
  String _resolveChildId(List<ChildProfile> profiles) {
    final selected = _childId;
    if (selected != null && profiles.any((child) => child.childId == selected)) {
      return selected;
    }
    final first = profiles.first.childId;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted || _childId == first) {
        return;
      }
      setState(() => _childId = first);
      ref.read(growthReportControllerProvider.notifier).load(first);
    });
    return first;
  }

  void _leave(BuildContext context) {
    if (context.canPop()) {
      context.pop();
      return;
    }
    context.go('/me');
  }
}

class _ReportList extends StatelessWidget {
  const _ReportList({required this.state, required this.onFilter});

  final GrowthReportState state;
  final ValueChanged<GrowthReportPeriod?> onFilter;

  @override
  Widget build(BuildContext context) {
    if (state.isEmpty) {
      return const _EmptyState(
        title: '还没有成长报告',
        description: '报告会在每周或每月结束时自动生成。有了练习或使用记录后，这里会显示汇总。',
      );
    }
    final visible = state.visibleReports;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 4),
          child: Wrap(
            spacing: 8,
            children: <Widget>[
              ChoiceChip(
                label: const Text('全部'),
                selected: state.filter == null,
                onSelected: (_) => onFilter(null),
              ),
              for (final period in GrowthReportPeriod.values)
                ChoiceChip(
                  label: Text(period.label),
                  selected: state.filter == period,
                  onSelected: (_) => onFilter(period),
                ),
            ],
          ),
        ),
        Expanded(
          child: visible.isEmpty
              ? const _EmptyState(
                  title: '这个周期还没有报告',
                  description: '换一个周期类型看看，或等下一次汇总生成。',
                )
              : ListView.separated(
                  padding: const EdgeInsets.fromLTRB(16, 8, 16, 24),
                  itemCount: visible.length,
                  separatorBuilder: (_, _) => const SizedBox(height: 12),
                  itemBuilder: (context, index) =>
                      _ReportCard(report: visible[index]),
                ),
        ),
      ],
    );
  }
}

class _ReportCard extends StatelessWidget {
  const _ReportCard({required this.report});

  final GrowthReport report;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Row(
              children: <Widget>[
                Text(report.period.label, style: theme.textTheme.titleMedium),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    report.periodLabel,
                    style: theme.textTheme.bodySmall,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 10),
            Text(report.summary, style: theme.textTheme.bodyMedium),
            const SizedBox(height: 14),
            Wrap(
              spacing: 20,
              runSpacing: 10,
              children: <Widget>[
                _Stat(
                  label: '完成率',
                  value: report.accuracyPhrase,
                ),
                _Stat(
                  label: '练习次数',
                  value: '${report.practice.attempts}',
                  delta: report.practiceDelta,
                  deltaUnit: '次',
                ),
                _Stat(
                  label: '平均得分',
                  value: report.practice.isEmpty
                      ? '—'
                      : report.practice.averageScore.toStringAsFixed(1),
                ),
                _Stat(
                  label: '设备使用',
                  value: '${report.usage.activeMinutes} 分钟',
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _Stat extends StatelessWidget {
  const _Stat({
    required this.label,
    required this.value,
    this.delta,
    this.deltaUnit,
  });

  final String label;
  final String value;
  final int? delta;
  final String? deltaUnit;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final change = delta;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Text(label, style: theme.textTheme.bodySmall),
        const SizedBox(height: 2),
        Text(value, style: theme.textTheme.titleMedium),
        if (change != null && change != 0) ...<Widget>[
          const SizedBox(height: 2),
          Text(
            change > 0
                ? '比上期多 $change${deltaUnit ?? ''}'
                : '比上期少 ${-change}${deltaUnit ?? ''}',
            style: theme.textTheme.bodySmall,
          ),
        ],
      ],
    );
  }
}

class _ChildSelector extends StatelessWidget {
  const _ChildSelector({
    required this.children,
    required this.selectedId,
    required this.onSelected,
  });

  final List<ChildProfile> children;
  final String selectedId;
  final ValueChanged<String> onSelected;

  @override
  Widget build(BuildContext context) {
    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 0),
      child: Row(
        children: <Widget>[
          for (final child in children)
            Padding(
              padding: const EdgeInsets.only(right: 8),
              child: ChoiceChip(
                label: Text(child.nickname),
                selected: child.childId == selectedId,
                onSelected: (_) => onSelected(child.childId),
              ),
            ),
        ],
      ),
    );
  }
}

class _EmptyState extends StatelessWidget {
  const _EmptyState({required this.title, required this.description});

  final String title;
  final String description;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Text(title, style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 8),
            Text(
              description,
              textAlign: TextAlign.center,
              style: Theme.of(context).textTheme.bodyMedium,
            ),
          ],
        ),
      ),
    );
  }
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({required this.message, required this.onRetry});

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
            FilledButton(onPressed: onRetry, child: const Text('重新加载')),
          ],
        ),
      ),
    );
  }
}

/// Maps a transport or domain failure to text a guardian can act on.
String _errorMessage(Object? error) {
  if (error is AppException) {
    return error.message;
  }
  return '暂时读不到成长报告，请稍后重新加载。';
}

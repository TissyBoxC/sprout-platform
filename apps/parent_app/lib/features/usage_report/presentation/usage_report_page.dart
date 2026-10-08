import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../../child_profile/domain/child_profile.dart';
import '../application/usage_report_controller.dart';
import '../domain/usage_report.dart';

/// Shows daily trends, content usage, limits, and per-device activity.
class UsageReportPage extends ConsumerWidget {
  const UsageReportPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final report = ref.watch(usageReportControllerProvider);
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) {
          _leave(context);
        }
      },
      child: Scaffold(
        appBar: AppBar(
          title: const Text('使用记录'),
          leading: IconButton(
            tooltip: '返回',
            onPressed: () => _leave(context),
            icon: const Icon(Icons.arrow_back_rounded),
          ),
        ),
        body: AppStateSwitcher(
          stateKey: report.when(
            data: (value) => value.isEmpty ? 'usage-empty' : 'usage-content',
            error: (_, _) => 'usage-error',
            loading: () => 'usage-loading',
          ),
          child: report.when(
            loading: () => const Center(
              key: ValueKey<String>('usage-loading'),
              child: CircularProgressIndicator(),
            ),
            error: (error, _) => _UsageErrorState(
              key: const ValueKey<String>('usage-error'),
              message: usageReportErrorMessage(error),
              onRetry: () =>
                  ref.read(usageReportControllerProvider.notifier).refresh(),
            ),
            data: (value) => value.isEmpty
                ? const _UsageEmptyState(key: ValueKey<String>('usage-empty'))
                : _UsageReportContent(
                    key: const ValueKey<String>('usage-content'),
                    report: value,
                    selectedRangeDays: ref
                        .watch(usageReportControllerProvider.notifier)
                        .days,
                  ),
          ),
        ),
      ),
    );
  }
}

void _leave(BuildContext context) {
  if (context.canPop()) {
    context.pop();
  } else {
    context.go('/home');
  }
}

class _UsageErrorState extends StatelessWidget {
  const _UsageErrorState({
    required this.message,
    required this.onRetry,
    super.key,
  });

  final String message;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.cloud_off_outlined,
              size: 44,
              color: Theme.of(context).colorScheme.primary,
            ),
            const SizedBox(height: 14),
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 16),
            FilledButton.icon(
              onPressed: onRetry,
              icon: const Icon(Icons.refresh_rounded),
              label: const Text('重新加载'),
            ),
          ],
        ),
      ),
    );
  }
}

class _UsageEmptyState extends StatelessWidget {
  const _UsageEmptyState({super.key});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(28),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Container(
              width: 78,
              height: 78,
              decoration: BoxDecoration(
                color: Theme.of(context).colorScheme.primaryContainer,
                shape: BoxShape.circle,
              ),
              child: Icon(
                Icons.auto_graph_rounded,
                size: 38,
                color: Theme.of(context).colorScheme.primary,
              ),
            ),
            const SizedBox(height: 18),
            Text('还没有使用记录', style: Theme.of(context).textTheme.titleLarge),
            const SizedBox(height: 8),
            const Text(
              '设备联网并开始陪伴后，这里会按天显示使用时长、对话和内容记录。',
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 18),
            OutlinedButton.icon(
              onPressed: () => context.go('/devices'),
              icon: const Icon(Icons.toys_outlined),
              label: const Text('查看设备'),
            ),
          ],
        ),
      ),
    );
  }
}

class _UsageReportContent extends ConsumerStatefulWidget {
  const _UsageReportContent({
    required this.report,
    required this.selectedRangeDays,
    super.key,
  });

  final UsageReport report;
  final int selectedRangeDays;

  @override
  ConsumerState<_UsageReportContent> createState() =>
      _UsageReportContentState();
}

class _UsageReportContentState extends ConsumerState<_UsageReportContent> {
  DateTime? _selectedDate;

  @override
  Widget build(BuildContext context) {
    final days = widget.report.days;
    if (days.isEmpty) {
      return const _UsageEmptyState();
    }
    final selectedDay = days
        .where((day) => _sameDate(day.reportDate, _selectedDate))
        .firstOrNull;
    final day = selectedDay ?? days.first;
    final isToday = _sameDate(day.reportDate, days.first.reportDate);
    return RefreshIndicator(
      onRefresh: () =>
          ref.read(usageReportControllerProvider.notifier).refresh(),
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: [
          AppReveal(
            child: _RangeSelector(selectedDays: widget.selectedRangeDays),
          ),
          const SizedBox(height: 16),
          AppReveal(
            child: _TodaySummaryCard(day: day, isToday: isToday),
          ),
          const SizedBox(height: 16),
          AppReveal(
            delay: const Duration(milliseconds: 50),
            child: _TrendCard(
              days: days,
              selectedDate: day.reportDate,
              onSelected: (date) => setState(() => _selectedDate = date),
            ),
          ),
          const SizedBox(height: 16),
          AppReveal(
            delay: const Duration(milliseconds: 100),
            child: _CategoryCard(day: day),
          ),
          const SizedBox(height: 16),
          AppReveal(
            delay: const Duration(milliseconds: 130),
            child: _BlockedCard(blocked: day.blocked),
          ),
          const SizedBox(height: 16),
          AppReveal(
            delay: const Duration(milliseconds: 160),
            child: _DeviceCard(devices: day.devices),
          ),
        ],
      ),
    );
  }
}

class _RangeSelector extends ConsumerWidget {
  const _RangeSelector({required this.selectedDays});

  final int selectedDays;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.date_range_rounded),
                const SizedBox(width: 10),
                Text('查看范围', style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 12),
            SegmentedButton<int>(
              segments: const [
                ButtonSegment<int>(value: 7, label: Text('近 7 天')),
                ButtonSegment<int>(value: 30, label: Text('近 30 天')),
                ButtonSegment<int>(value: 90, label: Text('近 90 天')),
              ],
              selected: {selectedDays},
              onSelectionChanged: (selection) {
                ref
                    .read(usageReportControllerProvider.notifier)
                    .setRange(selection.first);
              },
            ),
          ],
        ),
      ),
    );
  }
}

class _TodaySummaryCard extends StatelessWidget {
  const _TodaySummaryCard({required this.day, required this.isToday});

  final UsageReportDay day;
  final bool isToday;

  @override
  Widget build(BuildContext context) {
    final remainingLabel = day.isUnlimited
        ? '不限制'
        : day.limitReached
        ? '今日已用完'
        : '${day.remainingMinutes} 分钟';
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Container(
                  width: 42,
                  height: 42,
                  decoration: BoxDecoration(
                    color: Theme.of(context).colorScheme.primaryContainer,
                    borderRadius: BorderRadius.circular(14),
                  ),
                  child: Icon(
                    Icons.today_rounded,
                    color: Theme.of(context).colorScheme.primary,
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        isToday ? '今日陪伴' : '当天记录',
                        style: Theme.of(context).textTheme.titleLarge,
                      ),
                      Text(
                        usageReportDateLabel(day.reportDate),
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                    ],
                  ),
                ),
                _LimitPill(day: day),
              ],
            ),
            const SizedBox(height: 20),
            Row(
              children: [
                Expanded(
                  child: _SummaryMetric(
                    label: '使用时长',
                    value: '${day.activeMinutes}',
                    unit: '分钟',
                    icon: Icons.schedule_rounded,
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: _SummaryMetric(
                    label: '对话',
                    value: '${day.conversationCount}',
                    unit: '次',
                    icon: Icons.chat_bubble_outline_rounded,
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: _SummaryMetric(
                    label: '内容',
                    value: '${day.contentPlayCount}',
                    unit: '次',
                    icon: Icons.auto_stories_outlined,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 18),
            Row(
              children: [
                Text(
                  day.isUnlimited ? '今日不限时' : '今日剩余',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
                const Spacer(),
                Text(
                  remainingLabel,
                  style: Theme.of(context).textTheme.titleMedium?.copyWith(
                    color: day.limitReached
                        ? Theme.of(context).colorScheme.error
                        : Theme.of(context).colorScheme.primary,
                    fontWeight: FontWeight.w700,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            ClipRRect(
              borderRadius: BorderRadius.circular(99),
              child: TweenAnimationBuilder<double>(
                tween: Tween(begin: 0, end: day.usagePercent / 100),
                duration: const Duration(milliseconds: 520),
                curve: Curves.easeOutCubic,
                builder: (context, value, _) => LinearProgressIndicator(
                  value: value,
                  minHeight: 10,
                  backgroundColor: Theme.of(
                    context,
                  ).colorScheme.surfaceContainer,
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _LimitPill extends StatelessWidget {
  const _LimitPill({required this.day});

  final UsageReportDay day;

  @override
  Widget build(BuildContext context) {
    final label = day.isUnlimited
        ? '不限时'
        : day.limitReached
        ? '已达上限'
        : '${day.usagePercent}%';
    final color = day.limitReached
        ? Theme.of(context).colorScheme.error
        : Theme.of(context).colorScheme.primary;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.1),
        borderRadius: BorderRadius.circular(99),
      ),
      child: Text(
        label,
        style: Theme.of(context).textTheme.labelMedium?.copyWith(color: color),
      ),
    );
  }
}

class _SummaryMetric extends StatelessWidget {
  const _SummaryMetric({
    required this.label,
    required this.value,
    required this.unit,
    required this.icon,
  });

  final String label;
  final String value;
  final String unit;
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: Theme.of(context).colorScheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 19, color: Theme.of(context).colorScheme.primary),
          const SizedBox(height: 9),
          Text(label, style: Theme.of(context).textTheme.bodySmall),
          const SizedBox(height: 3),
          FittedBox(
            fit: BoxFit.scaleDown,
            alignment: Alignment.centerLeft,
            child: Text.rich(
              TextSpan(
                text: value,
                style: Theme.of(
                  context,
                ).textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w700),
                children: [
                  TextSpan(
                    text: ' $unit',
                    style: Theme.of(context).textTheme.bodySmall,
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

class _TrendCard extends StatelessWidget {
  const _TrendCard({
    required this.days,
    required this.selectedDate,
    required this.onSelected,
  });

  final List<UsageReportDay> days;
  final DateTime selectedDate;
  final ValueChanged<DateTime> onSelected;

  @override
  Widget build(BuildContext context) {
    final chronological = days.reversed.toList(growable: false);
    final visible = chronological.length > 14
        ? chronological.sublist(chronological.length - 14)
        : chronological;
    final peak = visible.fold<int>(
      0,
      (maximum, day) =>
          day.activeMinutes > maximum ? day.activeMinutes : maximum,
    );
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.bar_chart_rounded),
                const SizedBox(width: 10),
                Text('每日使用趋势', style: Theme.of(context).textTheme.titleMedium),
                const Spacer(),
                Text(
                  '最近 ${visible.length} 天',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
            ),
            const SizedBox(height: 18),
            SizedBox(
              height: 154,
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  for (final day in visible)
                    Expanded(
                      child: _TrendBar(
                        day: day,
                        peakMinutes: peak,
                        isSelected: _sameDate(day.reportDate, selectedDate),
                        onTap: () => onSelected(day.reportDate),
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

class _TrendBar extends StatelessWidget {
  const _TrendBar({
    required this.day,
    required this.peakMinutes,
    required this.isSelected,
    required this.onTap,
  });

  final UsageReportDay day;
  final int peakMinutes;
  final bool isSelected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final ratio = peakMinutes <= 0
        ? 0.0
        : (day.activeMinutes / peakMinutes).clamp(0.0, 1.0);
    final height = 16 + ratio * 88;
    final color = day.limitReached
        ? Theme.of(context).colorScheme.error
        : Theme.of(context).colorScheme.primary;
    return Tooltip(
      message:
          '${usageReportDateLabel(day.reportDate)}：${day.activeMinutes} 分钟',
      child: InkWell(
        key: ValueKey<String>(
          'usage-bar-${day.reportDate.toIso8601String().split('T').first}',
        ),
        onTap: onTap,
        borderRadius: BorderRadius.circular(12),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 3),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.end,
            children: [
              Text(
                day.activeMinutes == 0 ? '' : '${day.activeMinutes}',
                style: Theme.of(context).textTheme.labelSmall,
              ),
              const SizedBox(height: 4),
              TweenAnimationBuilder<double>(
                tween: Tween(begin: 0, end: height),
                duration: const Duration(milliseconds: 520),
                curve: Curves.easeOutCubic,
                builder: (context, value, _) => Container(
                  height: value,
                  decoration: BoxDecoration(
                    color: color.withValues(alpha: isSelected ? 1 : 0.72),
                    borderRadius: BorderRadius.circular(9),
                    border: isSelected
                        ? Border.all(color: color, width: 2)
                        : null,
                  ),
                ),
              ),
              const SizedBox(height: 7),
              Text(
                '${day.reportDate.day}',
                style: Theme.of(context).textTheme.labelSmall?.copyWith(
                  fontWeight: isSelected ? FontWeight.w700 : null,
                  color: isSelected ? color : null,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

bool _sameDate(DateTime? left, DateTime? right) {
  if (left == null || right == null) {
    return false;
  }
  return left.year == right.year &&
      left.month == right.month &&
      left.day == right.day;
}

class _CategoryCard extends StatelessWidget {
  const _CategoryCard({required this.day});

  final UsageReportDay day;

  @override
  Widget build(BuildContext context) {
    final totalMinutes = day.categories.fold<int>(
      0,
      (total, category) => total + category.minutes,
    );
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.library_music_outlined),
                const SizedBox(width: 10),
                Text('内容分类', style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 6),
            const Text('这里只记录分类和时长，不会显示孩子听过的具体内容。'),
            const SizedBox(height: 14),
            if (day.categories.isEmpty)
              const ListTile(
                contentPadding: EdgeInsets.zero,
                leading: Icon(Icons.stars_outlined),
                title: Text('今天还没有播放内容'),
                subtitle: Text('开始播放故事或儿歌后，分类记录会显示在这里。'),
              )
            else
              for (final category in day.categories)
                _CategoryRow(category: category, totalMinutes: totalMinutes),
          ],
        ),
      ),
    );
  }
}

class _CategoryRow extends StatelessWidget {
  const _CategoryRow({required this.category, required this.totalMinutes});

  final UsageReportCategory category;
  final int totalMinutes;

  @override
  Widget build(BuildContext context) {
    final ratio = totalMinutes <= 0
        ? 0.0
        : (category.minutes / totalMinutes).clamp(0.0, 1.0);
    return Padding(
      padding: const EdgeInsets.only(bottom: 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(child: Text(category.category.label)),
              Text(
                '${category.minutes} 分钟 · ${category.playCount} 次',
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ],
          ),
          const SizedBox(height: 7),
          ClipRRect(
            borderRadius: BorderRadius.circular(99),
            child: TweenAnimationBuilder<double>(
              tween: Tween(begin: 0, end: ratio),
              duration: const Duration(milliseconds: 480),
              curve: Curves.easeOutCubic,
              builder: (context, value, _) => LinearProgressIndicator(
                value: value,
                minHeight: 8,
                backgroundColor: Theme.of(context).colorScheme.surfaceContainer,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _BlockedCard extends StatelessWidget {
  const _BlockedCard({required this.blocked});

  final UsageReportBlocked blocked;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.shield_outlined),
                const SizedBox(width: 10),
                Text('使用限制', style: Theme.of(context).textTheme.titleMedium),
                const Spacer(),
                Text(
                  blocked.total == 0 ? '今天没有拦截' : '共 ${blocked.total} 次',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
            ),
            const SizedBox(height: 12),
            if (blocked.total == 0)
              const ListTile(
                contentPadding: EdgeInsets.zero,
                leading: Icon(Icons.check_circle_outline_rounded),
                title: Text('今天都在设置范围内'),
                subtitle: Text('如果之后触发免打扰、时长或内容限制，会显示在这里。'),
              )
            else ...[
              _BlockedRow(
                label: '免打扰时段',
                count: blocked.disabledPeriod,
                icon: Icons.bedtime_outlined,
              ),
              _BlockedRow(
                label: '每日时长',
                count: blocked.dailyLimit,
                icon: Icons.timer_outlined,
              ),
              _BlockedRow(
                label: '内容分类',
                count: blocked.categoryDenied,
                icon: Icons.library_books_outlined,
              ),
              _BlockedRow(
                label: '时间尚未同步',
                count: blocked.timeUntrusted,
                icon: Icons.schedule_outlined,
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _BlockedRow extends StatelessWidget {
  const _BlockedRow({
    required this.label,
    required this.count,
    required this.icon,
  });

  final String label;
  final int count;
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    final isZero = count == 0;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        children: [
          Icon(
            icon,
            size: 20,
            color: isZero
                ? Theme.of(context).colorScheme.onSurfaceVariant
                : Theme.of(context).colorScheme.primary,
          ),
          const SizedBox(width: 12),
          Expanded(child: Text(label)),
          Text(
            '$count 次',
            style: Theme.of(
              context,
            ).textTheme.titleSmall?.copyWith(fontWeight: FontWeight.w700),
          ),
        ],
      ),
    );
  }
}

class _DeviceCard extends StatelessWidget {
  const _DeviceCard({required this.devices});

  final List<UsageReportDevice> devices;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.toys_outlined),
                const SizedBox(width: 10),
                Text('设备使用', style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 12),
            if (devices.isEmpty)
              const ListTile(
                contentPadding: EdgeInsets.zero,
                leading: Icon(Icons.info_outline_rounded),
                title: Text('今天还没有设备使用记录'),
                subtitle: Text('设备联网并开始陪伴后，会在这里显示每台设备的时长。'),
              )
            else
              for (final device in devices)
                Padding(
                  padding: const EdgeInsets.only(bottom: 12),
                  child: _DeviceUsageRow(device: device),
                ),
          ],
        ),
      ),
    );
  }
}

class _DeviceUsageRow extends StatelessWidget {
  const _DeviceUsageRow({required this.device});

  final UsageReportDevice device;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: Theme.of(context).colorScheme.outlineVariant),
      ),
      child: Row(
        children: [
          Container(
            width: 42,
            height: 42,
            decoration: BoxDecoration(
              color: Theme.of(context).colorScheme.primaryContainer,
              borderRadius: BorderRadius.circular(14),
            ),
            child: Icon(
              Icons.toys_rounded,
              color: Theme.of(context).colorScheme.primary,
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  device.deviceName,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: Theme.of(context).textTheme.titleSmall,
                ),
                const SizedBox(height: 4),
                Text(
                  '对话 ${device.conversationCount} 次 · 内容 ${device.contentPlayCount} 次',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
            ),
          ),
          const SizedBox(width: 10),
          Text(
            '${device.activeMinutes} 分钟',
            style: Theme.of(
              context,
            ).textTheme.titleSmall?.copyWith(fontWeight: FontWeight.w700),
          ),
        ],
      ),
    );
  }
}

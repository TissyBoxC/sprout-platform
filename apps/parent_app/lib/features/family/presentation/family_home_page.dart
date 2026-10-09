import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/error/app_exception.dart';
import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../../auth/application/auth_controller.dart';
import '../../auth/data/auth_api.dart';
import '../../device/application/device_binding_controller.dart';
import '../../device/data/device_binding_api.dart';

/// Guardian home screen with daily usage and device status summary.
class FamilyHomePage extends ConsumerStatefulWidget {
  const FamilyHomePage({super.key});

  @override
  ConsumerState<FamilyHomePage> createState() => _FamilyHomePageState();
}

class _FamilyHomePageState extends ConsumerState<FamilyHomePage> {
  Future<ParentOverview>? _overviewFuture;

  Future<ParentOverview> _loadOverview() {
    return ref.read(authApiProvider).overview();
  }

  Future<void> _refresh() async {
    final overviewFuture = _loadOverview();
    setState(() => _overviewFuture = overviewFuture);
    await Future.wait<void>([
      overviewFuture,
      ref.read(authControllerProvider.notifier).refreshAccount(),
      ref.read(deviceBindingControllerProvider.notifier).refresh(),
    ]);
  }

  @override
  Widget build(BuildContext context) {
    final auth = ref.watch(authControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: Row(
          children: [
            ClipOval(
              child: Image.asset(
                'assets/brand/sprout/brand_avatar.png',
                width: 32,
                height: 32,
                semanticLabel: '如此萌屋',
              ),
            ),
            const SizedBox(width: 10),
            const Text('首页'),
          ],
        ),
      ),
      body: AppStateSwitcher(
        stateKey: auth.when(
          data: (value) {
            if (value.canRetryRestore) {
              return 'restore-failed';
            }
            return value.account == null ? 'signed-out' : 'content';
          },
          error: (_, _) => 'error',
          loading: () => 'loading',
        ),
        child: auth.when(
          loading: () => const Center(
            key: ValueKey<String>('loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ErrorState(
            key: const ValueKey<String>('error'),
            message: authErrorMessage(error),
            onRetry: () =>
                ref.read(authControllerProvider.notifier).refreshAccount(),
          ),
          data: (state) {
            if (state.canRetryRestore) {
              return _RestoreErrorState(
                key: const ValueKey<String>('restore-failed'),
                message: state.errorMessage ?? '暂时无法恢复登录，请检查网络后重试',
                onRetry: () =>
                    ref.read(authControllerProvider.notifier).retryRestore(),
              );
            }
            final account = state.account;
            if (account == null) {
              return const _SignInPrompt(key: ValueKey<String>('signed-out'));
            }
            final overviewFuture = _overviewFuture ??= _loadOverview();
            final devices = ref.watch(deviceBindingControllerProvider);
            return RefreshIndicator(
              key: const ValueKey<String>('content'),
              onRefresh: _refresh,
              child: ListView(
                padding: const EdgeInsets.fromLTRB(16, 16, 16, 28),
                children: [
                  AppReveal(child: _GreetingCard(account: account)),
                  const SizedBox(height: 16),
                  AppReveal(
                    delay: const Duration(milliseconds: 60),
                    child: _DashboardCard(
                      overviewFuture: overviewFuture,
                      onRetry: () {
                        setState(() => _overviewFuture = _loadOverview());
                      },
                    ),
                  ),
                  const SizedBox(height: 20),
                  AppReveal(
                    delay: const Duration(milliseconds: 120),
                    child: _SectionHeader(
                      title: '最近设备',
                      actionLabel: '查看全部',
                      onAction: () => context.go('/devices'),
                    ),
                  ),
                  const SizedBox(height: 8),
                  AppReveal(
                    delay: const Duration(milliseconds: 90),
                    child: _FamilyToolsCard(
                      onChildProfiles: () => context.push('/me/children'),
                      onUsageReports: () => context.push('/me/usage-reports'),
                      onNotifications: () => context.push('/me/notifications'),
                    ),
                  ),
                  const SizedBox(height: 20),
                  devices.when(
                    loading: () => const _DeviceLoadingCard(),
                    error: (error, _) => _InlineError(
                      message: authErrorMessage(error),
                      onRetry: () => ref
                          .read(deviceBindingControllerProvider.notifier)
                          .refresh(),
                    ),
                    data: (deviceState) {
                      if (deviceState.errorMessage != null) {
                        return _InlineError(
                          message: deviceState.errorMessage!,
                          onRetry: () => ref
                              .read(deviceBindingControllerProvider.notifier)
                              .refresh(),
                        );
                      }
                      if (deviceState.bindings.isEmpty) {
                        return _EmptyDeviceCard(
                          onAdd: () => context.go('/devices/scan'),
                        );
                      }
                      return Column(
                        children: [
                          for (final device
                              in deviceState.bindings.take(2).toList())
                            Padding(
                              padding: const EdgeInsets.only(bottom: 10),
                              child: _RecentDeviceTile(
                                device: device,
                                onTap: () => context.go('/devices'),
                              ),
                            ),
                        ],
                      );
                    },
                  ),
                ],
              ),
            );
          },
        ),
      ),
    );
  }
}

class _FamilyToolsCard extends StatelessWidget {
  const _FamilyToolsCard({
    required this.onChildProfiles,
    required this.onUsageReports,
    required this.onNotifications,
  });

  final VoidCallback onChildProfiles;
  final VoidCallback onUsageReports;
  final VoidCallback onNotifications;

  @override
  Widget build(BuildContext context) {
    return Card(
      clipBehavior: Clip.antiAlias,
      child: Column(
        children: [
          ListTile(
            leading: const Icon(Icons.child_care_rounded),
            title: const Text('儿童档案'),
            subtitle: const Text('管理宝贝年龄、兴趣、内容和使用边界'),
            trailing: const Icon(Icons.chevron_right_rounded),
            onTap: onChildProfiles,
          ),
          const Divider(height: 1, indent: 16, endIndent: 16),
          ListTile(
            leading: const Icon(Icons.auto_graph_rounded),
            title: const Text('使用记录'),
            subtitle: const Text('查看每日时长、对话、内容和限制记录'),
            trailing: const Icon(Icons.chevron_right_rounded),
            onTap: onUsageReports,
          ),
          const Divider(height: 1, indent: 16, endIndent: 16),
          ListTile(
            leading: const Icon(Icons.mark_email_unread_outlined),
            title: const Text('消息通知'),
            subtitle: const Text('查看平台通知，给设备留言'),
            trailing: const Icon(Icons.chevron_right_rounded),
            onTap: onNotifications,
          ),
        ],
      ),
    );
  }
}

class _GreetingCard extends StatelessWidget {
  const _GreetingCard({required this.account});

  final ParentAccount account;

  @override
  Widget build(BuildContext context) {
    final greetingName = account.displayName.isNotEmpty
        ? account.displayName
        : '家长';
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Row(
          children: [
            Container(
              width: 56,
              height: 56,
              decoration: BoxDecoration(
                color: Theme.of(context).colorScheme.primaryContainer,
                shape: BoxShape.circle,
              ),
              child: Icon(
                Icons.wb_sunny_rounded,
                color: Theme.of(context).colorScheme.primary,
              ),
            ),
            const SizedBox(width: 14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '你好，$greetingName',
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                  const SizedBox(height: 4),
                  const Text('今天也一起陪孩子探索吧'),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _DashboardCard extends StatelessWidget {
  const _DashboardCard({required this.overviewFuture, required this.onRetry});

  final Future<ParentOverview> overviewFuture;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<ParentOverview>(
      future: overviewFuture,
      builder: (context, snapshot) {
        if (snapshot.connectionState == ConnectionState.waiting) {
          return const Card(
            child: Padding(
              padding: EdgeInsets.all(20),
              child: Center(child: CircularProgressIndicator()),
            ),
          );
        }
        if (snapshot.hasError || !snapshot.hasData) {
          return Card(
            child: Padding(
              padding: const EdgeInsets.all(20),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '暂时无法读取今日数据',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                  const SizedBox(height: 8),
                  Text(_overviewErrorMessage(snapshot.error)),
                  const SizedBox(height: 12),
                  OutlinedButton(onPressed: onRetry, child: const Text('重新加载')),
                ],
              ),
            ),
          );
        }
        final overview = snapshot.data!;
        return Card(
          child: Padding(
            padding: const EdgeInsets.all(20),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Text('今日陪伴', style: Theme.of(context).textTheme.titleLarge),
                    const Spacer(),
                    _StatusPill(
                      label: overview.onlineDeviceCount > 0 ? '设备在线' : '设备离线',
                      isActive: overview.onlineDeviceCount > 0,
                    ),
                  ],
                ),
                const SizedBox(height: 18),
                Row(
                  children: [
                    Expanded(
                      child: _MetricTile(
                        label: '今日对话',
                        value: '${overview.todayConversationCount}',
                        unit: '次',
                        icon: Icons.chat_bubble_outline_rounded,
                      ),
                    ),
                    const SizedBox(width: 12),
                    Expanded(
                      child: _MetricTile(
                        label: '今日消耗',
                        value: overview.todaySpentUsd.toStringAsFixed(2),
                        unit: '',
                        icon: Icons.trending_up_rounded,
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 16),
                _BalanceBar(
                  remaining: overview.remainingBalanceUsd,
                  total: overview.balanceUsd,
                ),
                const SizedBox(height: 10),
                Text(
                  '${overview.deviceCount} 台设备 · ${overview.onlineDeviceCount} 台在线',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
            ),
          ),
        );
      },
    );
  }
}

class _MetricTile extends StatelessWidget {
  const _MetricTile({
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
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.surfaceContainerLowest,
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: Theme.of(context).colorScheme.outlineVariant),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 20, color: Theme.of(context).colorScheme.primary),
          const SizedBox(height: 10),
          Text(label, style: Theme.of(context).textTheme.bodySmall),
          const SizedBox(height: 4),
          FittedBox(
            fit: BoxFit.scaleDown,
            alignment: Alignment.centerLeft,
            child: Text.rich(
              TextSpan(
                text: value,
                style: Theme.of(context).textTheme.headlineSmall?.copyWith(
                  fontWeight: FontWeight.w700,
                ),
                children: [
                  if (unit.isNotEmpty)
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

class _BalanceBar extends StatelessWidget {
  const _BalanceBar({required this.remaining, required this.total});

  final double remaining;
  final double total;

  @override
  Widget build(BuildContext context) {
    final progress = total <= 0 ? 0.0 : (remaining / total).clamp(0.0, 1.0);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Text('剩余额度', style: Theme.of(context).textTheme.bodySmall),
            const Spacer(),
            Text(
              remaining.toStringAsFixed(2),
              style: Theme.of(
                context,
              ).textTheme.titleMedium?.copyWith(fontWeight: FontWeight.w700),
            ),
          ],
        ),
        const SizedBox(height: 8),
        ClipRRect(
          borderRadius: BorderRadius.circular(99),
          child: TweenAnimationBuilder<double>(
            tween: Tween(begin: 0, end: progress),
            duration: const Duration(milliseconds: 520),
            curve: Curves.easeOutCubic,
            builder: (context, value, _) => LinearProgressIndicator(
              value: value,
              minHeight: 10,
              backgroundColor: Theme.of(context).colorScheme.surfaceContainer,
            ),
          ),
        ),
      ],
    );
  }
}

class _StatusPill extends StatelessWidget {
  const _StatusPill({required this.label, required this.isActive});

  final String label;
  final bool isActive;

  @override
  Widget build(BuildContext context) {
    final color = isActive
        ? const Color(0xFF2E7D5B)
        : Theme.of(context).colorScheme.onSurfaceVariant;
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

class _SectionHeader extends StatelessWidget {
  const _SectionHeader({
    required this.title,
    required this.actionLabel,
    required this.onAction,
  });

  final String title;
  final String actionLabel;
  final VoidCallback onAction;

  @override
  Widget build(BuildContext context) {
    return Row(
      children: [
        Text(title, style: Theme.of(context).textTheme.titleMedium),
        const Spacer(),
        TextButton(onPressed: onAction, child: Text(actionLabel)),
      ],
    );
  }
}

class _RecentDeviceTile extends StatelessWidget {
  const _RecentDeviceTile({required this.device, required this.onTap});

  final BoundDevice device;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final isOnline = device.runtime?.isOnline ?? false;
    return Card(
      child: ListTile(
        onTap: onTap,
        leading: Icon(
          Icons.toys_rounded,
          color: isOnline
              ? const Color(0xFF2E7D5B)
              : Theme.of(context).colorScheme.onSurfaceVariant,
        ),
        title: Text(device.deviceName),
        subtitle: Text(isOnline ? '在线 · 随时可以开始陪伴' : '离线 · 设备联网后会自动同步'),
        trailing: const Icon(Icons.chevron_right_rounded),
      ),
    );
  }
}

class _DeviceLoadingCard extends StatelessWidget {
  const _DeviceLoadingCard();

  @override
  Widget build(BuildContext context) {
    return const Card(
      child: ListTile(
        leading: SizedBox(
          width: 20,
          height: 20,
          child: CircularProgressIndicator(strokeWidth: 2),
        ),
        title: Text('正在读取设备…'),
      ),
    );
  }
}

class _EmptyDeviceCard extends StatelessWidget {
  const _EmptyDeviceCard({required this.onAdd});

  final VoidCallback onAdd;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          children: [
            Icon(
              Icons.toys_outlined,
              size: 40,
              color: Theme.of(context).colorScheme.primary,
            ),
            const SizedBox(height: 12),
            Text('还没有绑定设备', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 6),
            const Text('添加初芽后，孩子就可以开始对话了。', textAlign: TextAlign.center),
            const SizedBox(height: 14),
            FilledButton.icon(
              onPressed: onAdd,
              icon: const Icon(Icons.add_rounded),
              label: const Text('添加设备'),
            ),
          ],
        ),
      ),
    );
  }
}

class _InlineError extends StatelessWidget {
  const _InlineError({required this.message, required this.onRetry});

  final String message;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: ListTile(
        leading: const Icon(Icons.info_outline_rounded),
        title: Text(message),
        trailing: TextButton(onPressed: onRetry, child: const Text('重试')),
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
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 12),
            FilledButton(onPressed: onRetry, child: const Text('重新加载')),
          ],
        ),
      ),
    );
  }
}

class _RestoreErrorState extends StatelessWidget {
  const _RestoreErrorState({
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
            const Icon(Icons.cloud_off_outlined, size: 40),
            const SizedBox(height: 12),
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 16),
            FilledButton(onPressed: onRetry, child: const Text('重新恢复登录')),
          ],
        ),
      ),
    );
  }
}

class _SignInPrompt extends StatelessWidget {
  const _SignInPrompt({super.key});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.lock_outline_rounded, size: 40),
            const SizedBox(height: 12),
            const Text('请先登录家长账号'),
            const SizedBox(height: 16),
            FilledButton(
              onPressed: () => context.go('/login'),
              child: const Text('去登录'),
            ),
          ],
        ),
      ),
    );
  }
}

String _overviewErrorMessage(Object? error) {
  if (error is AppException) {
    return error.message;
  }
  return '网络连接不稳定，请稍后重试';
}

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/offline_policy_controller.dart';
import '../domain/offline_policy.dart';

/// Guardian view of the platform-wide offline download policy.
///
/// The policy caps how much content a device keeps for offline use and how
/// long a cached item stays valid. It applies to every bound device, so there
/// is one published revision rather than a per-device setting.
class OfflinePolicyPage extends ConsumerWidget {
  const OfflinePolicyPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final policy = ref.watch(offlinePolicyControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('离线下载'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: policy.when(
          data: (_) => 'offline-policy-data',
          error: (_, _) => 'offline-policy-error',
          loading: () => 'offline-policy-loading',
        ),
        child: policy.when(
          loading: () => const Center(
            key: ValueKey<String>('offline-policy-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ErrorState(
            key: const ValueKey<String>('offline-policy-error'),
            message: OfflinePolicyController.messageFor(error),
            onRetry: () =>
                ref.read(offlinePolicyControllerProvider.notifier).refresh(),
          ),
          data: (value) => RefreshIndicator(
            key: const ValueKey<String>('offline-policy-data'),
            onRefresh: () =>
                ref.read(offlinePolicyControllerProvider.notifier).refresh(),
            child: ListView(
              padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
              children: [
                AppReveal(child: _SummaryCard(policy: value)),
                const SizedBox(height: 16),
                AppReveal(
                  delay: const Duration(milliseconds: 60),
                  child: _DetailCard(policy: value),
                ),
                const SizedBox(height: 16),
                AppReveal(
                  delay: const Duration(milliseconds: 120),
                  child: _EditEntryCard(
                    onEdit: () => _openEditor(context, ref, value),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _SummaryCard extends StatelessWidget {
  const _SummaryCard({required this.policy});

  final OfflinePolicy policy;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Icon(
                  policy.offlineEnabled
                      ? Icons.cloud_off_rounded
                      : Icons.cloud_queue_rounded,
                  color: theme.colorScheme.primary,
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    policy.offlineEnabled ? '已开启离线播放' : '离线播放已关闭',
                    style: theme.textTheme.titleMedium,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            Text(
              policy.offlineEnabled
                  ? '断网后设备可以继续播放已经缓存的内容。'
                  : '设备在断网时不会播放缓存内容。',
              style: theme.textTheme.bodyMedium,
            ),
            const SizedBox(height: 12),
            Text(
              '当前策略版本 ${policy.revision}',
              style: theme.textTheme.bodySmall,
            ),
          ],
        ),
      ),
    );
  }
}

class _DetailCard extends StatelessWidget {
  const _DetailCard({required this.policy});

  final OfflinePolicy policy;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _DetailRow(
              icon: Icons.inventory_2_outlined,
              label: '最多缓存内容',
              value: '${policy.maxCachedPackages} 个',
            ),
            const Divider(height: 24),
            _DetailRow(
              icon: Icons.sd_storage_outlined,
              label: '缓存空间上限',
              value: '${policy.maxCacheMegabytes} MB',
            ),
            const Divider(height: 24),
            _DetailRow(
              icon: Icons.schedule_outlined,
              label: '缓存有效期',
              value: '${policy.cacheTtlHours} 小时',
            ),
            const Divider(height: 24),
            _DetailRow(
              icon: Icons.wifi_outlined,
              label: '下载网络',
              value: policy.downloadOverWifiOnly ? '仅 Wi-Fi' : 'Wi-Fi 与移动网络',
            ),
          ],
        ),
      ),
    );
  }
}

class _DetailRow extends StatelessWidget {
  const _DetailRow({
    required this.icon,
    required this.label,
    required this.value,
  });

  final IconData icon;
  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Row(
      children: [
        Icon(icon, size: 20, color: theme.colorScheme.onSurfaceVariant),
        const SizedBox(width: 12),
        Expanded(child: Text(label, style: theme.textTheme.bodyMedium)),
        Text(value, style: theme.textTheme.titleSmall),
      ],
    );
  }
}

class _EditEntryCard extends StatelessWidget {
  const _EditEntryCard({required this.onEdit});

  final VoidCallback onEdit;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: ListTile(
        leading: const Icon(Icons.tune_rounded),
        title: const Text('调整离线策略'),
        subtitle: const Text('修改后所有设备会同步新的缓存上限'),
        trailing: const Icon(Icons.chevron_right_rounded),
        onTap: onEdit,
      ),
    );
  }
}

class _ErrorState extends StatelessWidget {
  const _ErrorState({super.key, required this.message, required this.onRetry});

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
            const SizedBox(height: 16),
            FilledButton(onPressed: onRetry, child: const Text('重新加载')),
          ],
        ),
      ),
    );
  }
}

Future<void> _openEditor(
  BuildContext context,
  WidgetRef ref,
  OfflinePolicy current,
) async {
  final updated = await showModalBottomSheet<OfflinePolicy>(
    context: context,
    isScrollControlled: true,
    builder: (context) => _OfflinePolicyEditor(initial: current),
  );
  if (updated == null || !context.mounted) {
    return;
  }
  final messenger = ScaffoldMessenger.of(context);
  try {
    await ref.read(offlinePolicyControllerProvider.notifier).publish(updated);
    messenger.showSnackBar(
      const SnackBar(content: Text('离线策略已更新')),
    );
  } catch (error) {
    messenger.showSnackBar(
      SnackBar(content: Text(OfflinePolicyController.messageFor(error))),
    );
  }
}

void _leave(BuildContext context) {
  if (context.canPop()) {
    context.pop();
  } else {
    context.go('/me');
  }
}

/// Bottom sheet that edits one policy revision.
class _OfflinePolicyEditor extends StatefulWidget {
  const _OfflinePolicyEditor({required this.initial});

  final OfflinePolicy initial;

  @override
  State<_OfflinePolicyEditor> createState() => _OfflinePolicyEditorState();
}

class _OfflinePolicyEditorState extends State<_OfflinePolicyEditor> {
  late bool _offlineEnabled;
  late bool _downloadOverWifiOnly;
  late int _maxCachedPackages;
  late int _maxCacheMegabytes;
  late int _cacheTtlHours;
  final _formKey = GlobalKey<FormState>();

  @override
  void initState() {
    super.initState();
    _offlineEnabled = widget.initial.offlineEnabled;
    _downloadOverWifiOnly = widget.initial.downloadOverWifiOnly;
    _maxCachedPackages = widget.initial.maxCachedPackages;
    _maxCacheMegabytes = widget.initial.maxCacheMegabytes;
    _cacheTtlHours = widget.initial.cacheTtlHours;
  }

  void _submit() {
    if (!(_formKey.currentState?.validate() ?? false)) {
      return;
    }
    final next = OfflinePolicy(
      offlineEnabled: _offlineEnabled,
      maxCachedPackages: _maxCachedPackages,
      maxCacheBytes: _maxCacheMegabytes * 1024 * 1024,
      cacheTtlHours: _cacheTtlHours,
      downloadOverWifiOnly: _downloadOverWifiOnly,
      revision: widget.initial.revision,
    );
    Navigator.of(context).pop(next);
  }

  @override
  Widget build(BuildContext context) {
    final bottomInset = MediaQuery.of(context).viewInsets.bottom;
    return Padding(
      padding: EdgeInsets.fromLTRB(20, 20, 20, 20 + bottomInset),
      child: Form(
        key: _formKey,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                '调整离线策略',
                style: Theme.of(context).textTheme.titleLarge,
              ),
              const SizedBox(height: 16),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('允许离线播放'),
                subtitle: const Text('设备断网后继续播放缓存内容'),
                value: _offlineEnabled,
                onChanged: (value) => setState(() => _offlineEnabled = value),
              ),
              SwitchListTile(
                contentPadding: EdgeInsets.zero,
                title: const Text('仅 Wi-Fi 下载'),
                subtitle: const Text('关闭后允许使用移动网络下载'),
                value: _downloadOverWifiOnly,
                onChanged: (value) =>
                    setState(() => _downloadOverWifiOnly = value),
              ),
              const SizedBox(height: 8),
              TextFormField(
                initialValue: '$_maxCachedPackages',
                decoration: const InputDecoration(
                  labelText: '最多缓存内容数',
                  suffixText: '个',
                ),
                keyboardType: TextInputType.number,
                validator: (value) => _validateRange(value, 1, 200, '缓存内容数'),
                onSaved: (value) =>
                    _maxCachedPackages = int.tryParse(value ?? '') ?? 0,
              ),
              const SizedBox(height: 12),
              TextFormField(
                initialValue: '$_maxCacheMegabytes',
                decoration: const InputDecoration(
                  labelText: '缓存空间上限',
                  suffixText: 'MB',
                ),
                keyboardType: TextInputType.number,
                validator: (value) =>
                    _validateRange(value, 16, 8192, '缓存空间'),
                onSaved: (value) =>
                    _maxCacheMegabytes = int.tryParse(value ?? '') ?? 0,
              ),
              const SizedBox(height: 12),
              TextFormField(
                initialValue: '$_cacheTtlHours',
                decoration: const InputDecoration(
                  labelText: '缓存有效期',
                  suffixText: '小时',
                ),
                keyboardType: TextInputType.number,
                validator: (value) =>
                    _validateRange(value, 1, 8760, '缓存有效期'),
                onSaved: (value) =>
                    _cacheTtlHours = int.tryParse(value ?? '') ?? 0,
              ),
              const SizedBox(height: 20),
              Row(
                children: [
                  Expanded(
                    child: OutlinedButton(
                      onPressed: () => Navigator.of(context).pop(),
                      child: const Text('取消'),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: FilledButton(
                      onPressed: () {
                        _formKey.currentState?.save();
                        _submit();
                      },
                      child: const Text('保存'),
                    ),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

String? _validateRange(
  String? value,
  int minimum,
  int maximum,
  String label,
) {
  final parsed = int.tryParse((value ?? '').trim());
  if (parsed == null) {
    return '请输入$label';
  }
  if (parsed < minimum || parsed > maximum) {
    return '$label需要在 $minimum 到 $maximum 之间';
  }
  return null;
}

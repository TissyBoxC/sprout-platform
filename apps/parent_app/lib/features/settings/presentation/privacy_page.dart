import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/privacy_controller.dart';
import '../application/privacy_export_store.dart';
import '../domain/privacy_state.dart';

/// Guardian controls for consent, data export, retention, and account deletion.
class PrivacyPage extends ConsumerStatefulWidget {
  const PrivacyPage({super.key});

  @override
  ConsumerState<PrivacyPage> createState() => _PrivacyPageState();
}

class _PrivacyPageState extends ConsumerState<PrivacyPage> {
  PrivacyExportFile? _exportFile;
  bool _isExporting = false;
  bool _isOpeningExport = false;
  bool _isSharingExport = false;
  bool _isUpdatingDeletion = false;
  bool _isWithdrawing = false;
  bool _isReauthorizing = false;
  String? _operationMessage;

  Future<void> _refresh() async {
    try {
      await ref.read(privacyControllerProvider.notifier).refresh();
    } on Object catch (error) {
      if (mounted) {
        _showMessage(privacyErrorMessage(error));
      }
    }
  }

  Future<void> _exportData() async {
    if (_isExporting) {
      return;
    }
    setState(() {
      _isExporting = true;
      _operationMessage = null;
    });
    try {
      final file = await ref
          .read(privacyControllerProvider.notifier)
          .exportData();
      if (!mounted) {
        return;
      }
      setState(() => _exportFile = file);
      _showMessage('数据副本已保存在本机');
    } on Object catch (error) {
      if (mounted) {
        setState(() => _operationMessage = privacyErrorMessage(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isExporting = false);
      }
    }
  }

  Future<void> _openExportFile() async {
    final file = _exportFile;
    if (file == null || _isOpeningExport) {
      return;
    }
    setState(() => _isOpeningExport = true);
    try {
      final result = await ref.read(privacyExportOpenerProvider).open(file);
      if (!mounted || result == PrivacyExportOpenResult.opened) {
        return;
      }
      _showMessage(
        result == PrivacyExportOpenResult.unavailable
            ? '本机暂时没有可打开这个文件的应用，请使用“保存或分享”'
            : '文件暂时无法打开，请重新生成后再试',
      );
    } on Object {
      if (mounted) {
        _showMessage('文件暂时无法打开，请重新生成后再试');
      }
    } finally {
      if (mounted) {
        setState(() => _isOpeningExport = false);
      }
    }
  }

  Future<void> _shareExportFile() async {
    final file = _exportFile;
    if (file == null || _isSharingExport) {
      return;
    }
    setState(() {
      _isSharingExport = true;
      _operationMessage = null;
    });
    try {
      final outcome = await ref.read(privacyExportOpenerProvider).share(file);
      if (!mounted || outcome.wasDismissed) {
        return;
      }
      if (outcome.didShare) {
        _showMessage('数据副本已交给所选应用');
        return;
      }
      _showMessage(outcome.canRetry ? '暂时无法分享，文件已经保存在本机，可以稍后重试' : '文件已经保存在本机');
    } on Object {
      if (mounted) {
        _showMessage('暂时无法分享文件，请稍后重试');
      }
    } finally {
      if (mounted) {
        setState(() => _isSharingExport = false);
      }
    }
  }

  Future<void> _requestDeletion() async {
    final confirmed = await _showTypedConfirmation(
      context,
      title: '申请注销账号',
      description:
          '注销后，儿童资料、设备绑定、AI 陪伴记录和剩余额度将进入删除流程。'
          '等待期内可以取消；等待期结束后无法恢复。',
      confirmationLabel: '请输入“注销”继续',
      expectedText: '注销',
      actionLabel: '申请注销',
      isDangerous: true,
    );
    if (confirmed != true || !mounted) {
      return;
    }
    setState(() {
      _isUpdatingDeletion = true;
      _operationMessage = null;
    });
    try {
      final deletion = await ref
          .read(privacyControllerProvider.notifier)
          .requestDeletion();
      if (!mounted) {
        return;
      }
      if (deletion.isCompleted) {
        await _finishSession();
        return;
      }
      _showMessage('注销申请已提交，等待期内可以取消');
    } on Object catch (error) {
      if (mounted) {
        setState(() => _operationMessage = privacyErrorMessage(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isUpdatingDeletion = false);
      }
    }
  }

  Future<void> _cancelDeletion() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('取消注销账号'),
        content: const Text('取消后账号和数据会继续保留，设备绑定不会受到影响。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('继续注销'),
          ),
          FilledButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('取消注销'),
          ),
        ],
      ),
    );
    if (confirmed != true || !mounted) {
      return;
    }
    setState(() {
      _isUpdatingDeletion = true;
      _operationMessage = null;
    });
    try {
      await ref.read(privacyControllerProvider.notifier).cancelDeletion();
      if (mounted) {
        _showMessage('注销申请已取消');
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() => _operationMessage = privacyErrorMessage(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isUpdatingDeletion = false);
      }
    }
  }

  Future<void> _withdrawConsent() async {
    final confirmed = await _showTypedConfirmation(
      context,
      title: '撤回授权并结束会话',
      description:
          '撤回后 AI 陪伴服务会停止，当前登录也会结束。'
          '重新登录并授权后可以继续使用。',
      confirmationLabel: '请输入“撤回”继续',
      expectedText: '撤回',
      actionLabel: '撤回授权',
      isDangerous: true,
    );
    if (confirmed != true || !mounted) {
      return;
    }
    setState(() {
      _isWithdrawing = true;
      _operationMessage = null;
    });
    try {
      await ref.read(privacyControllerProvider.notifier).withdrawConsent();
      if (!mounted) {
        return;
      }
      await _finishSession();
    } on Object catch (error) {
      if (mounted) {
        setState(() => _operationMessage = privacyErrorMessage(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isWithdrawing = false);
      }
    }
  }

  Future<void> _reauthorizeConsent() async {
    if (_isReauthorizing) {
      return;
    }
    final route = ref
        .read(privacyControllerProvider)
        .value
        ?.consent
        .reconsentRoute;
    if (route == null) {
      _showMessage('暂时无法重新授权，请退出登录后再试');
      return;
    }
    setState(() {
      _isReauthorizing = true;
      _operationMessage = null;
    });
    try {
      await context.push<void>(route);
      if (!mounted) {
        return;
      }
      await ref.read(privacyControllerProvider.notifier).refresh();
      final isActive =
          ref.read(privacyControllerProvider).value?.consent.isActive ?? false;
      if (mounted) {
        _showMessage(isActive ? '家长授权已恢复' : '请按页面提示完成重新授权');
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() => _operationMessage = privacyErrorMessage(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isReauthorizing = false);
      }
    }
  }

  Future<void> _finishSession() async {
    await ref.read(privacySessionCloserProvider)();
    if (mounted) {
      context.go('/login');
    }
  }

  void _showMessage(String message) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(message)));
  }

  @override
  Widget build(BuildContext context) {
    final privacy = ref.watch(privacyControllerProvider);
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) {
          _leave(context);
        }
      },
      child: Scaffold(
        appBar: AppBar(
          title: const Text('隐私与数据'),
          leading: IconButton(
            tooltip: '返回',
            onPressed: () => _leave(context),
            icon: const Icon(Icons.arrow_back_rounded),
          ),
          actions: [
            IconButton(
              tooltip: '重新加载',
              onPressed: privacy.isLoading ? null : _refresh,
              icon: const Icon(Icons.refresh_rounded),
            ),
          ],
        ),
        body: AppStateSwitcher(
          stateKey: privacy.when(
            data: (_) => 'privacy-content',
            error: (_, _) => 'privacy-error',
            loading: () => 'privacy-loading',
          ),
          child: privacy.when(
            loading: () => const Center(
              key: ValueKey<String>('privacy-loading'),
              child: CircularProgressIndicator(),
            ),
            error: (error, _) => _PrivacyErrorState(
              key: const ValueKey<String>('privacy-error'),
              message: privacyErrorMessage(error),
              onRetry: _refresh,
            ),
            data: (status) => _PrivacyContent(
              key: const ValueKey<String>('privacy-content'),
              status: status,
              exportFile: _exportFile,
              isExporting: _isExporting,
              isOpeningExport: _isOpeningExport,
              isSharingExport: _isSharingExport,
              isUpdatingDeletion: _isUpdatingDeletion,
              isWithdrawing: _isWithdrawing,
              isReauthorizing: _isReauthorizing,
              operationMessage: _operationMessage,
              onExport: _exportData,
              onOpenExport: _openExportFile,
              onShareExport: _shareExportFile,
              onRequestDeletion: _requestDeletion,
              onCancelDeletion: _cancelDeletion,
              onWithdrawConsent: _withdrawConsent,
              onReauthorizeConsent: _reauthorizeConsent,
              onRefresh: _refresh,
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
    context.go('/me');
  }
}

class _PrivacyContent extends StatelessWidget {
  const _PrivacyContent({
    required this.status,
    required this.exportFile,
    required this.isExporting,
    required this.isOpeningExport,
    required this.isSharingExport,
    required this.isUpdatingDeletion,
    required this.isWithdrawing,
    required this.isReauthorizing,
    required this.operationMessage,
    required this.onExport,
    required this.onOpenExport,
    required this.onShareExport,
    required this.onRequestDeletion,
    required this.onCancelDeletion,
    required this.onWithdrawConsent,
    required this.onReauthorizeConsent,
    required this.onRefresh,
    super.key,
  });

  final PrivacyStatus status;
  final PrivacyExportFile? exportFile;
  final bool isExporting;
  final bool isOpeningExport;
  final bool isSharingExport;
  final bool isUpdatingDeletion;
  final bool isWithdrawing;
  final bool isReauthorizing;
  final String? operationMessage;
  final Future<void> Function() onExport;
  final Future<void> Function() onOpenExport;
  final Future<void> Function() onShareExport;
  final Future<void> Function() onRequestDeletion;
  final Future<void> Function() onCancelDeletion;
  final Future<void> Function() onWithdrawConsent;
  final Future<void> Function() onReauthorizeConsent;
  final Future<void> Function() onRefresh;

  @override
  Widget build(BuildContext context) {
    return RefreshIndicator(
      onRefresh: onRefresh,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: [
          AppReveal(
            child: _ConsentCard(
              consent: status.consent,
              isWithdrawing: isWithdrawing,
              isReauthorizing: isReauthorizing,
              onWithdraw: onWithdrawConsent,
              onReauthorize: onReauthorizeConsent,
            ),
          ),
          const SizedBox(height: 16),
          AppReveal(
            delay: const Duration(milliseconds: 55),
            child: _ExportCard(
              available: status.exportAvailable,
              isExporting: isExporting,
              isOpeningExport: isOpeningExport,
              isSharingExport: isSharingExport,
              exportFile: exportFile,
              onExport: onExport,
              onOpenExport: onOpenExport,
              onShareExport: onShareExport,
            ),
          ),
          const SizedBox(height: 16),
          AppReveal(
            delay: const Duration(milliseconds: 110),
            child: _RetentionCard(policy: status.policy),
          ),
          const SizedBox(height: 16),
          AppReveal(
            delay: const Duration(milliseconds: 165),
            child: _DeletionCard(
              deletion: status.deletion,
              isUpdating: isUpdatingDeletion,
              onRequest: onRequestDeletion,
              onCancel: onCancelDeletion,
            ),
          ),
          AnimatedSize(
            duration: const Duration(milliseconds: 220),
            curve: Curves.easeOutCubic,
            alignment: Alignment.topCenter,
            child: operationMessage == null
                ? const SizedBox(width: double.infinity)
                : Padding(
                    padding: const EdgeInsets.only(top: 16),
                    child: Card(
                      color: Theme.of(context).colorScheme.errorContainer,
                      child: ListTile(
                        leading: Icon(
                          Icons.error_outline_rounded,
                          color: Theme.of(context).colorScheme.onErrorContainer,
                        ),
                        title: Text(
                          operationMessage!,
                          style: TextStyle(
                            color: Theme.of(
                              context,
                            ).colorScheme.onErrorContainer,
                          ),
                        ),
                      ),
                    ),
                  ),
          ),
        ],
      ),
    );
  }
}

class _ConsentCard extends StatelessWidget {
  const _ConsentCard({
    required this.consent,
    required this.isWithdrawing,
    required this.isReauthorizing,
    required this.onWithdraw,
    required this.onReauthorize,
  });

  final GuardianConsent consent;
  final bool isWithdrawing;
  final bool isReauthorizing;
  final Future<void> Function() onWithdraw;
  final Future<void> Function() onReauthorize;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                _SectionIcon(
                  icon: consent.isActive
                      ? Icons.verified_user_outlined
                      : Icons.gpp_bad_outlined,
                  isWarning: !consent.isActive,
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        '家长授权',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      const SizedBox(height: 3),
                      Text(
                        consent.isActive ? '授权有效' : '授权已撤回',
                        style: Theme.of(context).textTheme.bodySmall?.copyWith(
                          color: consent.isActive
                              ? colorScheme.primary
                              : colorScheme.error,
                          fontWeight: FontWeight.w700,
                        ),
                      ),
                    ],
                  ),
                ),
              ],
            ),
            const SizedBox(height: 16),
            _InfoLine(
              label: '授权版本',
              value: consent.version.isEmpty ? '未记录' : consent.version,
            ),
            _InfoLine(
              label: '授权时间',
              value: _dateTimeLabel(consent.consentedAt),
            ),
            if (consent.withdrawnAt != null)
              _InfoLine(
                label: '撤回时间',
                value: _dateTimeLabel(consent.withdrawnAt),
              ),
            const SizedBox(height: 14),
            if (consent.isActive)
              OutlinedButton.icon(
                onPressed: isWithdrawing ? null : onWithdraw,
                icon: isWithdrawing
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.logout_rounded),
                label: Text(isWithdrawing ? '正在撤回…' : '撤回授权并结束会话'),
                style: OutlinedButton.styleFrom(
                  foregroundColor: colorScheme.error,
                  side: BorderSide(color: colorScheme.error),
                ),
              )
            else
              Container(
                width: double.infinity,
                padding: const EdgeInsets.all(14),
                decoration: BoxDecoration(
                  color: colorScheme.errorContainer,
                  borderRadius: BorderRadius.circular(16),
                ),
                child: Text(
                  consent.canReauthorize
                      ? '当前授权已经撤回。重新确认家长授权后，才能继续使用 AI 陪伴服务。'
                      : '当前授权已经撤回。请退出登录后重新进入授权流程。',
                  style: TextStyle(color: colorScheme.onErrorContainer),
                ),
              ),
            if (!consent.isActive) ...[
              const SizedBox(height: 12),
              FilledButton.icon(
                onPressed: isReauthorizing || !consent.canReauthorize
                    ? null
                    : onReauthorize,
                icon: isReauthorizing
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.verified_user_outlined),
                label: Text(isReauthorizing ? '正在打开…' : '重新确认授权'),
              ),
            ],
          ],
        ),
      ),
    );
  }
}

class _ExportCard extends StatelessWidget {
  const _ExportCard({
    required this.available,
    required this.isExporting,
    required this.isOpeningExport,
    required this.isSharingExport,
    required this.exportFile,
    required this.onExport,
    required this.onOpenExport,
    required this.onShareExport,
  });

  final bool available;
  final bool isExporting;
  final bool isOpeningExport;
  final bool isSharingExport;
  final PrivacyExportFile? exportFile;
  final Future<void> Function() onExport;
  final Future<void> Function() onOpenExport;
  final Future<void> Function() onShareExport;

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
                const _SectionIcon(icon: Icons.file_download_outlined),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        '获取数据副本',
                        style: Theme.of(context).textTheme.titleMedium,
                      ),
                      const SizedBox(height: 3),
                      const Text('包含账号、儿童资料、设备和授权记录，不含密码和密钥。'),
                    ],
                  ),
                ),
              ],
            ),
            const SizedBox(height: 16),
            if (exportFile == null)
              Text(
                available ? '副本会保存到本机，可随时重新获取。' : '当前暂时不能获取数据副本。',
                style: Theme.of(context).textTheme.bodyMedium,
              )
            else
              Container(
                width: double.infinity,
                padding: const EdgeInsets.all(14),
                decoration: BoxDecoration(
                  color: Theme.of(context).colorScheme.primaryContainer,
                  borderRadius: BorderRadius.circular(16),
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text('最近导出的副本'),
                    const SizedBox(height: 4),
                    Text(
                      exportFile!.fileName,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: Theme.of(context).textTheme.titleSmall,
                    ),
                    const SizedBox(height: 4),
                    Text(
                      '${_dateTimeLabel(exportFile!.exportedAt)} · '
                      '${_fileSizeLabel(exportFile!.sizeBytes)}',
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                  ],
                ),
              ),
            const SizedBox(height: 14),
            Wrap(
              spacing: 10,
              runSpacing: 10,
              children: [
                FilledButton.icon(
                  onPressed: !available || isExporting ? null : onExport,
                  icon: isExporting
                      ? const SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.download_rounded),
                  label: Text(isExporting ? '正在生成…' : '生成数据副本'),
                ),
                if (exportFile != null) ...[
                  OutlinedButton.icon(
                    onPressed: isOpeningExport ? null : onOpenExport,
                    icon: isOpeningExport
                        ? const SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Icon(Icons.open_in_new_rounded),
                    label: Text(isOpeningExport ? '正在打开…' : '打开副本'),
                  ),
                  FilledButton.tonalIcon(
                    onPressed: isSharingExport ? null : onShareExport,
                    icon: isSharingExport
                        ? const SizedBox(
                            width: 18,
                            height: 18,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Icon(Icons.ios_share_rounded),
                    label: Text(isSharingExport ? '正在准备…' : '保存或分享'),
                  ),
                ],
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class _RetentionCard extends StatelessWidget {
  const _RetentionCard({required this.policy});

  final PrivacyRetentionPolicy policy;

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
                const _SectionIcon(icon: Icons.hourglass_bottom_rounded),
                const SizedBox(width: 12),
                Text('数据保留说明', style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 16),
            _RetentionRow(
              icon: Icons.mic_none_rounded,
              label: '录音',
              value: _retentionLabel(policy.audioRetentionDays),
            ),
            _RetentionRow(
              icon: Icons.image_outlined,
              label: '图片',
              value: _retentionLabel(policy.imageRetentionDays),
            ),
            _RetentionRow(
              icon: Icons.forum_outlined,
              label: '对话记录',
              value: _retentionLabel(policy.conversationRetentionDays),
            ),
            const SizedBox(height: 12),
            Text(
              '达到保留期限后，平台会自动清理这些数据。',
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ),
      ),
    );
  }
}

class _RetentionRow extends StatelessWidget {
  const _RetentionRow({
    required this.icon,
    required this.label,
    required this.value,
  });

  final IconData icon;
  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: Row(
        children: [
          Icon(icon, size: 21, color: Theme.of(context).colorScheme.primary),
          const SizedBox(width: 12),
          Expanded(child: Text(label)),
          Text(
            value,
            style: Theme.of(
              context,
            ).textTheme.titleSmall?.copyWith(fontWeight: FontWeight.w700),
          ),
        ],
      ),
    );
  }
}

class _DeletionCard extends StatelessWidget {
  const _DeletionCard({
    required this.deletion,
    required this.isUpdating,
    required this.onRequest,
    required this.onCancel,
  });

  final PrivacyDeletion deletion;
  final bool isUpdating;
  final Future<void> Function() onRequest;
  final Future<void> Function() onCancel;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                _SectionIcon(
                  icon: Icons.delete_forever_outlined,
                  isWarning: true,
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    '注销账号',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            if (deletion.isNone)
              const Text(
                '注销会删除儿童资料、设备绑定、AI 陪伴记录和剩余额度。'
                '提交后会有一段等待期，等待期内可以取消。',
              )
            else if (deletion.isPending) ...[
              Text(
                '注销申请已提交',
                style: Theme.of(context).textTheme.titleSmall?.copyWith(
                  color: colorScheme.error,
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(height: 10),
              _InfoLine(
                label: '提交时间',
                value: _dateTimeLabel(deletion.requestedAt),
              ),
              _InfoLine(
                label: '预计完成',
                value: _dateTimeLabel(deletion.scheduledFor),
              ),
              const SizedBox(height: 6),
              _DeletionCountdown(scheduledFor: deletion.scheduledFor),
            ] else ...[
              Text(
                '账号已进入完成状态',
                style: Theme.of(context).textTheme.titleSmall?.copyWith(
                  color: colorScheme.error,
                  fontWeight: FontWeight.w700,
                ),
              ),
              const SizedBox(height: 6),
              Text('完成时间：${_dateTimeLabel(deletion.completedAt)}'),
            ],
            const SizedBox(height: 16),
            if (deletion.isNone)
              FilledButton.icon(
                onPressed: isUpdating ? null : onRequest,
                icon: isUpdating
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.delete_outline_rounded),
                label: Text(isUpdating ? '正在提交…' : '申请注销账号'),
                style: FilledButton.styleFrom(
                  backgroundColor: colorScheme.error,
                  foregroundColor: colorScheme.onError,
                ),
              )
            else if (deletion.isPending && deletion.cancellable)
              OutlinedButton.icon(
                onPressed: isUpdating ? null : onCancel,
                icon: isUpdating
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.undo_rounded),
                label: Text(isUpdating ? '正在取消…' : '取消注销'),
              ),
          ],
        ),
      ),
    );
  }
}

class _DeletionCountdown extends StatefulWidget {
  const _DeletionCountdown({required this.scheduledFor});

  final DateTime? scheduledFor;

  @override
  State<_DeletionCountdown> createState() => _DeletionCountdownState();
}

class _DeletionCountdownState extends State<_DeletionCountdown> {
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    _timer = Timer.periodic(const Duration(minutes: 1), (_) {
      if (mounted) {
        setState(() {});
      }
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.errorContainer,
        borderRadius: BorderRadius.circular(16),
      ),
      child: Text(
        _remainingLabel(widget.scheduledFor),
        style: TextStyle(
          color: Theme.of(context).colorScheme.onErrorContainer,
          fontWeight: FontWeight.w700,
        ),
      ),
    );
  }
}

class _InfoLine extends StatelessWidget {
  const _InfoLine({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 76,
            child: Text(label, style: Theme.of(context).textTheme.bodySmall),
          ),
          const SizedBox(width: 10),
          Expanded(child: Text(value)),
        ],
      ),
    );
  }
}

class _SectionIcon extends StatelessWidget {
  const _SectionIcon({required this.icon, this.isWarning = false});

  final IconData icon;
  final bool isWarning;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return Container(
      width: 44,
      height: 44,
      decoration: BoxDecoration(
        color: isWarning
            ? colorScheme.errorContainer
            : colorScheme.primaryContainer,
        borderRadius: BorderRadius.circular(15),
      ),
      child: Icon(
        icon,
        color: isWarning ? colorScheme.onErrorContainer : colorScheme.primary,
      ),
    );
  }
}

class _PrivacyErrorState extends StatelessWidget {
  const _PrivacyErrorState({
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
        padding: const EdgeInsets.all(28),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.cloud_off_outlined,
              size: 46,
              color: Theme.of(context).colorScheme.primary,
            ),
            const SizedBox(height: 14),
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 8),
            const Text('设备上的登录状态不会受到影响，可以稍后重新加载。', textAlign: TextAlign.center),
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

Future<bool?> _showTypedConfirmation(
  BuildContext context, {
  required String title,
  required String description,
  required String confirmationLabel,
  required String expectedText,
  required String actionLabel,
  required bool isDangerous,
}) {
  return showDialog<bool>(
    context: context,
    builder: (context) => _TypedConfirmationDialog(
      title: title,
      description: description,
      confirmationLabel: confirmationLabel,
      expectedText: expectedText,
      actionLabel: actionLabel,
      isDangerous: isDangerous,
    ),
  );
}

class _TypedConfirmationDialog extends StatefulWidget {
  const _TypedConfirmationDialog({
    required this.title,
    required this.description,
    required this.confirmationLabel,
    required this.expectedText,
    required this.actionLabel,
    required this.isDangerous,
  });

  final String title;
  final String description;
  final String confirmationLabel;
  final String expectedText;
  final String actionLabel;
  final bool isDangerous;

  @override
  State<_TypedConfirmationDialog> createState() =>
      _TypedConfirmationDialogState();
}

class _TypedConfirmationDialogState extends State<_TypedConfirmationDialog> {
  final _controller = TextEditingController();
  bool _matches = false;

  @override
  void initState() {
    super.initState();
    _controller.addListener(_onChanged);
  }

  @override
  void dispose() {
    _controller
      ..removeListener(_onChanged)
      ..dispose();
    super.dispose();
  }

  void _onChanged() {
    final matches = _controller.text.trim() == widget.expectedText;
    if (matches != _matches) {
      setState(() => _matches = matches);
    }
  }

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return AlertDialog(
      title: Text(widget.title),
      content: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(widget.description),
            const SizedBox(height: 16),
            TextField(
              controller: _controller,
              autofocus: true,
              decoration: InputDecoration(labelText: widget.confirmationLabel),
            ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('暂不操作'),
        ),
        FilledButton(
          onPressed: _matches ? () => Navigator.of(context).pop(true) : null,
          style: widget.isDangerous
              ? FilledButton.styleFrom(
                  backgroundColor: colorScheme.error,
                  foregroundColor: colorScheme.onError,
                )
              : null,
          child: Text(widget.actionLabel),
        ),
      ],
    );
  }
}

String _dateTimeLabel(DateTime? value) {
  if (value == null) {
    return '未记录';
  }
  final local = value.toLocal();
  return '${local.year}年${local.month}月${local.day}日 '
      '${_twoDigits(local.hour)}:${_twoDigits(local.minute)}';
}

String _remainingLabel(DateTime? scheduledFor) {
  if (scheduledFor == null) {
    return '完成时间待确认';
  }
  final remaining = scheduledFor.toLocal().difference(DateTime.now());
  if (remaining.isNegative) {
    return '等待期已结束，正在完成注销';
  }
  if (remaining.inHours >= 24) {
    final days = remaining.inDays;
    final hours = remaining.inHours % 24;
    return '距离注销完成还有 $days 天 $hours 小时';
  }
  if (remaining.inHours > 0) {
    final hours = remaining.inHours;
    final minutes = remaining.inMinutes % 60;
    return '距离注销完成还有 $hours 小时 $minutes 分';
  }
  return '距离注销完成还有 ${remaining.inMinutes + 1} 分钟';
}

String _retentionLabel(int days) {
  if (days <= 0) {
    return '不保留';
  }
  return '保留 $days 天';
}

String _fileSizeLabel(int bytes) {
  if (bytes < 1024) {
    return '$bytes 字节';
  }
  if (bytes < 1024 * 1024) {
    return '${(bytes / 1024).toStringAsFixed(1)} 千字节';
  }
  return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} 兆字节';
}

String _twoDigits(int value) => value.toString().padLeft(2, '0');

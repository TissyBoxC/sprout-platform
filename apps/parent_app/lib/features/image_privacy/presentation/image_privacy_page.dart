import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/image_privacy_controller.dart';
import '../domain/image_privacy.dart';

/// Guardian view of the family image-privacy policy and device status.
class ImagePrivacyPage extends ConsumerWidget {
  const ImagePrivacyPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(imagePrivacyControllerProvider);
    return Scaffold(
      appBar: AppBar(
        title: const Text('图片隐私'),
        leading: IconButton(
          tooltip: '返回',
          onPressed: () => _leave(context),
          icon: const Icon(Icons.arrow_back_rounded),
        ),
      ),
      body: AppStateSwitcher(
        stateKey: state.when(
          data: (value) =>
              value.isEmpty ? 'image-privacy-empty' : 'image-privacy-content',
          error: (_, _) => 'image-privacy-error',
          loading: () => 'image-privacy-loading',
        ),
        child: state.when(
          loading: () => const Center(
            key: ValueKey<String>('image-privacy-loading'),
            child: CircularProgressIndicator(),
          ),
          error: (error, _) => _ImagePrivacyErrorState(
            key: const ValueKey<String>('image-privacy-error'),
            message: ImagePrivacyController.messageFor(error),
            onRetry: () =>
                ref.read(imagePrivacyControllerProvider.notifier).refresh(),
          ),
          data: (value) => _ImagePrivacyContent(
            key: ValueKey<String>(
              value.isEmpty ? 'image-privacy-empty' : 'image-privacy-content',
            ),
            state: value,
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

class _ImagePrivacyContent extends ConsumerWidget {
  const _ImagePrivacyContent({required this.state, super.key});

  final ImagePrivacyState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final controller = ref.read(imagePrivacyControllerProvider.notifier);
    return RefreshIndicator(
      onRefresh: controller.refresh,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: [
          AppReveal(child: _PolicyCard(state: state)),
          const SizedBox(height: 20),
          AppReveal(child: _DeviceSection(state: state)),
        ],
      ),
    );
  }
}

class _PolicyCard extends ConsumerWidget {
  const _PolicyCard({required this.state});

  final ImagePrivacyState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final policy = state.policy;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                const Icon(Icons.privacy_tip_rounded),
                const SizedBox(width: 10),
                Text('图片隐私设置', style: Theme.of(context).textTheme.titleMedium),
                const Spacer(),
                Switch(
                  value: policy.enabled,
                  onChanged: (value) =>
                      _publish(context, ref, state, enabled: value),
                ),
              ],
            ),
            const SizedBox(height: 4),
            Text(
              policy.enabled
                  ? policy.localProcessingOnly
                        ? '已开启：图片只在本机处理，不会上传。'
                        : '已开启：图片会按已授权的范围处理。'
                  : '未开启：设备不会处理或上传图片。',
              style: Theme.of(context).textTheme.bodyMedium,
            ),
            const SizedBox(height: 12),
            _InfoRow(
              icon: Icons.photo_camera_outlined,
              label: '处理方式',
              value: imageProcessingModeLabel(policy.processingMode),
            ),
            _InfoRow(
              icon: Icons.cloud_upload_outlined,
              label: '上传范围',
              value: imageUploadScopeLabel(policy.uploadScope),
            ),
            _InfoRow(
              icon: Icons.blur_on_rounded,
              label: '脱敏级别',
              value: imageRedactionLevelLabel(policy.redactionLevel),
            ),
            _InfoRow(
              icon: Icons.schedule_rounded,
              label: '保留时长',
              value: policy.retentionDays == 0
                  ? '不留存'
                  : '${policy.retentionDays} 天',
            ),
            _InfoRow(
              icon: Icons.verified_user_outlined,
              label: '孩子授权',
              value: policy.requireChildConsent ? '需要' : '不需要',
            ),
            _InfoRow(
              icon: Icons.computer_rounded,
              label: '本机处理',
              value: policy.localProcessingOnly
                  ? '仅本机'
                  : policy.uploadAllowed
                  ? '可上传'
                  : '不可上传',
            ),
            const SizedBox(height: 12),
            Align(
              alignment: Alignment.centerRight,
              child: FilledButton.tonalIcon(
                onPressed: () => _openEditor(context, ref, state),
                icon: const Icon(Icons.tune_rounded),
                label: const Text('编辑设置'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _DeviceSection extends ConsumerWidget {
  const _DeviceSection({required this.state});

  final ImagePrivacyState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            const Icon(Icons.devices_other_rounded),
            const SizedBox(width: 10),
            Text('设备状态', style: Theme.of(context).textTheme.titleMedium),
            const Spacer(),
            Text(
              '可上传 ${state.uploadCount} · 已拦截 ${state.blockedCount}',
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ),
        const SizedBox(height: 8),
        if (state.devices.isEmpty)
          const _EmptyCard(
            icon: Icons.photo_library_outlined,
            message: '还没有设备上报图片处理状态。',
          )
        else
          for (final device in state.devices) _DeviceTile(device: device),
      ],
    );
  }
}

class _DeviceTile extends ConsumerWidget {
  const _DeviceTile({required this.device});

  final ImagePrivacyDevice device;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      child: ListTile(
        onTap: () => _openCapability(context, ref, device),
        leading: Icon(
          device.blocked
              ? Icons.block_rounded
              : device.uploadAllowed
              ? Icons.cloud_done_outlined
              : Icons.enhanced_encryption_outlined,
          color: device.blocked ? Theme.of(context).colorScheme.error : null,
        ),
        title: Text(_shortId(device.deviceId)),
        subtitle: Text(
          device.cameraSupported
              ? '${imageProcessingModeLabel(device.processingMode)} · '
                    '${imageUploadScopeLabel(device.uploadScope)} · '
                    '处理 ${device.processedCount} · '
                    '上传 ${device.uploadedCount} · '
                    '拦截 ${device.blockedCount}'
              : '此设备不支持图片处理',
        ),
        trailing: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (device.cameraSupported && !device.upToDate)
              const Padding(
                padding: EdgeInsets.only(right: 4),
                child: Tooltip(
                  message: '尚未应用最新设置',
                  child: Icon(Icons.sync_problem_rounded),
                ),
              ),
            const Icon(Icons.chevron_right_rounded),
          ],
        ),
      ),
    );
  }
}

class _InfoRow extends StatelessWidget {
  const _InfoRow({
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
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        children: [
          Icon(icon, size: 18, color: Theme.of(context).colorScheme.outline),
          const SizedBox(width: 10),
          Text(label),
          const Spacer(),
          Text(
            value,
            style: Theme.of(
              context,
            ).textTheme.bodyMedium?.copyWith(fontWeight: FontWeight.w600),
          ),
        ],
      ),
    );
  }
}

class _EmptyCard extends StatelessWidget {
  const _EmptyCard({required this.icon, required this.message});

  final IconData icon;
  final String message;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Row(
          children: [
            Icon(icon, color: Theme.of(context).colorScheme.primary),
            const SizedBox(width: 12),
            Expanded(child: Text(message)),
          ],
        ),
      ),
    );
  }
}

class _ImagePrivacyErrorState extends StatelessWidget {
  const _ImagePrivacyErrorState({
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
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 12),
            FilledButton(onPressed: onRetry, child: const Text('重新加载')),
          ],
        ),
      ),
    );
  }
}

Future<void> _publish(
  BuildContext context,
  WidgetRef ref,
  ImagePrivacyState state, {
  bool? enabled,
}) async {
  final policy = state.policy;
  final nextEnabled = enabled ?? policy.enabled;
  final nextLocalOnly = policy.localProcessingOnly;
  final nextUploadAllowed = nextEnabled && !nextLocalOnly
      ? policy.uploadAllowed
      : false;
  final nextProcessingMode = nextEnabled
      ? policy.processingMode == 'off'
            ? _firstNonOff(state.catalogs.processingModes, fallback: 'metadata')
            : policy.processingMode
      : 'off';
  final draft = ImagePrivacyDraft(
    enabled: nextEnabled,
    uploadAllowed: nextUploadAllowed,
    localProcessingOnly: nextLocalOnly,
    processingMode: nextProcessingMode,
    uploadScope: nextUploadAllowed ? policy.uploadScope : 'none',
    retentionDays: policy.retentionDays,
    redactionLevel: policy.redactionLevel,
    requireChildConsent: policy.requireChildConsent,
  );
  try {
    await ref
        .read(imagePrivacyControllerProvider.notifier)
        .publishPolicy(draft);
    if (context.mounted) {
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('图片隐私设置已保存')));
    }
  } on Object catch (error) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(ImagePrivacyController.messageFor(error))),
      );
    }
  }
}

Future<void> _openEditor(
  BuildContext context,
  WidgetRef ref,
  ImagePrivacyState state,
) {
  return Navigator.of(context).push<void>(
    MaterialPageRoute<void>(
      builder: (context) => _ImagePrivacyEditPage(state: state),
    ),
  );
}

Future<void> _openCapability(
  BuildContext context,
  WidgetRef ref,
  ImagePrivacyDevice device,
) async {
  showDialog<void>(
    context: context,
    barrierDismissible: false,
    builder: (context) => const Center(child: CircularProgressIndicator()),
  );
  try {
    final capability = await ref
        .read(imagePrivacyControllerProvider.notifier)
        .fetchCapability(device.deviceId);
    if (!context.mounted) {
      return;
    }
    Navigator.of(context).pop();
    await showModalBottomSheet<void>(
      context: context,
      showDragHandle: true,
      builder: (context) =>
          _CapabilitySheet(device: device, capability: capability),
    );
  } on Object catch (error) {
    if (!context.mounted) {
      return;
    }
    Navigator.of(context).pop();
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(content: Text(ImagePrivacyController.messageFor(error))),
    );
  }
}

class _CapabilitySheet extends StatelessWidget {
  const _CapabilitySheet({required this.device, required this.capability});

  final ImagePrivacyDevice device;
  final ImagePrivacyCapability capability;

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 0, 20, 28),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('设备图片能力', style: Theme.of(context).textTheme.titleLarge),
            const SizedBox(height: 6),
            Text(_shortId(device.deviceId)),
            const SizedBox(height: 16),
            _InfoRow(
              icon: Icons.photo_camera_outlined,
              label: '摄像头支持',
              value: capability.supported ? '支持' : '不支持',
            ),
            _InfoRow(
              icon: Icons.enhanced_encryption_outlined,
              label: '本机处理',
              value: capability.localProcessing ? '开启' : '关闭',
            ),
            _InfoRow(
              icon: Icons.cloud_upload_outlined,
              label: '允许上传',
              value: capability.uploadAllowed ? '是' : '否',
            ),
            _InfoRow(
              icon: Icons.blur_on_rounded,
              label: '脱敏级别',
              value: imageRedactionLevelLabel(capability.redactionLevel),
            ),
            _InfoRow(
              icon: Icons.sync_rounded,
              label: '设置版本',
              value: capability.upToDate
                  ? '已同步（${capability.appliedRevision}）'
                  : '待同步（${capability.appliedRevision}/${capability.policyRevision}）',
            ),
          ],
        ),
      ),
    );
  }
}

String _shortId(String deviceId) {
  if (deviceId.length <= 12) {
    return deviceId.isEmpty ? '未知设备' : deviceId;
  }
  return '${deviceId.substring(0, 6)}…${deviceId.substring(deviceId.length - 4)}';
}

class _ImagePrivacyEditPage extends ConsumerStatefulWidget {
  const _ImagePrivacyEditPage({required this.state});

  final ImagePrivacyState state;

  @override
  ConsumerState<_ImagePrivacyEditPage> createState() =>
      _ImagePrivacyEditPageState();
}

class _ImagePrivacyEditPageState extends ConsumerState<_ImagePrivacyEditPage> {
  late bool _enabled;
  late bool _uploadAllowed;
  late bool _localProcessingOnly;
  late String _processingMode;
  late String _uploadScope;
  late double _retentionDays;
  late String _redactionLevel;
  late bool _requireChildConsent;
  bool _isSubmitting = false;
  String? _errorMessage;

  ImagePrivacyCatalogs get _catalogs => widget.state.catalogs;

  @override
  void initState() {
    super.initState();
    final policy = widget.state.policy;
    _enabled = policy.enabled;
    _uploadAllowed = policy.uploadAllowed;
    _localProcessingOnly = policy.localProcessingOnly;
    _processingMode = policy.processingMode;
    _uploadScope = policy.uploadScope;
    _retentionDays = policy.retentionDays.toDouble();
    _redactionLevel = policy.redactionLevel;
    _requireChildConsent = policy.requireChildConsent;
  }

  void _setEnabled(bool value) {
    setState(() {
      _enabled = value;
      if (!value) {
        _processingMode = 'off';
        _uploadAllowed = false;
        _uploadScope = 'none';
      } else if (_processingMode == 'off') {
        _processingMode = _firstNonOff(
          _catalogs.processingModes,
          fallback: 'metadata',
        );
      }
    });
  }

  void _setLocalProcessingOnly(bool value) {
    setState(() {
      _localProcessingOnly = value;
      if (value) {
        _uploadAllowed = false;
        _uploadScope = 'none';
      }
    });
  }

  void _setUploadAllowed(bool value) {
    setState(() {
      _uploadAllowed = value;
      if (!value) {
        _uploadScope = 'none';
      } else if (_uploadScope == 'none') {
        _uploadScope = _firstNonNone(
          _catalogs.uploadScopes,
          fallback: 'thumbnails',
        );
      }
    });
  }

  Future<void> _submit() async {
    setState(() {
      _isSubmitting = true;
      _errorMessage = null;
    });
    final draft = ImagePrivacyDraft(
      enabled: _enabled,
      uploadAllowed: _enabled && !_localProcessingOnly && _uploadAllowed,
      localProcessingOnly: _localProcessingOnly,
      processingMode: _enabled ? _processingMode : 'off',
      uploadScope: _enabled && !_localProcessingOnly && _uploadAllowed
          ? _uploadScope
          : 'none',
      retentionDays: _retentionDays.round(),
      redactionLevel: _redactionLevel,
      requireChildConsent: _requireChildConsent,
    );
    try {
      await ref
          .read(imagePrivacyControllerProvider.notifier)
          .publishPolicy(draft);
      if (mounted) {
        Navigator.of(context).pop();
        ScaffoldMessenger.of(
          context,
        ).showSnackBar(const SnackBar(content: Text('图片隐私设置已保存')));
      }
    } on Object catch (error) {
      if (mounted) {
        setState(
          () => _errorMessage = ImagePrivacyController.messageFor(error),
        );
      }
    } finally {
      if (mounted) {
        setState(() => _isSubmitting = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    final canUpload = _enabled && !_localProcessingOnly;
    final modes = _safeCatalog(_catalogs.processingModes, const <String>[
      'off',
      'metadata',
      'local_full',
    ]);
    final scopes = _safeCatalog(_catalogs.uploadScopes, const <String>[
      'none',
      'thumbnails',
      'analysis',
    ]);
    final levels = _safeCatalog(_catalogs.redactionLevels, const <String>[
      'none',
      'standard',
      'strict',
    ]);
    return Scaffold(
      appBar: AppBar(title: const Text('图片隐私设置')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(20),
          children: [
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('开启图片处理'),
              subtitle: const Text('关闭后设备不会处理或上传任何图片'),
              value: _enabled,
              onChanged: _setEnabled,
            ),
            const Divider(),
            DropdownButtonFormField<String>(
              initialValue: modes.contains(_processingMode)
                  ? _processingMode
                  : modes.first,
              decoration: const InputDecoration(labelText: '处理方式'),
              items: [
                for (final mode in modes)
                  DropdownMenuItem<String>(
                    value: mode,
                    child: Text(imageProcessingModeLabel(mode)),
                  ),
              ],
              onChanged: _enabled
                  ? (value) =>
                        setState(() => _processingMode = value ?? modes.first)
                  : null,
            ),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('仅在本机处理'),
              subtitle: const Text('开启后图片不会离开设备'),
              value: _localProcessingOnly,
              onChanged: _enabled ? _setLocalProcessingOnly : null,
            ),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('允许上传'),
              subtitle: const Text('仅在关闭“仅在本机处理”后可用'),
              value: canUpload && _uploadAllowed,
              onChanged: canUpload ? _setUploadAllowed : null,
            ),
            const SizedBox(height: 8),
            DropdownButtonFormField<String>(
              initialValue: scopes.contains(_uploadScope)
                  ? _uploadScope
                  : scopes.first,
              decoration: const InputDecoration(labelText: '上传范围'),
              items: [
                for (final scope in scopes)
                  DropdownMenuItem<String>(
                    value: scope,
                    child: Text(imageUploadScopeLabel(scope)),
                  ),
              ],
              onChanged: canUpload && _uploadAllowed
                  ? (value) =>
                        setState(() => _uploadScope = value ?? scopes.first)
                  : null,
            ),
            const SizedBox(height: 16),
            DropdownButtonFormField<String>(
              initialValue: levels.contains(_redactionLevel)
                  ? _redactionLevel
                  : levels.first,
              decoration: const InputDecoration(labelText: '脱敏级别'),
              items: [
                for (final level in levels)
                  DropdownMenuItem<String>(
                    value: level,
                    child: Text(imageRedactionLevelLabel(level)),
                  ),
              ],
              onChanged: _enabled
                  ? (value) =>
                        setState(() => _redactionLevel = value ?? levels.first)
                  : null,
            ),
            const SizedBox(height: 8),
            Text(
              '保留时长：${_retentionDays.round() == 0 ? '不留存' : '${_retentionDays.round()} 天'}',
            ),
            Slider(
              value: _retentionDays,
              min: 0,
              max: 30,
              divisions: 30,
              label: _retentionDays.round() == 0
                  ? '不留存'
                  : '${_retentionDays.round()} 天',
              onChanged: _enabled
                  ? (value) => setState(() => _retentionDays = value)
                  : null,
            ),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('需要孩子授权'),
              subtitle: const Text('处理前需要孩子确认'),
              value: _requireChildConsent && _enabled,
              onChanged: _enabled
                  ? (value) => setState(() => _requireChildConsent = value)
                  : null,
            ),
            if (_errorMessage != null) ...[
              const SizedBox(height: 8),
              Text(
                _errorMessage!,
                style: TextStyle(color: Theme.of(context).colorScheme.error),
              ),
            ],
            const SizedBox(height: 20),
            FilledButton(
              onPressed: _isSubmitting ? null : _submit,
              child: Text(_isSubmitting ? '正在保存…' : '保存设置'),
            ),
          ],
        ),
      ),
    );
  }
}

List<String> _safeCatalog(List<String> values, List<String> fallback) {
  return values.isEmpty ? fallback : values;
}

String _firstNonOff(List<String> values, {required String fallback}) {
  for (final value in values) {
    if (value != 'off') {
      return value;
    }
  }
  return fallback;
}

String _firstNonNone(List<String> values, {required String fallback}) {
  for (final value in values) {
    if (value != 'none') {
      return value;
    }
  }
  return fallback;
}

import 'dart:io';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/error/app_exception.dart';
import '../../../shared/widgets/app_reveal.dart';
import '../../auth/application/auth_controller.dart';
import '../../auth/data/auth_api.dart';
import '../application/app_update_service.dart';

/// Whether the startup mandatory-update check runs.
///
/// Tests disable this because the check needs platform package metadata and a
/// live update endpoint.
final mandatoryUpdateCheckProvider = Provider<bool>((ref) => true);

/// Blocks the guardian application until a mandatory client update is opened.
///
/// Only client packages block. Resource packages do not change the installed
/// package version, so treating them as blocking would loop forever on the
/// same decision. A startup check must positively confirm that no blocking
/// client update is required. Until then the gate fails closed.
class MandatoryUpdateGate extends ConsumerStatefulWidget {
  const MandatoryUpdateGate({
    required this.child,
    this.updateService,
    this.updateLoader,
    super.key,
  });

  /// Rendered behind the blocking dialog when an update is required.
  final Widget child;

  /// Test seam for update installation without filesystem or network access.
  final AppUpdateCoordinator? updateService;

  /// Test seam for update discovery without platform package metadata.
  final Future<AppUpdateInfo?> Function(
    AppUpdateCoordinator service,
    String currentVersion,
  )?
  updateLoader;

  @override
  ConsumerState<MandatoryUpdateGate> createState() =>
      _MandatoryUpdateGateState();
}

class _MandatoryUpdateGateState extends ConsumerState<MandatoryUpdateGate>
    with WidgetsBindingObserver {
  late final AppUpdateCoordinator _updateService;
  AppUpdateInfo? _mandatoryUpdate;
  AppUpdateProgress? _progress;
  String? _actionError;
  String? _checkError;
  CancelToken? _installCancelToken;
  bool _isInstalling = false;
  bool _awaitsInstaller = false;
  bool _isChecking = true;
  late bool _checkSucceeded;
  bool _checkRunning = false;

  @override
  void initState() {
    super.initState();
    _updateService = widget.updateService ?? ref.read(appUpdateServiceProvider);
    final shouldCheck = ref.read(mandatoryUpdateCheckProvider);
    _checkSucceeded = !shouldCheck;
    _isChecking = shouldCheck;
    if (shouldCheck) {
      WidgetsBinding.instance.addObserver(this);
      WidgetsBinding.instance.addPostFrameCallback((_) => _check());
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _installCancelToken?.cancel('update gate closed');
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    // Returning from the system installer is the only signal that the
    // guardian may now be running the required build.
    if (state == AppLifecycleState.resumed && !_isInstalling) {
      _check();
    }
  }

  Future<void> _check() async {
    if (_checkRunning) {
      return;
    }
    _checkRunning = true;
    if (mounted) {
      setState(() {
        _isChecking = true;
        _checkError = null;
      });
    }
    try {
      final currentVersion = await _updateService.installedVersion();
      final update = await _loadUpdate(currentVersion);
      if (!mounted) {
        return;
      }
      final isBlocking =
          update != null && update.isMandatory && update.isClientUpdate;
      setState(() {
        _mandatoryUpdate = isBlocking ? update : null;
        _actionError = null;
        _checkError = null;
        _checkSucceeded = true;
        // Returning from a cancelled installer must allow a new attempt
        // instead of leaving the action permanently disabled.
        if (!_isInstalling) {
          _awaitsInstaller = false;
        }
      });
    } on Object catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        // Never retain a cleared decision after a failed check. A guardian who
        // was previously allowed must be blocked until a successful check.
        _checkSucceeded = false;
        _checkError = _errorMessage(error);
      });
    } finally {
      _checkRunning = false;
      if (mounted) {
        setState(() => _isChecking = false);
      }
    }
  }

  Future<AppUpdateInfo?> _loadUpdate(String currentVersion) {
    final updateLoader = widget.updateLoader;
    if (updateLoader != null) {
      return updateLoader(_updateService, currentVersion);
    }
    return ref
        .read(authApiProvider)
        .appUpdate(
          platform: Platform.isAndroid ? 'android' : 'ios',
          currentVersion: currentVersion,
        );
  }

  Future<void> _install() async {
    final update = _mandatoryUpdate;
    if (update == null || _isInstalling) {
      return;
    }
    final blockingProblem = _updateService.updateProblem(update);
    if (blockingProblem != null) {
      setState(() => _actionError = blockingProblem);
      return;
    }
    setState(() {
      _isInstalling = true;
      _actionError = null;
      _installCancelToken = CancelToken();
      _progress = const AppUpdateProgress(
        stage: AppUpdateStage.downloading,
        fraction: 0,
      );
    });
    final cancelToken = _installCancelToken;
    try {
      await _updateService.installClientUpdate(
        update: update,
        onProgress: _onProgress,
        cancelToken: cancelToken,
      );
      if (!mounted) {
        return;
      }
      setState(() {
        _isInstalling = false;
        _progress = null;
        _installCancelToken = null;
        _awaitsInstaller = true;
      });
    } on Object catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _isInstalling = false;
        _progress = null;
        _installCancelToken = null;
        _actionError = _errorMessage(error);
      });
    }
  }

  void _onProgress(AppUpdateProgress progress) {
    if (!mounted) {
      return;
    }
    setState(() => _progress = progress);
  }

  @override
  Widget build(BuildContext context) {
    final update = _mandatoryUpdate;
    if (_checkSucceeded && update == null) {
      return widget.child;
    }
    return PopScope(
      canPop: false,
      child: Stack(
        children: [
          widget.child,
          const ModalBarrier(dismissible: false, color: Color(0x66D94F83)),
          Center(
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: AppReveal(
                child: update != null
                    ? _MandatoryUpdateDialog(
                        update: update,
                        progress: _progress,
                        actionError: _actionError,
                        isInstalling: _isInstalling,
                        awaitsInstaller: _awaitsInstaller,
                        onInstall: _install,
                        onRetryCheck: _check,
                      )
                    : _UpdateCheckDialog(
                        isChecking: _isChecking,
                        errorMessage: _checkError,
                        onRetryCheck: _check,
                      ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _UpdateCheckDialog extends StatelessWidget {
  const _UpdateCheckDialog({
    required this.isChecking,
    required this.errorMessage,
    required this.onRetryCheck,
  });

  final bool isChecking;
  final String? errorMessage;
  final VoidCallback onRetryCheck;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 440),
      child: Material(
        borderRadius: BorderRadius.circular(24),
        color: theme.colorScheme.surface,
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(
                    Icons.verified_user_outlined,
                    color: theme.colorScheme.primary,
                  ),
                  const SizedBox(width: 10),
                  Text('安全检查', style: theme.textTheme.titleLarge),
                ],
              ),
              const SizedBox(height: 14),
              if (isChecking) ...[
                const LinearProgressIndicator(),
                const SizedBox(height: 12),
                const Text('正在确认当前版本是否可以安全使用。'),
              ] else ...[
                Text(errorMessage ?? '暂时无法确认当前版本是否可以安全使用。'),
                const SizedBox(height: 18),
                SizedBox(
                  width: double.infinity,
                  child: FilledButton.icon(
                    onPressed: onRetryCheck,
                    icon: const Icon(Icons.refresh_rounded),
                    label: const Text('重新检查'),
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _MandatoryUpdateDialog extends StatelessWidget {
  const _MandatoryUpdateDialog({
    required this.update,
    required this.progress,
    required this.actionError,
    required this.isInstalling,
    required this.awaitsInstaller,
    required this.onInstall,
    required this.onRetryCheck,
  });

  final AppUpdateInfo update;
  final AppUpdateProgress? progress;
  final String? actionError;
  final bool isInstalling;
  final bool awaitsInstaller;
  final VoidCallback onInstall;
  final VoidCallback onRetryCheck;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 440),
      child: Material(
        borderRadius: BorderRadius.circular(24),
        color: theme.colorScheme.surface,
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Icon(
                    Icons.system_update_alt_rounded,
                    color: theme.colorScheme.primary,
                  ),
                  const SizedBox(width: 10),
                  Text('需要更新', style: theme.textTheme.titleLarge),
                ],
              ),
              const SizedBox(height: 14),
              Text('当前版本 ${update.version} 需要更新后才能继续使用。'),
              if (update.releaseNotes.isNotEmpty) ...[
                const SizedBox(height: 14),
                Text('本次更新', style: theme.textTheme.titleMedium),
                const SizedBox(height: 6),
                Text(update.releaseNotes),
              ],
              if (progress != null) ...[
                const SizedBox(height: 16),
                _GateProgress(progress: progress!),
              ],
              if (awaitsInstaller) ...[
                const SizedBox(height: 14),
                _GateNotice(
                  icon: Icons.info_outline_rounded,
                  message: '请按系统提示完成安装，安装完成后重新打开应用。',
                ),
              ],
              if (actionError != null) ...[
                const SizedBox(height: 14),
                _GateNotice(
                  icon: Icons.error_outline_rounded,
                  message: actionError!,
                ),
              ],
              const SizedBox(height: 22),
              SizedBox(
                width: double.infinity,
                child: FilledButton.icon(
                  onPressed: isInstalling || awaitsInstaller ? null : onInstall,
                  icon: const Icon(Icons.download_rounded),
                  label: Text(
                    isInstalling
                        ? '正在准备更新'
                        : (awaitsInstaller ? '等待安装完成' : '立即更新'),
                  ),
                ),
              ),
              const SizedBox(height: 8),
              SizedBox(
                width: double.infinity,
                child: TextButton(
                  onPressed: isInstalling ? null : onRetryCheck,
                  child: const Text('重新检查更新'),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _GateProgress extends StatelessWidget {
  const _GateProgress({required this.progress});

  final AppUpdateProgress progress;

  @override
  Widget build(BuildContext context) {
    final fraction = progress.fraction.isFinite ? progress.fraction : 0.0;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(_stageLabel(progress.stage)),
        const SizedBox(height: 8),
        ClipRRect(
          borderRadius: BorderRadius.circular(8),
          child: LinearProgressIndicator(value: fraction.clamp(0.0, 1.0)),
        ),
      ],
    );
  }

  String _stageLabel(AppUpdateStage stage) {
    return switch (stage) {
      AppUpdateStage.downloading => '正在下载更新包',
      AppUpdateStage.verifying => '正在校验更新包',
      AppUpdateStage.installing => '正在打开安装页面',
      AppUpdateStage.extracting => '正在准备更新内容',
    };
  }
}

class _GateNotice extends StatelessWidget {
  const _GateNotice({required this.icon, required this.message});

  final IconData icon;
  final String message;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return DecoratedBox(
      decoration: BoxDecoration(
        color: colorScheme.errorContainer.withValues(alpha: 0.35),
        borderRadius: BorderRadius.circular(16),
      ),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(icon, size: 20, color: colorScheme.error),
            const SizedBox(width: 10),
            Expanded(child: Text(message)),
          ],
        ),
      ),
    );
  }
}

String _errorMessage(Object? error) {
  if (error is AppException) {
    return error.message;
  }
  return '更新没有完成，请稍后重试';
}

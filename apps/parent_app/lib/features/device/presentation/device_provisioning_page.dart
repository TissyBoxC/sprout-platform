import 'dart:async';

import 'package:esp_provisioning_wifi/esp_provisioning_wifi.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/error/app_exception.dart';
import '../../../core/theme/app_motion.dart';
import '../../../shared/widgets/app_reveal.dart';
import '../../../shared/widgets/app_state_switcher.dart';
import '../application/device_binding_controller.dart';
import '../data/device_binding_api.dart';
import '../domain/device_payload.dart';

/// Guided first-run flow for a display-equipped or nearby 初芽 device.
class DeviceProvisioningPage extends ConsumerStatefulWidget {
  const DeviceProvisioningPage({super.key, required this.setup});

  final DeviceSetupPayload setup;

  @override
  ConsumerState<DeviceProvisioningPage> createState() =>
      _DeviceProvisioningPageState();
}

enum _ProvisioningStep { finding, wifi, password, binding, done }

enum _ProvisioningStage {
  bluetoothConnection,
  sendingWifi,
  waitingForNetwork,
  confirmingBinding,
}

class _DeviceProvisioningPageState
    extends ConsumerState<DeviceProvisioningPage> {
  _ProvisioningStep _step = _ProvisioningStep.finding;
  List<String> _devices = const [];
  List<EspWifiNetwork> _networks = const [];
  String? _selectedDevice;
  String? _selectedSSID;
  String? _errorMessage;
  BoundDevice? _completedBinding;
  _ProvisioningStage? _activeStage;
  _ProvisioningStage? _failedStage;
  bool _isLeaving = false;
  late final DeviceBindingController _controller;
  final _passwordController = TextEditingController();

  @override
  void initState() {
    super.initState();
    _controller = ref.read(deviceBindingControllerProvider.notifier);
    _findDevice();
  }

  @override
  void dispose() {
    _passwordController.dispose();
    unawaited(_controller.cancelProvisioning());
    super.dispose();
  }

  Future<void> _leave() async {
    if (_isLeaving) {
      return;
    }
    _isLeaving = true;
    await _controller.cancelProvisioning();
    if (!mounted) {
      return;
    }
    final navigator = Navigator.of(context);
    if (navigator.canPop()) {
      navigator.pop();
      return;
    }
    context.go('/devices');
  }

  Future<void> _findDevice() async {
    setState(() {
      _step = _ProvisioningStep.finding;
      _errorMessage = null;
    });
    try {
      final devices = await _controller.scanProvisioningDevices();
      if (!mounted) {
        return;
      }
      final matching = devices
          .where((name) => name == widget.setup.serviceName)
          .toList(growable: false);
      setState(() {
        _devices = matching.isEmpty ? devices : matching;
        _selectedDevice = _devices.length == 1 ? _devices.single : null;
      });
    } on Object catch (error) {
      if (mounted) {
        setState(() => _errorMessage = _messageFor(error));
      }
    }
  }

  Future<void> _selectDevice(String deviceName) async {
    setState(() {
      _selectedDevice = deviceName;
      _step = _ProvisioningStep.wifi;
      _errorMessage = null;
      _networks = const [];
    });
    try {
      final networks = await _controller.scanWifiNetworks(widget.setup);
      if (!mounted) {
        return;
      }
      setState(() => _networks = networks);
    } on Object catch (error) {
      if (mounted) {
        setState(() => _errorMessage = _messageFor(error));
      }
    }
  }

  Future<void> _selectNetwork(EspWifiNetwork network) async {
    setState(() {
      _selectedSSID = network.ssid;
      _step = network.security == EspWifiSecurity.open
          ? _ProvisioningStep.password
          : _ProvisioningStep.password;
      _errorMessage = null;
    });
    if (network.security == EspWifiSecurity.open) {
      await _connect();
    }
  }

  Future<void> _connect() async {
    final deviceName = _selectedDevice;
    final ssid = _selectedSSID;
    if (deviceName == null || ssid == null) {
      setState(() => _errorMessage = '请先选择要连接的设备');
      return;
    }
    setState(() {
      _step = _ProvisioningStep.binding;
      _errorMessage = null;
      _activeStage = _ProvisioningStage.sendingWifi;
      _failedStage = null;
    });
    try {
      await _controller.provisionWifi(
        setup: widget.setup,
        ssid: ssid,
        password: _passwordController.text,
      );
      if (!mounted) {
        return;
      }
      setState(() {
        _activeStage = _ProvisioningStage.waitingForNetwork;
        _failedStage = null;
      });
      await _confirmBinding();
    } on Object catch (error) {
      if (mounted) {
        setState(() {
          _failedStage = _activeStage ?? _ProvisioningStage.sendingWifi;
          _errorMessage = _messageForStage(
            error,
            _failedStage ?? _ProvisioningStage.sendingWifi,
          );
        });
      }
    }
  }

  Future<void> _confirmBinding() async {
    try {
      final binding = await _controller.bindProvisionedDevice(
        setup: widget.setup,
      );
      if (mounted) {
        setState(() {
          _completedBinding = binding;
          _activeStage = null;
          _step = _ProvisioningStep.done;
        });
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() {
          _failedStage = _activeStage ?? _ProvisioningStage.waitingForNetwork;
          _errorMessage = _messageForStage(
            error,
            _failedStage ?? _ProvisioningStage.waitingForNetwork,
          );
        });
      }
    }
  }

  Future<void> _retryFailedStage() async {
    final stage = _failedStage;
    setState(() {
      _errorMessage = null;
      _failedStage = null;
    });
    switch (stage) {
      case _ProvisioningStage.bluetoothConnection:
        await _findDevice();
      case _ProvisioningStage.sendingWifi:
        await _connect();
      case _ProvisioningStage.waitingForNetwork:
      case _ProvisioningStage.confirmingBinding:
        await _confirmBinding();
      case null:
        await _connect();
    }
  }

  @override
  Widget build(BuildContext context) {
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) {
          _leave();
        }
      },
      child: Scaffold(
        appBar: AppBar(
          title: const Text('连接新设备'),
          leading: IconButton(
            tooltip: '返回',
            onPressed: _leave,
            icon: const Icon(Icons.arrow_back_rounded),
          ),
        ),
        body: SafeArea(
          child: Padding(
            padding: const EdgeInsets.all(20),
            child: AppStateSwitcher(
              stateKey: _step,
              child: switch (_step) {
                _ProvisioningStep.finding => KeyedSubtree(
                  key: const ValueKey<_ProvisioningStep>(
                    _ProvisioningStep.finding,
                  ),
                  child: _buildFinding(),
                ),
                _ProvisioningStep.wifi => KeyedSubtree(
                  key: const ValueKey<_ProvisioningStep>(
                    _ProvisioningStep.wifi,
                  ),
                  child: _buildWifiList(),
                ),
                _ProvisioningStep.password => KeyedSubtree(
                  key: const ValueKey<_ProvisioningStep>(
                    _ProvisioningStep.password,
                  ),
                  child: _buildPassword(),
                ),
                _ProvisioningStep.binding => KeyedSubtree(
                  key: const ValueKey<_ProvisioningStep>(
                    _ProvisioningStep.binding,
                  ),
                  child: _buildProgress(),
                ),
                _ProvisioningStep.done => KeyedSubtree(
                  key: const ValueKey<_ProvisioningStep>(
                    _ProvisioningStep.done,
                  ),
                  child: _buildDone(),
                ),
              },
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildFinding() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const Text('正在寻找初芽'),
        const SizedBox(height: 12),
        const LinearProgressIndicator(),
        if (_devices.isNotEmpty) ...[
          const SizedBox(height: 24),
          Text('找到这些设备', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          ..._devices.map(
            (device) => Card(
              child: ListTile(
                leading: const Icon(Icons.toys_outlined),
                title: Text(device),
                trailing: const Icon(Icons.chevron_right),
                onTap: () => _selectDevice(device),
              ),
            ),
          ),
        ],
        if (_devices.isEmpty && _errorMessage == null) ...[
          const SizedBox(height: 16),
          const Text('请让初芽保持开机，并停留在配网页面。'),
          const SizedBox(height: 12),
          OutlinedButton.icon(
            onPressed: _findDevice,
            icon: const Icon(Icons.refresh),
            label: const Text('重新寻找'),
          ),
        ],
        if (_errorMessage != null) ...[
          const SizedBox(height: 16),
          Text(
            _errorMessage!,
            style: TextStyle(color: Theme.of(context).colorScheme.error),
          ),
          const SizedBox(height: 12),
          FilledButton(onPressed: _findDevice, child: const Text('重新寻找')),
        ],
      ],
    );
  }

  Widget _buildWifiList() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text('选择要连接的无线网络', style: Theme.of(context).textTheme.titleMedium),
        const SizedBox(height: 8),
        if (_networks.isEmpty && _errorMessage == null)
          const LinearProgressIndicator(),
        if (_errorMessage != null) ...[
          const SizedBox(height: 16),
          Text(
            _errorMessage!,
            style: TextStyle(color: Theme.of(context).colorScheme.error),
          ),
          const SizedBox(height: 12),
          FilledButton(
            onPressed: _selectedDevice == null
                ? null
                : () => _selectDevice(_selectedDevice!),
            child: const Text('重新读取网络'),
          ),
        ],
        Expanded(
          child: ListView(
            children: _networks
                .map(
                  (network) => AnimatedContainer(
                    duration: const Duration(milliseconds: 180),
                    curve: Curves.easeOutCubic,
                    margin: const EdgeInsets.only(bottom: 2),
                    child: Card(
                      child: ListTile(
                        leading: const Icon(Icons.wifi),
                        title: Text(network.ssid),
                        subtitle: network.rssi == null
                            ? null
                            : Text('信号 ${network.rssi} dBm'),
                        trailing: const Icon(Icons.chevron_right),
                        onTap: () => _selectNetwork(network),
                      ),
                    ),
                  ),
                )
                .toList(growable: false),
          ),
        ),
      ],
    );
  }

  Widget _buildPassword() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          '输入“${_selectedSSID ?? ''}”的密码',
          style: Theme.of(context).textTheme.titleMedium,
        ),
        const SizedBox(height: 16),
        TextField(
          controller: _passwordController,
          obscureText: true,
          autofocus: true,
          decoration: const InputDecoration(labelText: '无线网络密码'),
          onSubmitted: (_) => _connect(),
        ),
        if (_errorMessage != null) ...[
          const SizedBox(height: 12),
          Text(
            _errorMessage!,
            style: TextStyle(color: Theme.of(context).colorScheme.error),
          ),
        ],
        const SizedBox(height: 20),
        FilledButton(onPressed: _connect, child: const Text('连接无线网络')),
        const SizedBox(height: 10),
        OutlinedButton(
          onPressed: () => setState(() {
            _step = _ProvisioningStep.wifi;
            _errorMessage = null;
          }),
          child: const Text('选择其他网络'),
        ),
      ],
    );
  }

  Widget _buildProgress() {
    final current =
        _failedStage ?? _activeStage ?? _ProvisioningStage.sendingWifi;
    final hasFailed = _failedStage != null;
    return ListView(
      children: [
        AppReveal(
          child: Text(
            hasFailed ? '连接没有完成' : '正在连接初芽',
            style: Theme.of(context).textTheme.titleLarge,
          ),
        ),
        const SizedBox(height: 8),
        AppReveal(
          delay: const Duration(milliseconds: 40),
          child: Text(
            hasFailed ? '已停留在没有完成的步骤，你可以直接重试。' : '请保持初芽开机，并把手机放在设备附近。',
          ),
        ),
        const SizedBox(height: 20),
        AppReveal(
          delay: const Duration(milliseconds: 80),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.all(18),
              child: Column(
                children: [
                  for (
                    var index = 0;
                    index < _ProvisioningStage.values.length;
                    index++
                  )
                    _ProvisioningStageTile(
                      stage: _ProvisioningStage.values[index],
                      isComplete: index < current.index,
                      isCurrent: !hasFailed && index == current.index,
                      hasFailed: hasFailed && index == current.index,
                      isLast: index == _ProvisioningStage.values.length - 1,
                    ),
                  if (!hasFailed) ...[
                    const SizedBox(height: 16),
                    const LinearProgressIndicator(),
                  ],
                ],
              ),
            ),
          ),
        ),
        if (hasFailed && _errorMessage != null) ...[
          const SizedBox(height: 16),
          _ProvisioningFailureNotice(
            message: _errorMessage!,
            onRetry: _retryFailedStage,
            onEditPassword: current == _ProvisioningStage.sendingWifi
                ? () => setState(() {
                    _step = _ProvisioningStep.password;
                    _activeStage = null;
                    _failedStage = null;
                    _errorMessage = null;
                  })
                : null,
          ),
        ],
      ],
    );
  }

  Widget _buildDone() {
    final binding = _completedBinding;
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          TweenAnimationBuilder<double>(
            tween: Tween<double>(begin: 0.82, end: 1),
            duration: const Duration(milliseconds: 360),
            curve: Curves.easeOutBack,
            builder: (context, scale, child) =>
                Transform.scale(scale: scale, child: child),
            child: Icon(
              Icons.check_circle_outline,
              size: 56,
              color: Theme.of(context).colorScheme.primary,
            ),
          ),
          const SizedBox(height: 16),
          Text('初芽已连接', style: Theme.of(context).textTheme.titleLarge),
          const SizedBox(height: 6),
          Text(
            binding == null ? '现在可以在家长端查看设备状态。' : '设备已加入“我的设备”，联网后会显示最新状态。',
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 20),
          FilledButton(
            onPressed: () {
              ref.read(deviceBindingControllerProvider.notifier).refresh();
              if (context.canPop()) {
                context.pop();
              } else {
                context.go('/devices');
              }
            },
            child: const Text('查看我的设备'),
          ),
        ],
      ),
    );
  }

  String _messageFor(Object error) {
    if (error is AppException) {
      return error.message;
    }
    return '连接没有完成，请确认初芽已开机并靠近手机';
  }

  String _messageForStage(Object error, _ProvisioningStage stage) {
    final message = _messageFor(error);
    if (message != '连接没有完成，请确认初芽已开机并靠近手机') {
      return message;
    }
    return switch (stage) {
      _ProvisioningStage.bluetoothConnection => '没有通过蓝牙找到初芽，请确认设备已开机并靠近手机后重试',
      _ProvisioningStage.sendingWifi => '没有把无线网络信息发送到设备，请确认密码后重试',
      _ProvisioningStage.waitingForNetwork => '设备还没有连上无线网络，请确认网络可用并保持设备开机后重试',
      _ProvisioningStage.confirmingBinding => '设备已经联网，但平台还没有确认绑定，请检查网络后重试',
    };
  }
}

class _ProvisioningStageTile extends StatelessWidget {
  const _ProvisioningStageTile({
    required this.stage,
    required this.isComplete,
    required this.isCurrent,
    required this.hasFailed,
    required this.isLast,
  });

  final _ProvisioningStage stage;
  final bool isComplete;
  final bool isCurrent;
  final bool hasFailed;
  final bool isLast;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    final color = hasFailed
        ? colorScheme.error
        : isComplete
        ? const Color(0xFF2E7D5B)
        : isCurrent
        ? colorScheme.primary
        : colorScheme.outline;
    final icon = hasFailed
        ? Icons.close_rounded
        : isComplete
        ? Icons.check_rounded
        : isCurrent
        ? Icons.more_horiz_rounded
        : Icons.circle_outlined;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Column(
          children: [
            AnimatedContainer(
              duration: AppMotion.fast,
              width: 34,
              height: 34,
              decoration: BoxDecoration(
                color: color.withValues(alpha: 0.12),
                shape: BoxShape.circle,
                border: Border.all(color: color, width: 1.5),
              ),
              child: Icon(icon, size: 20, color: color),
            ),
            if (!isLast)
              Container(
                width: 2,
                height: 38,
                color: isComplete
                    ? const Color(0xFF2E7D5B).withValues(alpha: 0.42)
                    : colorScheme.outlineVariant,
              ),
          ],
        ),
        const SizedBox(width: 12),
        Expanded(
          child: Padding(
            padding: const EdgeInsets.only(top: 4, bottom: 18),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  _stageTitle(stage),
                  style: Theme.of(context).textTheme.titleSmall?.copyWith(
                    color: color,
                    fontWeight: FontWeight.w700,
                  ),
                ),
                const SizedBox(height: 3),
                Text(hasFailed ? '这一步没有完成' : _stageDescription(stage)),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

class _ProvisioningFailureNotice extends StatelessWidget {
  const _ProvisioningFailureNotice({
    required this.message,
    required this.onRetry,
    this.onEditPassword,
  });

  final String message;
  final VoidCallback onRetry;
  final VoidCallback? onEditPassword;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.errorContainer,
        borderRadius: BorderRadius.circular(20),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Icon(
                Icons.info_outline_rounded,
                color: Theme.of(context).colorScheme.onErrorContainer,
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  message,
                  style: TextStyle(
                    color: Theme.of(context).colorScheme.onErrorContainer,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 14),
          FilledButton.icon(
            onPressed: onRetry,
            icon: const Icon(Icons.refresh_rounded),
            label: const Text('重试'),
          ),
          if (onEditPassword != null) ...[
            const SizedBox(height: 8),
            TextButton(
              onPressed: onEditPassword,
              child: const Text('返回修改无线网络密码'),
            ),
          ],
        ],
      ),
    );
  }
}

String _stageTitle(_ProvisioningStage stage) {
  return switch (stage) {
    _ProvisioningStage.bluetoothConnection => '连接设备蓝牙',
    _ProvisioningStage.sendingWifi => '发送无线网络',
    _ProvisioningStage.waitingForNetwork => '等待设备联网',
    _ProvisioningStage.confirmingBinding => '确认设备绑定',
  };
}

String _stageDescription(_ProvisioningStage stage) {
  return switch (stage) {
    _ProvisioningStage.bluetoothConnection => '让手机找到附近的初芽',
    _ProvisioningStage.sendingWifi => '把选好的网络和密码安全发送到设备',
    _ProvisioningStage.waitingForNetwork => '设备正在连接家里的无线网络',
    _ProvisioningStage.confirmingBinding => '让设备加入当前家长账号',
  };
}

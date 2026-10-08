import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../../../core/error/app_exception.dart';
import '../../../core/theme/app_motion.dart';
import '../../../shared/widgets/app_reveal.dart';
import '../application/device_binding_controller.dart';
import '../domain/device_payload.dart';

typedef DeviceScannerViewBuilder =
    Widget Function(BuildContext context, MobileScannerController controller);

/// Scans the one-time provisioning QR shown by a display-equipped device.
class DeviceQrScanPage extends ConsumerStatefulWidget {
  const DeviceQrScanPage({super.key, this.scannerViewBuilder});

  /// Test seam that keeps camera platform channels out of widget tests.
  final DeviceScannerViewBuilder? scannerViewBuilder;

  @override
  ConsumerState<DeviceQrScanPage> createState() => _DeviceQrScanPageState();
}

class _DeviceQrScanPageState extends ConsumerState<DeviceQrScanPage> {
  final _controller = MobileScannerController(
    formats: const [BarcodeFormat.qrCode],
    detectionSpeed: DetectionSpeed.noDuplicates,
  );
  bool _isHandlingResult = false;
  bool _isLeaving = false;
  bool _isScannerReleased = false;
  String? _errorMessage;
  DeviceBindingPayload? _failedBindingPayload;

  @override
  void dispose() {
    _releaseScanner();
    super.dispose();
  }

  Future<void> _releaseScanner() async {
    if (_isScannerReleased) {
      return;
    }
    _isScannerReleased = true;
    try {
      await _controller.stop();
    } on Object {
      // The scanner may already be stopped or detached during route teardown.
    }
    try {
      await _controller.dispose();
    } on Object {
      // Disposal must not block returning to the device list.
    }
  }

  Future<void> _handleBarcode(BarcodeCapture capture) async {
    if (_isHandlingResult || _isLeaving) {
      return;
    }
    final rawValue = capture.barcodes.firstOrNull?.rawValue;
    if (rawValue == null || rawValue.isEmpty) {
      return;
    }
    _isHandlingResult = true;
    await _controller.stop();
    if (!mounted || _isLeaving) {
      return;
    }

    final payload = DevicePayload.tryParse(rawValue);
    if (payload == null) {
      setState(() {
        _errorMessage = '这不是初芽设备上的绑定码，请重新扫描';
        _failedBindingPayload = null;
      });
      _isHandlingResult = false;
      if (mounted && !_isLeaving) {
        await _controller.start();
      }
      return;
    }
    try {
      if (payload is DeviceSetupPayload) {
        if (mounted) {
          await context.push('/devices/provision', extra: payload);
        }
        _isHandlingResult = false;
        if (mounted && !_isLeaving) {
          await _controller.start();
        }
        return;
      }
      final bindingPayload = payload as DeviceBindingPayload;
      setState(() {
        _errorMessage = null;
        _failedBindingPayload = null;
      });
      await ref
          .read(deviceBindingControllerProvider.notifier)
          .bindToken(
            token: bindingPayload.bindingToken,
            deviceName: bindingPayload.deviceName,
          );
      if (mounted) {
        context.go('/devices');
      }
    } on Object catch (error) {
      if (mounted && !_isLeaving) {
        setState(() {
          _errorMessage = _scanErrorMessage(
            error,
            isBindingPayload: payload is DeviceBindingPayload,
          );
          _failedBindingPayload = payload is DeviceBindingPayload
              ? payload
              : null;
        });
      }
      _isHandlingResult = false;
      if (mounted && !_isLeaving) {
        await _controller.start();
      }
    }
  }

  Future<void> _leave() async {
    if (_isLeaving) {
      return;
    }
    _isLeaving = true;
    await _releaseScanner();
    if (mounted) {
      final navigator = Navigator.of(context);
      if (navigator.canPop()) {
        navigator.pop();
        return;
      }
      context.go('/devices');
    }
  }

  Future<void> _retryBinding() async {
    final payload = _failedBindingPayload;
    setState(() => _errorMessage = null);
    if (payload == null) {
      _isHandlingResult = false;
      if (!_isLeaving) {
        setState(() => _isScannerReleased = false);
        await _controller.start();
      }
      return;
    }
    setState(() => _isHandlingResult = true);
    try {
      await ref
          .read(deviceBindingControllerProvider.notifier)
          .bindToken(
            token: payload.bindingToken,
            deviceName: payload.deviceName,
          );
      if (mounted) {
        context.go('/devices');
      }
    } on Object catch (error) {
      if (!mounted) {
        return;
      }
      setState(() {
        _isHandlingResult = false;
        _errorMessage = _scanErrorMessage(error, isBindingPayload: true);
      });
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
          title: const Text('扫描设备绑定码'),
          leading: IconButton(
            tooltip: '返回',
            onPressed: _leave,
            icon: const Icon(Icons.arrow_back_rounded),
          ),
        ),
        body: Stack(
          children: [
            _scannerView(context),
            const Center(child: _ScannerFrame()),
            AnimatedSlide(
              offset: _errorMessage == null
                  ? const Offset(0, 0.18)
                  : Offset.zero,
              duration: AppMotion.standard,
              curve: AppMotion.enterCurve,
              child: AnimatedOpacity(
                opacity: _errorMessage == null ? 0 : 1,
                duration: AppMotion.fast,
                child: Align(
                  alignment: Alignment.bottomCenter,
                  child: Container(
                    width: double.infinity,
                    margin: const EdgeInsets.all(24),
                    padding: const EdgeInsets.all(16),
                    decoration: BoxDecoration(
                      color: Theme.of(context).colorScheme.errorContainer,
                      borderRadius: BorderRadius.circular(18),
                    ),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Text(
                          _errorMessage ?? '',
                          style: TextStyle(
                            color: Theme.of(
                              context,
                            ).colorScheme.onErrorContainer,
                          ),
                        ),
                        const SizedBox(height: 12),
                        FilledButton.icon(
                          onPressed: _isHandlingResult ? null : _retryBinding,
                          icon: Icon(
                            _failedBindingPayload == null
                                ? Icons.qr_code_scanner_rounded
                                : Icons.refresh_rounded,
                          ),
                          label: Text(
                            _failedBindingPayload == null ? '重新扫描' : '重试',
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _scannerView(BuildContext context) {
    final builder = widget.scannerViewBuilder;
    if (builder != null) {
      return builder(context, _controller);
    }
    return MobileScanner(
      controller: _controller,
      onDetect: _handleBarcode,
      errorBuilder: (context, error) => const Center(
        child: Padding(
          padding: EdgeInsets.all(24),
          child: Text('无法使用相机，请在系统设置中允许相机权限后重试。'),
        ),
      ),
    );
  }
}

String _scanErrorMessage(Object error, {required bool isBindingPayload}) {
  if (error is AppException && error.message.trim().isNotEmpty) {
    return error.message;
  }
  return isBindingPayload
      ? '绑定没有完成，请确认设备已开机并靠近手机后重试'
      : '没有读取到绑定信息，请重新扫描设备上的绑定码';
}

class _ScannerFrame extends StatelessWidget {
  const _ScannerFrame();

  @override
  Widget build(BuildContext context) {
    return AppReveal(
      child: Container(
        width: 240,
        height: 240,
        decoration: BoxDecoration(
          border: Border.all(
            color: Theme.of(context).colorScheme.primary,
            width: 3,
          ),
          borderRadius: BorderRadius.circular(24),
          boxShadow: [
            BoxShadow(
              color: Theme.of(
                context,
              ).colorScheme.primary.withValues(alpha: 0.18),
              blurRadius: 28,
              spreadRadius: 2,
            ),
          ],
        ),
      ),
    );
  }
}

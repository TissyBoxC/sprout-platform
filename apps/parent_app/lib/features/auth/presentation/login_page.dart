import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/theme/app_motion.dart';
import '../../../shared/widgets/app_reveal.dart';
import '../../../app/router/app_router.dart';
import '../application/auth_controller.dart';

/// Parent sign-in screen for returning guardians.
class LoginPage extends ConsumerStatefulWidget {
  const LoginPage({required this.destination, super.key});

  /// Restores the protected page that redirected an unauthenticated visitor.
  final LoginDestination destination;

  @override
  ConsumerState<LoginPage> createState() => _LoginPageState();
}

class _LoginPageState extends ConsumerState<LoginPage> {
  final _formKey = GlobalKey<FormState>();
  final _identifierController = TextEditingController();
  final _passwordController = TextEditingController();
  bool _isSubmitting = false;
  bool _isLoadingPolicy = true;
  bool _emailLoginEnabled = true;
  String? _errorMessage;

  @override
  void initState() {
    super.initState();
    _loadRegistrationPolicy();
  }

  Future<void> _loadRegistrationPolicy() async {
    try {
      final policy = await ref.read(authApiProvider).registrationPolicy();
      if (!mounted) {
        return;
      }
      setState(() {
        _emailLoginEnabled = policy.emailLoginEnabled;
        _isLoadingPolicy = false;
      });
    } on Object {
      if (mounted) {
        setState(() => _isLoadingPolicy = false);
      }
    }
  }

  @override
  void dispose() {
    _identifierController.dispose();
    _passwordController.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (!_formKey.currentState!.validate()) {
      return;
    }
    setState(() {
      _isSubmitting = true;
      _errorMessage = null;
    });
    try {
      final succeeded = await ref
          .read(authControllerProvider.notifier)
          .login(
            identifier: _identifierController.text.trim(),
            password: _passwordController.text,
          );
      if (!mounted) {
        return;
      }
      if (succeeded) {
        context.go(widget.destination.take() ?? '/home');
        return;
      }
      setState(() {
        _errorMessage = authErrorMessage(
          ref.read(authControllerProvider).error,
        );
      });
    } on Object catch (error) {
      if (mounted) {
        setState(() => _errorMessage = authErrorMessage(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isSubmitting = false);
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 420),
              child: AppReveal(
                child: Form(
                  key: _formKey,
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      const _AuthBrandHeader(),
                      const SizedBox(height: 28),
                      TextFormField(
                        controller: _identifierController,
                        keyboardType: TextInputType.text,
                        autofillHints: const [AutofillHints.username],
                        decoration: InputDecoration(
                          labelText: _emailLoginEnabled ? '手机号或邮箱' : '手机号',
                          helperText: _isLoadingPolicy
                              ? '正在读取登录方式…'
                              : _emailLoginEnabled
                              ? null
                              : '邮箱登录暂时关闭，请使用手机号登录',
                        ),
                        validator: (value) {
                          if (value == null || value.trim().isEmpty) {
                            return _emailLoginEnabled ? '请输入手机号或邮箱' : '请输入手机号';
                          }
                          final identifier = value.trim();
                          if (_isValidPhone(identifier) ||
                              (_emailLoginEnabled &&
                                  _isValidEmail(identifier))) {
                            return null;
                          }
                          return _emailLoginEnabled
                              ? '请输入有效的手机号或邮箱'
                              : '请输入有效的手机号';
                        },
                      ),
                      const SizedBox(height: 16),
                      TextFormField(
                        controller: _passwordController,
                        obscureText: true,
                        autofillHints: const [AutofillHints.password],
                        decoration: const InputDecoration(labelText: '密码'),
                        validator: (value) {
                          if (value == null || value.isEmpty) {
                            return '请输入密码';
                          }
                          return null;
                        },
                      ),
                      AnimatedSize(
                        duration: AppMotion.standard,
                        curve: AppMotion.enterCurve,
                        alignment: Alignment.topCenter,
                        child: _errorMessage == null
                            ? const SizedBox(width: double.infinity)
                            : Padding(
                                padding: const EdgeInsets.only(top: 12),
                                child: Text(
                                  _errorMessage!,
                                  style: TextStyle(
                                    color: Theme.of(context).colorScheme.error,
                                  ),
                                ),
                              ),
                      ),
                      const SizedBox(height: 24),
                      FilledButton(
                        onPressed: _isSubmitting ? null : _submit,
                        child: Text(
                          _isSubmitting ? '正在登录…' : '登录',
                          semanticsLabel: _isSubmitting ? '正在登录' : '登录',
                        ),
                      ),
                      const SizedBox(height: 12),
                      TextButton(
                        onPressed: _isSubmitting
                            ? null
                            : () => context.go('/register'),
                        child: const Text('还没有账号？创建家长账号'),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

bool _isValidPhone(String value) {
  final normalized = value.replaceAll(RegExp(r'[\s\-()]'), '');
  return RegExp(r'^1\d{10}$').hasMatch(normalized);
}

bool _isValidEmail(String value) {
  return RegExp(r'^[^@\s]+@[^@\s]+\.[^@\s]+$').hasMatch(value.trim());
}

class _AuthBrandHeader extends StatelessWidget {
  const _AuthBrandHeader();

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        ClipOval(
          child: Image.asset(
            'assets/brand/sprout/brand_avatar.png',
            width: 88,
            height: 88,
            semanticLabel: '如此萌屋',
          ),
        ),
        const SizedBox(height: 16),
        Text(
          '欢迎回家',
          style: Theme.of(context).textTheme.headlineSmall,
          textAlign: TextAlign.center,
        ),
        const SizedBox(height: 6),
        Text(
          '登录后管理孩子的设备和 AI 陪伴设置',
          style: Theme.of(context).textTheme.bodyMedium,
          textAlign: TextAlign.center,
        ),
      ],
    );
  }
}

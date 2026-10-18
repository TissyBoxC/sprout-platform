import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/theme/app_motion.dart';
import '../../../shared/widgets/app_reveal.dart';
import '../../../app/router/app_router.dart';
import '../application/auth_controller.dart';

/// Guardian registration screen with an explicit consent requirement.
class RegisterPage extends ConsumerStatefulWidget {
  const RegisterPage({required this.destination, super.key});

  /// Restores the protected page that led the guardian into registration.
  final LoginDestination destination;

  @override
  ConsumerState<RegisterPage> createState() => _RegisterPageState();
}

class _RegisterPageState extends ConsumerState<RegisterPage> {
  final _formKey = GlobalKey<FormState>();
  final _phoneController = TextEditingController();
  final _phoneVerificationCodeController = TextEditingController();
  final _passwordController = TextEditingController();
  final _guardianFamilyNameController = TextEditingController();
  final _childNicknameController = TextEditingController();
  final _childBirthdayController = TextEditingController();
  bool _hasGuardianConsent = false;
  bool _isSubmitting = false;
  bool _isSendingVerificationCode = false;
  String? _errorMessage;
  String? _verificationStatusMessage;

  @override
  void dispose() {
    _phoneController.dispose();
    _phoneVerificationCodeController.dispose();
    _passwordController.dispose();
    _guardianFamilyNameController.dispose();
    _childNicknameController.dispose();
    _childBirthdayController.dispose();
    super.dispose();
  }

  Future<void> _sendVerificationCode() async {
    final phone = _phoneController.text.trim();
    if (!_isValidPhone(phone)) {
      setState(() => _errorMessage = '请输入有效的手机号');
      return;
    }
    setState(() {
      _isSendingVerificationCode = true;
      _errorMessage = null;
      _verificationStatusMessage = null;
    });
    try {
      await ref
          .read(authControllerProvider.notifier)
          .sendPhoneVerification(phone: phone);
      if (mounted) {
        setState(() {
          _verificationStatusMessage = '验证码已发送，请查看手机短信';
        });
      }
    } on Object catch (error) {
      if (mounted) {
        setState(() => _errorMessage = authErrorMessage(error));
      }
    } finally {
      if (mounted) {
        setState(() => _isSendingVerificationCode = false);
      }
    }
  }

  Future<void> _submit() async {
    if (!_formKey.currentState!.validate()) {
      return;
    }
    if (!_hasGuardianConsent) {
      setState(() => _errorMessage = '请先确认你是孩子的监护人');
      return;
    }
    setState(() {
      _isSubmitting = true;
      _errorMessage = null;
    });
    try {
      final succeeded = await ref
          .read(authControllerProvider.notifier)
          .register(
            phone: _phoneController.text.trim(),
            phoneVerificationCode: _phoneVerificationCodeController.text.trim(),
            password: _passwordController.text,
            guardianFamilyName: _guardianFamilyNameController.text.trim(),
            childNickname: _childNicknameController.text.trim(),
            childBirthday: _childBirthdayController.text.trim(),
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
      appBar: AppBar(
        leading: IconButton(
          tooltip: '返回登录',
          onPressed: () => context.go('/login'),
          icon: const Icon(Icons.arrow_back),
        ),
        title: const Text('创建家长账号'),
      ),
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 440),
              child: AppReveal(
                child: Form(
                  key: _formKey,
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      TextFormField(
                        controller: _phoneController,
                        keyboardType: TextInputType.phone,
                        autofillHints: const [AutofillHints.telephoneNumber],
                        decoration: const InputDecoration(labelText: '手机号'),
                        validator: (value) {
                          if (value == null || value.trim().isEmpty) {
                            return '请输入手机号';
                          }
                          if (!_isValidPhone(value)) {
                            return '请输入有效的手机号';
                          }
                          return null;
                        },
                      ),
                      const SizedBox(height: 16),
                      Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Expanded(
                            child: TextFormField(
                              controller: _phoneVerificationCodeController,
                              keyboardType: TextInputType.number,
                              decoration: const InputDecoration(
                                labelText: '短信验证码',
                              ),
                              validator: (value) {
                                final code = value?.trim() ?? '';
                                if (code.isNotEmpty &&
                                    !RegExp(r'^\d{6}$').hasMatch(code)) {
                                  return '请输入 6 位验证码';
                                }
                                return null;
                              },
                            ),
                          ),
                          const SizedBox(width: 12),
                          ConstrainedBox(
                            constraints: const BoxConstraints(
                              minWidth: 112,
                              maxWidth: 132,
                            ),
                            child: Padding(
                              padding: const EdgeInsets.only(top: 4),
                              child: OutlinedButton(
                                onPressed: _isSendingVerificationCode
                                    ? null
                                    : _sendVerificationCode,
                                child: FittedBox(
                                  fit: BoxFit.scaleDown,
                                  child: Text(
                                    _isSendingVerificationCode
                                        ? '正在发送…'
                                        : '获取验证码',
                                  ),
                                ),
                              ),
                            ),
                          ),
                        ],
                      ),
                      if (_verificationStatusMessage != null) ...[
                        const SizedBox(height: 8),
                        Text(
                          _verificationStatusMessage!,
                          style: Theme.of(context).textTheme.bodySmall,
                        ),
                      ],
                      const SizedBox(height: 16),
                      TextFormField(
                        controller: _passwordController,
                        obscureText: true,
                        autofillHints: const [AutofillHints.newPassword],
                        decoration: const InputDecoration(
                          labelText: '密码',
                          helperText: '至少 8 位，并同时包含字母和数字',
                        ),
                        validator: (value) {
                          if (value == null || value.length < 8) {
                            return '密码至少 8 位';
                          }
                          if (!RegExp(r'[A-Za-z]').hasMatch(value) ||
                              !RegExp(r'[0-9]').hasMatch(value)) {
                            return '密码需要同时包含字母和数字';
                          }
                          return null;
                        },
                      ),
                      const SizedBox(height: 16),
                      TextFormField(
                        controller: _guardianFamilyNameController,
                        decoration: const InputDecoration(
                          labelText: '家长姓氏（可选）',
                        ),
                        validator: (value) {
                          if ((value?.trim().length ?? 0) > 40) {
                            return '家长姓氏不能超过 40 个字';
                          }
                          return null;
                        },
                      ),
                      const SizedBox(height: 16),
                      TextFormField(
                        controller: _childNicknameController,
                        decoration: const InputDecoration(
                          labelText: '宝贝姓名（可选）',
                        ),
                        validator: (value) {
                          if ((value?.trim().length ?? 0) > 40) {
                            return '宝贝姓名不能超过 40 个字';
                          }
                          return null;
                        },
                      ),
                      const SizedBox(height: 16),
                      TextFormField(
                        controller: _childBirthdayController,
                        keyboardType: TextInputType.datetime,
                        decoration: const InputDecoration(
                          labelText: '宝贝生日（可选）',
                          hintText: '例如 2021-06-01',
                        ),
                        validator: (value) {
                          final birthday = value?.trim() ?? '';
                          if (birthday.isEmpty) {
                            return null;
                          }
                          final parsed = DateTime.tryParse(birthday);
                          if (parsed == null ||
                              parsed.isAfter(DateTime.now())) {
                            return '请输入有效日期，例如 2021-06-01';
                          }
                          return null;
                        },
                      ),
                      const SizedBox(height: 16),
                      CheckboxListTile(
                        value: _hasGuardianConsent,
                        onChanged: _isSubmitting
                            ? null
                            : (value) => setState(
                                () => _hasGuardianConsent = value ?? false,
                              ),
                        contentPadding: EdgeInsets.zero,
                        controlAffinity: ListTileControlAffinity.leading,
                        title: const Text('我是孩子的监护人，并同意创建家长账号'),
                        subtitle: const Text('孩子使用设备前，请先由家长完成账号和绑定设置。'),
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
                      const SizedBox(height: 20),
                      FilledButton(
                        onPressed: _isSubmitting ? null : _submit,
                        child: Text(_isSubmitting ? '正在创建…' : '创建账号'),
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

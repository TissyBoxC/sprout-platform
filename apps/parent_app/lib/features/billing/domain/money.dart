/// 人民币金额，内部只用整数「分」表示。
///
/// 所有传输、计算和比较都以分为单位；只有显示时才换算成 `¥X.XX`。
/// 资金来源金额一旦出现浮点误差就无法对账，因此构造时拒绝小数输入，
/// 而不是悄悄四舍五入。
class Money implements Comparable<Money> {
  /// 解析后端返回的整数分。
  ///
  /// 只接受 [int]，其他类型一律抛出 [MoneyFormatException]，避免把
  /// 字符串或小数金额带进业务层。
  factory Money.fromCents(Object? value) {
    if (value is int) {
      return Money._(value);
    }
    throw MoneyFormatException(_describe(value));
  }

  /// 解析必须为非负的整数分，用于价格、优惠等不允许为负的字段。
  ///
  /// 不能用 const 构造：负数在编译期无法拦住，会绕过这里的安全检查。
  factory Money.fromNonNegativeCents(Object? value) {
    final money = Money.fromCents(value);
    if (money._cents < 0) {
      throw const MoneyFormatException('金额不能为负数');
    }
    return money;
  }

  Money.fromYuanString(String value) : this._(_parseYuan(value));

  const Money._(this._cents);

  final int _cents;

  static const zero = Money._(0);

  /// 整数分，跨层传递时使用该值。
  int get cents => _cents;

  bool get isZero => _cents == 0;
  bool get isPositive => _cents > 0;

  /// 显示用文本，固定两位小数，例如 `¥9.90`。
  String format() {
    final isNegative = _cents < 0;
    // 先取绝对值再做整数除法，负数才不会出现 `-0` 这样的中间结果。
    final absolute = _cents.abs();
    final yuan = absolute ~/ 100;
    final fraction = (absolute % 100).toString().padLeft(2, '0');
    return '${isNegative ? '-' : ''}¥$yuan.$fraction';
  }

  /// 不带货币符号的金额文本，例如 `9.90`。
  String yuanString() {
    final absolute = _cents.abs();
    final yuan = absolute ~/ 100;
    final fraction = (absolute % 100).toString().padLeft(2, '0');
    return '${_cents < 0 ? '-' : ''}$yuan.$fraction';
  }

  Money operator +(Money other) => Money._(_cents + other._cents);

  Money operator -(Money other) => Money._(_cents - other._cents);

  @override
  int compareTo(Money other) => _cents.compareTo(other._cents);

  @override
  bool operator ==(Object other) => other is Money && other._cents == _cents;

  @override
  int get hashCode => _cents.hashCode;

  @override
  String toString() => format();

  /// 解析 `¥9.90`、`9.9` 或 `9` 这类展示文本，最多两位小数。
  static int _parseYuan(String value) {
    final normalized = value.trim().replaceFirst('¥', '');
    final match = RegExp(r'^(-)?(\d+)(?:\.(\d{1,2}))?$').firstMatch(normalized);
    if (match == null) {
      throw MoneyFormatException('无法识别的金额：$value');
    }
    final sign = match.group(1) == '-' ? -1 : 1;
    final yuan = int.parse(match.group(2)!);
    final fractionText = match.group(3) ?? '';
    final fraction = int.parse(fractionText.padRight(2, '0'));
    return sign * (yuan * 100 + fraction);
  }

  static String _describe(Object? value) {
    if (value == null) {
      return '缺少金额';
    }
    return '金额必须是整数分';
  }
}

/// 金额字段不合法时抛出，调用方应转换为用户可见的普通提示。
class MoneyFormatException implements Exception {
  const MoneyFormatException(this.message);

  final String message;

  @override
  String toString() => 'MoneyFormatException($message)';
}

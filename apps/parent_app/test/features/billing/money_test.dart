import 'package:flutter_test/flutter_test.dart';
import 'package:parent_app/features/billing/domain/billing_errors.dart';

void main() {
  group('Money', () {
    test('renders zero cents as ¥0.00', () {
      expect(Money.fromCents(0).format(), '¥0.00');
      expect(Money.zero.format(), '¥0.00');
    });

    test('renders 990 cents as ¥9.90 with two decimals', () {
      expect(Money.fromCents(990).format(), '¥9.90');
    });

    test('always keeps two decimals', () {
      expect(Money.fromCents(1).format(), '¥0.01');
      expect(Money.fromCents(10).format(), '¥0.10');
      expect(Money.fromCents(100).format(), '¥1.00');
      expect(Money.fromCents(129900).format(), '¥1299.00');
    });

    test('keeps the amount as an integer count of cents internally', () {
      final money = Money.fromCents(1999);
      expect(money.cents, 1999);
      expect(money.cents, isA<int>());
      expect(money.yuanString(), '19.99');
    });

    test('rejects a non-integer input instead of rounding', () {
      expect(() => Money.fromCents(9.9), throwsA(isA<MoneyFormatException>()));
      expect(() => Money.fromCents('990'), throwsA(isA<MoneyFormatException>()));
      expect(() => Money.fromCents(null), throwsA(isA<MoneyFormatException>()));
      expect(() => Money.fromCents(true), throwsA(isA<MoneyFormatException>()));
      expect(
        () => Money.fromNonNegativeCents(9.9),
        throwsA(isA<MoneyFormatException>()),
      );
    });

    test('rejects a negative amount for price fields', () {
      expect(
        () => Money.fromNonNegativeCents(-1),
        throwsA(isA<MoneyFormatException>()),
      );
    });

    test('parses whole and fractional yuan without floating point', () {
      expect(Money.fromYuanString('¥9.90').cents, 990);
      expect(Money.fromYuanString('9.9').cents, 990);
      expect(Money.fromYuanString('9').cents, 900);
      expect(Money.fromYuanString('0.05').cents, 5);
      expect(() => Money.fromYuanString('9.999'), throwsA(isA<MoneyFormatException>()));
    });

    test('adds and subtracts in cents', () {
      expect((Money.fromCents(990) + Money.fromCents(10)).format(), '¥10.00');
      expect((Money.fromCents(1000) - Money.fromCents(1)).format(), '¥9.99');
      expect(Money.fromCents(100).compareTo(Money.fromCents(99)), greaterThan(0));
      expect(Money.fromCents(100), Money.fromCents(100));
    });
  });
}

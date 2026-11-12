package dualwrite

import "context"

/* ExecBoth 在双写事务的两库分别执行同一条 SQL；某库句柄为 nil（按 write_mode 不写入）时跳过。 */
func ExecBoth(ctx context.Context, tx *Tx, sql string, args ...any) error {
	if tx.Plain() != nil {
		if _, err := tx.Plain().Exec(ctx, sql, args...); err != nil {
			return err
		}
	}
	if tx.Secure() != nil {
		if _, err := tx.Secure().Exec(ctx, sql, args...); err != nil {
			return err
		}
	}
	return nil
}

package r9

// Regression (lnd bench): a long aliased import path is a *dst.BasicLit in a
// concrete ImportSpec.Path slot. R9 must never try to split it into a "+"-chain
// (it panicked with reflect.Set: *dst.BinaryExpr not assignable to *dst.BasicLit)
// and must leave it untouched — import lines can't wrap and lnd's `ll` linter
// exempts them anyway.
import (
	paymentsmig1sqlc "github.com/lightningnetwork/lnd/payments/db/migration1/sqlc"
)

var _ = paymentsmig1sqlc.Querier(nil)

package r8

func f(ctx Context, userID int) {
	log.InfoS(ctx, "user connected", slog.Int("user_id", userID))
}

type Context struct{}

var log = struct {
	InfoS func(ctx Context, msg string, args ...any)
}{InfoS: func(ctx Context, msg string, args ...any) {}}

var slog = struct {
	Int func(k string, v int) any
}{Int: func(k string, v int) any { return nil }}

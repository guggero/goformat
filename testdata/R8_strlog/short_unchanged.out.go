package r8

var (
	log = struct {
		InfoS func(ctx Context, msg string, args ...any)
	}{InfoS: func(ctx Context, msg string, args ...any) {}}
	slog = struct {
		Int func(k string, v int) any
	}{Int: func(k string, v int) any { return nil }}
)

func f(ctx Context, userID int) {
	log.InfoS(ctx, "user connected", slog.Int("user_id", userID))
}

type Context struct{}

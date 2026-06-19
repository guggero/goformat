package r8

func f(ctx Context, userID int, channelID string) {
	log.InfoS(ctx, "Channel open performed",
		slog.Int("user_id", userID),
		slog.String("channel_id", channelID))
}

type Context struct{}

var log = struct {
	InfoS func(ctx Context, msg string, args ...any)
}{InfoS: func(ctx Context, msg string, args ...any) {}}

var slog = struct {
	Int    func(k string, v int) any
	String func(k string, v string) any
}{
	Int:    func(k string, v int) any { return nil },
	String: func(k string, v string) any { return nil },
}

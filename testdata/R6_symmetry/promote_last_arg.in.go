package r6

const SettleEverythingThresholdSeconds = 86400

type client struct{}

type option func(int)

func (c *client) Settle(ctx int, opts ...option) (int, error) {
	return 0, nil
}

func WithExpiryThreshold(s int) option { return nil }

func test(c *client, ctx int) (int, error) {
	res, err := c.Settle(ctx, WithExpiryThreshold(SettleEverythingThresholdSeconds))
	return res, err
}

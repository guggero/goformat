package r4

type Builder struct{}

func (b *Builder) AddOp(op byte) *Builder         { return b }
func (b *Builder) AddData(data []byte) *Builder   { return b }
func (b *Builder) Script() ([]byte, error)        { return nil, nil }
func NewScriptBuilder() *Builder                  { return nil }

func test(scriptHash []byte) ([]byte, error) {
	const opHash160 byte = 0xa9
	const opEqual byte = 0x87
	scriptHashScript, err := NewScriptBuilder().
		AddOp(opHash160).
		AddData(scriptHash).
		AddOp(opEqual).
		Script()
	_ = scriptHashScript
	return nil, err
}

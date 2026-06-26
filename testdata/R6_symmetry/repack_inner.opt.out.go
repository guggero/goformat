package r6

type FrameType int
type Frame struct{}

const urPSBTType FrameType = 1

func Encode(t FrameType, body []byte, seqNum, seqLen int) Frame {
	return Frame{}
}

func test(body []byte, seqNum, seqLen int) []Frame {
	var frames []Frame
	for i := 0; i < 10; i++ {
		for j := 0; j < 10; j++ {
			frames = append(frames, Encode(
				urPSBTType, body, seqNum, seqLen,
			))
		}
	}
	return frames
}

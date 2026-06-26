package r6

type SubmitSignedPSBTRequest struct {
	AccountCode string
	QrPayload   string
	SessionId   string
}

type SubmitSignedPSBTResponse struct{}

type backend int

func (backend) SignersSubmitSignedPSBT() {}

func backendCall(t int, req *SubmitSignedPSBTRequest,
	resp *SubmitSignedPSBTResponse, fn func(),
	to int) *SubmitSignedPSBTResponse {

	return resp
}

func test(t int, code, frame string, b backend, shortTimeoutSeconds int) {
	resp := backendCall(
		t, &SubmitSignedPSBTRequest{
			AccountCode: code,
			QrPayload:   frame,
			SessionId:   "s1",
		}, &SubmitSignedPSBTResponse{},
		b.SignersSubmitSignedPSBT, shortTimeoutSeconds,
	)
	_ = resp
}

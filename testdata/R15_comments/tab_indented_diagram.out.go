package r15

var (
	// reports.go storage hierarchy. Tab-indented diagram lines must be left
	// exactly as-is (not normalized to "// \t"):
	//
	//	[chainHashBucket]
	//		[channelBucket]
	//			[resolversBucket]
	reportsBucket = []byte("reports")
)

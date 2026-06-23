package r15

// reports.go storage hierarchy. Tab-indented diagram lines must be left
// exactly as-is (not normalized to "// \t"):
//	[chainHashBucket]
//		[channelBucket]
//			[resolversBucket]
var reportsBucket = []byte("reports")

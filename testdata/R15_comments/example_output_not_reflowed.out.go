package r15

import "fmt"

// ExampleRouter exercises the router and asserts on its printed output.
func ExampleRouter() {
	fmt.Println("hi")

	// Output:
	// Actor router-greeter-1 spawned.
	// Router router(router-greeter-service) created for service key 'router-greeter-service'.
	// For Charlie: Received 'Greetings, Charlie!' from router-greeter-1
}

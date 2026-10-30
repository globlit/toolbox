// Command log-emitter prints synthetic fixtures for the disposable test cluster.
package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	b, err := os.ReadFile("/logs.txt")
	if err != nil {
		panic(err)
	}
	fmt.Print(string(b))
	time.Sleep(time.Hour)
}

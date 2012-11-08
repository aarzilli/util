package main;

import "fmt"
import "os"

func main() {
	fmt.Printf("%d %v\n", len(os.Args[1:]), os.Args[1:])
}

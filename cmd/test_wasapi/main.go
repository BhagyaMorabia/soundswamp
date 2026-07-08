package main

import (
	"fmt"
	"time"

	"github.com/soundswarm/soundswarm/internal/capture"
)

func main() {
	fmt.Println("Starting capture...")
	cap := capture.NewCapture()
	err := cap.Start()
	if err != nil {
		fmt.Println("Failed to start:", err)
		return
	}
	defer cap.Stop()

	fmt.Printf("Format: %+v\n", cap.Format())

	buf := make([]float32, 480)
	for i := 0; i < 5; i++ {
		fmt.Println("Waiting for read...")
		n, err := cap.Read(buf)
		fmt.Printf("Read: n=%d err=%v\n", n, err)
		time.Sleep(time.Second)
	}
}

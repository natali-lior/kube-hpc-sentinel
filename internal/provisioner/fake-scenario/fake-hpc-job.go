package fakescenario

import (
	"fmt"
	"math/rand"
	"os"
	"time"
)

func FakeHpcProcess() {
	jobName := os.Getenv("JOB_NAME")
	if jobName == "" {
		jobName = "hpc-task"
	}

	fmt.Printf("🚀 Starting HPC Simulation for: %s\n", jobName)
	fmt.Printf("📦 Initializing GPU context (simulated)...\n")
	time.Sleep(2 * time.Second)

	iterations := 5
	for i := 1; i <= iterations; i++ {
		fmt.Printf("🔄 [Iteration %d/%d] Computing fast Fourier transform on matrix 0x%X...\n", i, iterations, rand.Intn(1000000))

		nonsenseSum := 0
		for j := 0; j < 1000000; j++ {
			nonsenseSum += j
		}

		fmt.Printf("⏳ Sleeping to let the 'GPU' cool down... (%dms)\n", rand.Intn(1000)+500)
		time.Sleep(time.Duration(rand.Intn(2000)) * time.Millisecond)
	}

	fmt.Println("✅ Calculation complete. Synchronizing results across nodes...")
	time.Sleep(1 * time.Second)
	fmt.Println("🎉 Job finished successfully. Exiting 0.")
	os.Exit(0)
}

// Command stdio-worker is the reference worker binary of the M6 slice-5B
// controlled implementation. It speaks the signed frame contract on stdio:
// HELLO (spec digest binding), HEARTBEAT, and one final RESULT. Real
// skill/plugin workers embed the same stdioworker.Child half; this binary
// exists so the runtime, the red team and the evidence bundles exercise a
// real, digest-pinned executable end to end.
//
// Modes (argv[1]):
//
//	echo      — hello, one beat, result {"ok":true}; default
//	sleep N   — hello, beats every second, result after N seconds
//	forever   — hello then beats forever (deadline/heartbeat negative test)
//	silent    — hello then nothing (heartbeat-loss negative test)
//	job       — echo one host job payload as the result
package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/lunitide/lunitide/internal/stdioworker"
)

func main() {
	mode := "echo"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	child, err := stdioworker.ChildFromEnv(os.Getenv, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "stdio-worker:", err)
		os.Exit(2)
	}
	if err := child.Hello(); err != nil {
		fmt.Fprintln(os.Stderr, "stdio-worker: hello:", err)
		os.Exit(3)
	}
	switch mode {
	case "echo":
		_ = child.Heartbeat()
		_ = child.Result(map[string]any{"ok": true, "mode": mode})
	case "sleep":
		secs := 1
		if len(os.Args) > 2 {
			secs, _ = strconv.Atoi(os.Args[2])
		}
		for i := 0; i < secs; i++ {
			time.Sleep(time.Second)
			_ = child.Heartbeat()
		}
		_ = child.Result(map[string]any{"ok": true, "slept": secs})
	case "forever":
		for {
			time.Sleep(time.Second)
			_ = child.Heartbeat()
		}
	case "silent":
		select {}
	case "job":
		env, err := stdioworker.Job(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "stdio-worker: job:", err)
			os.Exit(4)
		}
		_ = child.Result(map[string]any{"jobType": env.Type, "jobSeq": env.Seq})
	default:
		fmt.Fprintln(os.Stderr, "stdio-worker: unknown mode", mode)
		os.Exit(2)
	}
	os.Exit(0)
}

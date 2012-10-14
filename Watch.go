package main;

import (
	"syscall"
	"flag"
	"log"
	"fmt"
	"io"
	"unsafe"
	"strings"
	"os"
	"os/exec"
	"time"
	"sync"
)

var args []string
var shouldKill = flag.Bool("k", false, "If a change happens while the command is running kill the command instead of discarding the event")
var delayPeriod = flag.Int("d", 3, "Number of seconds after running the command while events will be discarded (default 3)")
var recurse = flag.Int("r", 0, "Recursively register subdirectories, set to the maximum recursion depth (default 0)")
var verbose = flag.Bool("v", false, "Verbose")

var doneTimeMutex sync.Mutex
var doneTime time.Time
var running bool
var killChan = make(chan bool, 0)

func startCommand(clean bool) {
	running = true

	if *verbose {
		log.Printf("Running command: %v", args)
	} else {
		if clean {
			os.Stdout.Write([]byte{ 0x1b, '[', 'H' })
			os.Stdout.Write([]byte{ 0x1b, '[', '2', 'J' })
		}
		fmt.Printf("Executing %s\n", strings.Join(args, " "))
	}

	go func() {
		cmd := exec.Command(args[0], args[1:]...)

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Printf("Could not get stdout of command: %v", err)
			return
		}

		stderr, err := cmd.StderrPipe()
		if err != nil {
			log.Printf("Could not get stderr of command: %v", err)
			return
		}

		err = cmd.Start()
		if err != nil {
			log.Printf("Could not execute command: %v", err)
			return
		}

		go io.Copy(os.Stdout, stdout)
		go io.Copy(os.Stderr, stderr)

		waitChan := make(chan bool, 0)
		go func() {
			err := cmd.Wait()
			if *verbose {
				if err != nil { log.Printf("Error executing command: %v", err) }
			}

			// signal the end of the process if anyone is listening
			select {
			case waitChan <- true:
			default:
			}
		}()

		// wait either for the end of the process (waitChan) or a request to kill it
		select {
		case <-waitChan:
			if *verbose {
				log.Printf("Command executed")
			} else {
				fmt.Printf("Done\n")
			}
			break
		case <-killChan:
			if *verbose { log.Printf("Killing command") }
			cmd.Process.Kill()
			break
		}

		doneTimeMutex.Lock()
		doneTime = time.Now()
		running = false
		doneTimeMutex.Unlock()
	}()
}

func canExecute() bool {
	if *shouldKill {
		select {
		case killChan <- true:
		default:
		}
		return true
	}

	doneTimeMutex.Lock()
	defer doneTimeMutex.Unlock()

	if running { return false }
	delayEnd := doneTime.Add(time.Duration(*delayPeriod) * time.Second)
	if *verbose { log.Printf("Now: %v delayEnd: %v\n", time.Now(), delayEnd) }
	return time.Now().After(delayEnd)
}

func LsDir(dirname string) ([]os.FileInfo, error) {
	dir, err := os.Open(dirname)
	if err != nil { return nil, err }
	defer dir.Close()
	return dir.Readdir(-1)
}

func registerDirectory(inotifyFd int, dirname string, recurse int) {
	_, err := syscall.InotifyAddWatch(inotifyFd, dirname, syscall.IN_CREATE | syscall.IN_DELETE | syscall.IN_CLOSE_WRITE)
	if err != nil { log.Fatalf("Can not add %s to inotify: %v", dirname, err) }

	if recurse <= 0 { return }

	dir, err := LsDir(dirname)
	if err != nil { log.Fatalf("Can not read directory %s: %v", dirname, err) }

	for _, cur := range dir {
		if cur.Mode().IsDir() {
			registerDirectory(inotifyFd, dirname + "/" + cur.Name(), recurse-1)
		}
	}
}

func main() {
	flag.Parse()
	args = flag.Args()

	startCommand(false)

	for {
		inotifyFd, err := syscall.InotifyInit()
		if err != nil { log.Fatalf("Inotify init failed: %v", err) }

		registerDirectory(inotifyFd, ".", *recurse)

		inotifyBuf := make([]byte, 1024*syscall.SizeofInotifyEvent + 16)

		for {
			n, err := syscall.Read(inotifyFd, inotifyBuf[0:])
			if err == io.EOF { break }
			if err != nil {
				log.Printf("Can not read inotify: %v", err)
				break
			}

			nameLen := uint32(0)
			for offset := uint32(0); offset < uint32(n)-syscall.SizeofInotifyEvent; offset += syscall.SizeofInotifyEvent + nameLen {
				event := (*syscall.InotifyEvent)(unsafe.Pointer(&inotifyBuf[offset]))
				nameLen = event.Len
				if canExecute() { startCommand(true) }
			}
		}

		syscall.Close(inotifyFd);
	}
}

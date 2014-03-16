package main

import (
	"os"
	"os/exec"
	"log"
	"regexp"
	"time"
	"fmt"
	"io"
	"hash/crc32"
)

const DUMMY = false

var sourcePath, backupPath, excludePath, includePath string
var checkSuccess bool

func initPaths() {
	config := os.Getenv("XDG_CONFIG_HOME")
	if config == "" {
		config = os.Getenv("HOME") + "/.config"
	}
	config = config + "/beck/"

	sourcePath = config + "source"
	backupPath = config + "backup"
	excludePath = config + "exclude"
	includePath = config + "include"
}

func readableFile(path string) {
	if file, err := os.Open(path); err != nil {
		log.Fatalf("Can not read %s\n", path)
	} else {
		file.Close()
	}
}

func validDirLink(path string) {
	entry, err := os.Lstat(path)
	if err != nil {
		log.Fatalf("Can not stat %s\n", path)
	}

	if (entry.Mode() & os.ModeSymlink) == 0 {
		log.Fatalf("%s is not a symbolic link", path)
	}

	entry, err = os.Stat(path)
	if err != nil {
		log.Fatalf("Can not stat path linked by %s\n", path)
	}

	if (entry.Mode() & os.ModeDir) == 0 {
		log.Fatalf("Path linked by %s is not a directory\n", path)
	}
}

func checkConfig() {
	readableFile(excludePath)
	readableFile(includePath)
	validDirLink(sourcePath)
	validDirLink(backupPath)
}

func lastBackupDir() (string, string) {
	dir, err := os.Open(backupPath)
	defer dir.Close()
	if err != nil {
		log.Fatalf("Can not read %s: %v\n", backupPath, err)
	}

	backupDirs, err := dir.Readdir(0)
	if err != nil {
		log.Fatalf("Can not read %s: %v\n", backupPath, err)
	}

	now := time.Now().Format("20060102150405")
	max := ""
	re := regexp.MustCompile("^backup\\.(\\d+)$")
	for _, backupDir := range backupDirs {
		submatches := re.FindStringSubmatch(backupDir.Name())
		if submatches == nil { continue }
		cur := submatches[1]
		if len(cur) != len("20060102150405") { continue }
		if submatches[1] > max { max = submatches[1] }
	}

	if max == "" {
		return "", fmt.Sprintf("%s/backup.%s", backupPath, now)
	}

	return fmt.Sprintf("%s/backup.%s", backupPath, max), fmt.Sprintf("%s/backup.%s", backupPath, now)
}

func cmdExec(args ...string) {
	log.Printf("Executing %v", args)
	if !DUMMY {
		cmd := exec.Command(args[0], args[1:]...)

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Fatalf("Could not get stdout of rsync command: %v", err)
		}

		stderr, err := cmd.StderrPipe()
		if err != nil {
			log.Fatalf("Could not get stderr of rsync command: %v", err)
		}

		err = cmd.Start()
		if err != nil {
			log.Fatalf("Could not execute rsync command correctly: %v", err)
		}

		go io.Copy(os.Stdout, stdout)
		go io.Copy(os.Stderr, stderr)
		err = cmd.Wait()
		if err != nil {
			log.Fatalf("Error executing rsync command: %v", err)
		}
	}
}

func newBackup(backupPath string) {
	err := os.Chdir(sourcePath)
	if err != nil {
		log.Fatalf("Can not access source directory %s: %v", sourcePath, err)
	}

	cmdExec("rsync", "-v", "-a", "--exclude-from="+excludePath, "--include-from="+includePath, ".", backupPath)
}

func incrementalBackup(oldBackupPath, newBackupPath string) {
	err := os.Chdir(sourcePath)
	if err != nil {
		log.Fatalf("Can not access source directory %s: %v", sourcePath, err)
	}

	cmdExec("rsync", "-v", "-a", "--delete", "--link-dest="+oldBackupPath, "--exclude-from="+excludePath, "--include-from="+includePath, ".", newBackupPath)
}

func doBackup(lbp, nbp string) {
	if lbp == "" {
		newBackup(nbp)
	} else {
		incrementalBackup(lbp, nbp)
	}
}

func checksum(path string, buf []byte) uint32{
	file, err := os.Open(path)
	if err != nil {
		log.Printf("Error reading %s: %v", path, err)
		return 0
	}
	defer file.Close()

	crc := crc32.NewIEEE()
	for {
		n, err := file.Read(buf)
		if err == io.EOF { break }
		if err != nil {
			log.Fatalf("Error reading %s: %v", path, err)
		}
		crc.Write(buf[:n])
	}

	//log.Printf("%s -> %v", path, crc.Sum32())

	return crc.Sum32()
}

func checkFile(sourcePath, backupPath string, buf []byte) {
	//log.Printf("Comparing <%s> <%s>", sourcePath, backupPath)
	if checksum(sourcePath, buf) != checksum(backupPath, buf) {
		log.Printf("FAILED for %s", sourcePath)
		checkSuccess = false
	}
}

func checkDir(sourcePath, backupDir string, shouldPrint bool, buf []byte) {
	backupFile, err := os.Open(backupDir)
	if err != nil {
		log.Fatalf("Can not open backup directory %s: %v", backupDir, err)
	}
	defer backupFile.Close()

	var files []os.FileInfo
	{
		defer backupFile.Close()
		files, err = backupFile.Readdir(0)
		if err != nil {
			log.Fatalf("Can not read directory %s: %v", backupDir, err)
		}
	}

	for _, fileInfo := range files {
		if (fileInfo.Mode() & os.ModeDir) != 0 {
			if (shouldPrint) { log.Printf("Checking directory %s", backupDir + "/" + fileInfo.Name()) }
			checkDir(sourcePath + "/" + fileInfo.Name(), backupDir + "/" + fileInfo.Name(), false, buf)
		} else if (fileInfo.Mode() & os.ModeType) == 0 {
			// regular file
			checkFile(sourcePath + "/" + fileInfo.Name(), backupDir + "/" + fileInfo.Name(), buf)
		} else {
			log.Printf("Skipping %s", backupDir + "/" + fileInfo.Name())
		}
	}
}

func doCheck(backupDir string, subdir string) {
	checkSuccess = true
	buf := make([]byte, 4086)
	if subdir != "" {
		checkDir(sourcePath+"/"+subdir, backupDir+"/"+subdir, true, buf)
	} else {
		checkDir(sourcePath, backupDir, true, buf)
	}
	if !checkSuccess {
		log.Printf("Some files did not check correctly")
	}
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: beck (back|check)")
	}

	initPaths()
	checkConfig()

	lbp, nbp := lastBackupDir()

	log.Printf("Last backup directory is: %s, %s\n", lbp, nbp)

	switch os.Args[1] {
	case "check":
		if len(os.Args) == 3 {
			doCheck(lbp, os.Args[2])
		} else {
			doCheck(lbp, "")
		}
		break;
	case "back":
		doBackup(lbp, nbp)
		break
	}
}

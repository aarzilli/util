package main

import (
	"bufio"
	"code.google.com/p/go.crypto/ssh"
	"compress/gzip"
	"fmt"
	"github.com/pkg/sftp"
	"hash/crc32"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const DUMMY = false

const SZDEBUG = false
const BACKUP_PREFIX = "backup."

const RSYNC_PREFIX = "rsync:"

var sourcePath, backupPath, excludePath, includePath string
var checkSuccess bool

func decideIfRemoteBackup(config string) {
	fh, err := os.Open(config + "remote")
	if err == nil {
		defer fh.Close()
		b, err := ioutil.ReadAll(fh)
		if err == nil {
			backupPath = RSYNC_PREFIX + strings.TrimSpace(string(b))
			log.Printf("Remote backup enabled: %s", backupPath)
			return
		}
	}

	dest, err := os.Readlink(backupPath)
	if err != nil {
		return
	}
	log.Printf("Backup link destination: %s", dest)
	if strings.HasPrefix(dest, RSYNC_PREFIX) {
		backupPath = dest
		log.Printf("Remote backup enabled: %s", backupPath)
		return
	}
}

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

	decideIfRemoteBackup(config)
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

func isRemoteBackup() bool {
	return strings.HasPrefix(backupPath, RSYNC_PREFIX)
}

func parseRemoteBackup(bp string) (user, host, path string) {
	defer func() {
		if ierr := recover(); ierr == nil {
			return
		}
		log.Fatalf("Unrecognized remote path \"%s\" expected format rsync:<username>@<host>:<path>", bp)
	}()
	a := strings.SplitN(bp, ":", 2)
	if a[0] != "rsync" {
		panic("blah")
	}
	v := strings.SplitN(a[1], "@", 2)
	user = v[0]
	vv := strings.SplitN(v[1], ":", 2)
	host = vv[0]
	path = vv[1]
	return
}

func getPublicKey() ssh.AuthMethod {
	fh, err := os.Open(os.ExpandEnv("$HOME/.ssh/id_rsa"))
	if err != nil {
		log.Fatalf("Could not open id_rsa: %v\n", err)
	}
	defer fh.Close()
	b, err := ioutil.ReadAll(fh)
	if err != nil {
		log.Fatalf("Could not read id_rsa: %v\n", err)
	}
	signer, err := ssh.ParsePrivateKey(b)
	if err != nil {
		log.Fatalf("Could not parse id_rsa: %v\n", err)
	}
	return ssh.PublicKeys(signer)
}

func checkConfig() {
	readableFile(excludePath)
	readableFile(includePath)
	validDirLink(sourcePath)
	if !isRemoteBackup() {
		validDirLink(backupPath)
	}
}

func readBackupDirLocal() []string {
	dir, err := os.Open(backupPath)
	defer dir.Close()
	if err != nil {
		log.Fatalf("Can not read %s: %v\n", backupPath, err)
	}

	backupDirs, err := dir.Readdir(0)
	if err != nil {
		log.Fatalf("Can not read %s: %v\n", backupPath, err)
	}

	r := make([]string, len(backupDirs))
	for i := range backupDirs {
		r[i] = backupDirs[i].Name()
	}

	return r
}

func openSshConnection() *ssh.Client {
	user, host, _ := parseRemoteBackup(backupPath)
	log.Printf("Connecting to %s %s", user, host)
	backupSsh, err := ssh.Dial("tcp", host+":22", &ssh.ClientConfig{User: user, Auth: []ssh.AuthMethod{getPublicKey()}})
	if err != nil {
		log.Fatalf("Error connecting to the server: %v", err)
	}
	return backupSsh
}

func readBackupDirRemote() []string {
	_, _, path := parseRemoteBackup(backupPath)
	backupSsh := openSshConnection()
	defer backupSsh.Close()
	backupSftp, err := sftp.NewClient(backupSsh)
	if err != nil {
		log.Fatalf("Error initiating sftp session: %v", err)
	}
	defer backupSftp.Close()
	backupDirs, err := backupSftp.ReadDir(path)
	if err != nil {
		log.Fatalf("Error reading directory: %v\n", err)
	}
	r := make([]string, len(backupDirs))
	for i := range backupDirs {
		r[i] = backupDirs[i].Name()
	}
	return r
}

func readBackupDir() []string {
	if isRemoteBackup() {
		return readBackupDirRemote()
	} else {
		return readBackupDirLocal()
	}
}

func lastBackupDir() (string, string) {
	backupDirs := readBackupDir()

	now := time.Now().Format("20060102150405")
	max := ""
	re := regexp.MustCompile("^backup\\.(\\d+)$")
	for _, backupDir := range backupDirs {
		submatches := re.FindStringSubmatch(backupDir)
		if submatches == nil {
			continue
		}
		cur := submatches[1]
		if len(cur) != len("20060102150405") {
			continue
		}
		if submatches[1] > max {
			max = submatches[1]
		}
	}

	if max == "" {
		return "", fmt.Sprintf("%s/backup.%s", backupPath, now)
	}

	return fmt.Sprintf("%s/backup.%s", backupPath, max), fmt.Sprintf("%s/backup.%s", backupPath, now)
}

func cmdExec(args ...string) {
	log.Printf("Executing %v", args)
	if DUMMY {
		return
	}

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

func cmdExecRemote(sshc *ssh.Client, args ...string) {
	cmd := strings.Join(args, " ")
	log.Printf("Executing (remotely) %s", cmd)
	if DUMMY {
		return
	}
	sshs, err := sshc.NewSession()
	if err != nil {
		log.Fatalf("Could not create ssh session: %v", err)
	}
	defer sshs.Close()

	stdout, err := sshs.StdoutPipe()
	if err != nil {
		log.Fatalf("Could not get stdout of ssh session: %v", err)
	}
	stderr, err := sshs.StderrPipe()
	if err != nil {
		log.Fatalf("Could not get stderr of ssh session: %v", err)
	}

	err = sshs.Start(cmd)
	if err != nil {
		log.Fatalf("Could not execute command: %v", err)
	}

	go io.Copy(os.Stdout, stdout)
	go io.Copy(os.Stderr, stderr)
	err = sshs.Wait()
	if err != nil {
		log.Fatalf("Error executing (remote) command: %v", err)
	}
}

func newBackup(backupPath string) {
	if isRemoteBackup() {
		log.Fatalf("Can not create new remote backup yet")
	}
	err := os.Chdir(sourcePath)
	if err != nil {
		log.Fatalf("Can not access source directory %s: %v", sourcePath, err)
	}

	cmdExec("rsync", "-v", "-a", "--exclude-from="+excludePath, "--include-from="+includePath, ".", backupPath)
}

func incrementalBackupLocal(oldBackupPath, newBackupPath string) {
	err := os.Chdir(sourcePath)
	if err != nil {
		log.Fatalf("Can not access source directory %s: %v", sourcePath, err)
	}

	cmdExec("rsync", "-v", "-a", "--delete", "--link-dest="+oldBackupPath, "--exclude-from="+excludePath, "--include-from="+includePath, ".", newBackupPath)
}

func incrementalBackupRemote(oldBackupPath, newBackupPath string) {
	err := os.Chdir(sourcePath)
	if err != nil {
		log.Fatalf("Can not access source directory %s: %v", sourcePath, err)
	}

	backupSsh := openSshConnection()
	defer backupSsh.Close()
	_, _, obp := parseRemoteBackup(oldBackupPath)
	user, host, nbp := parseRemoteBackup(newBackupPath)
	cmdExecRemote(backupSsh, "cp", "--preserve=all", "-l", "--no-dereference", "-R", obp, nbp)
	cmdExec("rsync", "-e", "ssh", "-v", "-a", "--delete", "--exclude-from="+excludePath, "--include-from="+includePath, ".", user+"@"+host+":"+nbp)
}

func incrementalBackup(oldBackupPath, newBackupPath string) {
	if isRemoteBackup() {
		incrementalBackupRemote(oldBackupPath, newBackupPath)
	} else {
		incrementalBackupLocal(oldBackupPath, newBackupPath)
	}
}

func doBackup(lbp, nbp string) {
	if lbp == "" {
		newBackup(nbp)
	} else {
		incrementalBackup(lbp, nbp)
	}
}

func checksum(path string, buf []byte) uint32 {
	file, err := os.Open(path)
	if err != nil {
		log.Printf("Error reading %s: %v", path, err)
		return 0
	}
	defer file.Close()

	crc := crc32.NewIEEE()
	for {
		n, err := file.Read(buf)
		if err == io.EOF {
			break
		}
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
			if shouldPrint {
				log.Printf("Checking directory %s", backupDir+"/"+fileInfo.Name())
			}
			checkDir(sourcePath+"/"+fileInfo.Name(), backupDir+"/"+fileInfo.Name(), false, buf)
		} else if (fileInfo.Mode() & os.ModeType) == 0 {
			// regular file
			checkFile(sourcePath+"/"+fileInfo.Name(), backupDir+"/"+fileInfo.Name(), buf)
		} else {
			log.Printf("Skipping %s", backupDir+"/"+fileInfo.Name())
		}
	}
}

func doCheck(backupDir string, subdir string) {
	if isRemoteBackup() {
		log.Printf("Can not check remote directory")
		return
	}
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

func humanReadable(v int) string {
	if d := float64(v) / float64(1024*1024*1024); d >= 1.0 {
		return fmt.Sprintf("%0.02fGB", d)
	}

	if d := float64(v) / float64(1024*1024); d >= 1.0 {
		return fmt.Sprintf("%0.02fMB", d)
	}

	if d := float64(v) / 1024.0; d >= 1.0 {
		return fmt.Sprintf("%0.02fkB", d)
	}

	return fmt.Sprintf("%dB", v)
}

func doSz(bsop string, verbose bool) {
	//TODO: verbose version of sz, show diff files
	var rd io.Reader

	fh, err := os.Open(bsop)
	if err != nil {
		log.Fatalf("Could not open %s: %v\n", bsop, err)
	}
	defer fh.Close()

	if strings.HasSuffix(bsop, ".gz") {
		gzrd, err := gzip.NewReader(fh)
		if err != nil {
			log.Fatalf("Could not open %s (compression): %v\n", bsop, err)
		}
		defer gzrd.Close()
		rd = gzrd
	} else {
		rd = fh
	}

	curInode := ""
	dateList := []string{}
	totSize := map[string]int{}
	changes := map[string][]string{}
	curSz := 0
	curPath := ""

	flushfn := func() {
		if len(dateList) <= 0 {
			return
		}

		sort.Strings(dateList)
		if _, ok := totSize[dateList[0]]; !ok {
			totSize[dateList[0]] = 0
			changes[dateList[0]] = []string{}
		}
		totSize[dateList[0]] += curSz
		changes[dateList[0]] = append(changes[dateList[0]], fmt.Sprintf("%s %s", humanReadable(curSz), curPath))
		if SZDEBUG {
			fmt.Printf("Assigning %s to %s: %s\n", curInode, dateList[0], curPath)
		}
		dateList = dateList[0:0]
	}

	scanner := bufio.NewScanner(rd)
	for scanner.Scan() {
		line := strings.SplitN(strings.TrimSpace(scanner.Text()), " ", 3)
		switch len(line) {
		case 0:
			continue
		case 3:
			//nothing
		default:
			log.Fatalf("Could not parse input line: <%s>\n", scanner.Text(), len(line))
		}

		inode := line[0]
		sz, err := strconv.ParseInt(line[1], 10, 32)
		if err != nil {
			log.Fatalf("Could not parse input line (malformed size): <%s>: %v\n", scanner.Text(), err)
		}
		path := strings.Split(line[2], "/")
		date := ""
		pathRest := ""

		for i := range path {
			if !strings.HasPrefix(path[i], BACKUP_PREFIX) {
				continue
			}

			date = path[i][len(BACKUP_PREFIX):]
			pathRest = strings.Join(path[i+1:], "/")
			break
		}

		if date == "" {
			log.Fatalf("Could not parse input line (no backup date): <%s>\n", scanner.Text())
		}

		if curInode != inode {
			flushfn()
			curInode = inode
			curPath = pathRest
			curSz = int(sz)
		}
		dateList = append(dateList, date)
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading %s: %v\n", bsop, err)
	}
	flushfn()

	ks := make([]string, 0, len(totSize))
	for k, _ := range totSize {
		ks = append(ks, k)
	}
	sort.Strings(ks)

	first := true
	for _, k := range ks {
		fmt.Printf("%s\t%s\n", k, humanReadable(totSize[k]))
		if verbose && !first {
			for i := range changes[k] {
				fmt.Printf("\t%s\n", changes[k][i])
			}
			fmt.Printf("\n")
		}
		first = false
	}
}

func main() {
	if len(os.Args) < 2 {
		log.Fatalf("Usage: beck (back|check [<subdir>]|sz <becksz.sh out>)")
	}

	var lbp, nbp string
	if os.Args[1] != "sz" {
		initPaths()
		checkConfig()

		lbp, nbp = lastBackupDir()

		log.Printf("Last backup directory is: %s, %s\n", lbp, nbp)
	}

	switch os.Args[1] {
	case "check":
		if len(os.Args) == 3 {
			doCheck(lbp, os.Args[2])
		} else {
			doCheck(lbp, "")
		}
		break
	case "back":
		doBackup(lbp, nbp)
		break
	case "sz":
		if len(os.Args) < 3 {
			log.Fatalf("Usage: beck sz <output of becksz.sh>")
		}

		path := ""
		verbose := false

		for i := range os.Args[2:] {
			switch os.Args[i+2] {
			case "-v":
				verbose = true
			default:
				path = os.Args[i+2]
			}
		}

		doSz(path, verbose)
	}
}

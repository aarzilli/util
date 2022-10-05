package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

type Node struct {
	Name  string
	Child []Node
}

func spacePrefix(s string) string {
	for i := range s {
		if s[i] != ' ' {
			return s[:i]
		}
	}
	return s
}

func removePrefix(ss []string, pfx string) []string {
	for i := range ss {
		ss[i] = ss[i][len(pfx):]
	}
	return ss
}

func parse(buf []string) []Node {
	var r []Node
	for i := 0; i < len(buf); i++ {
		if buf[i] == "" {
			continue
		}
		pfx := spacePrefix(buf[i])
		if pfx != "" {
			var j int
			for j = i; j < len(buf); j++ {
				if !strings.HasPrefix(buf[j], pfx) {
					break
				}
			}
			r[len(r)-1].Child = parse(removePrefix(buf[i:j], pfx))
			i = j - 1
		} else {
			r = append(r, Node{Name: buf[i]})
		}

	}
	return r
}

func volumes(ns []Node) []Node {
	var r []Node
	for i := range ns {
		if strings.HasPrefix(ns[i].Name, "Volume") {
			r = append(r, ns[i])
		} else {
			r = append(r, volumes(ns[i].Child)...)
		}
	}
	return r
}

func only(f func(Node) bool, ns []Node) []Node {
	var r []Node
	for i := range ns {
		if f(ns[i]) {
			r = append(r, ns[i])
		}
	}
	return r
}

func namePred(f func(string) bool) func(Node) bool {
	var r func(Node) bool
	r = func(n Node) bool {
		if f(n.Name) {
			return true
		}
		for i := range n.Child {
			if r(n.Child[i]) {
				return true
			}
		}
		return false
	}
	return r
}

func field(n Node, pfx string) string {
	if strings.HasPrefix(n.Name, pfx) {
		s := n.Name[len(pfx)+2:]
		return s[1 : len(s)-1]
	}
	for i := range n.Child {
		if s := field(n.Child[i], pfx); s != "" {
			return s
		}
	}
	return ""
}

func main() {
	cmd := exec.Command("gio", "mount", "-l", "-i")
	buf, err := cmd.CombinedOutput()
	must(err)
	out := parse(strings.Split(string(buf), "\n"))
	//fmt.Printf("%s\n", string(buf))

	r := only(namePred(func(name string) bool {
		return name == "can_mount=1"
	}), volumes(only(namePred(func(name string) bool {
		return strings.Contains(name, "media-removable-symbolic")
	}), out)))

	//fmt.Printf("%v\n", r)

	if len(r) != 1 {
		os.Exit(1)
	}

	dev := field(r[0], "unix-device")
	fmt.Printf("%q\n", dev)
	if dev != "" {
		cmd := exec.Command("gio", "mount", "-d", dev)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		must(cmd.Run())
	}
}
